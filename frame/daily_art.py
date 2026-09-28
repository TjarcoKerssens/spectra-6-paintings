#!/usr/bin/env -S uv run --script
# /// script
# requires-python = ">=3.9"
# dependencies = ["pillow", "pillow-heif", "pyyaml", "python-dotenv"]
# ///
"""Serve one random painting per day to the EE02 frame.

The frame wakes every few minutes by day and asks /pending whether there is a
picture it has not drawn yet. If there is it GETs /today.png. The first request of a
new day picks a random painting from one of the sources in sources.yaml, runs it
through spectra6 and caches the result, so every later request that day gets the
same image. If the source or the conversion fails, the last good image is served
instead. KEY2 on the board asks for /next.png, which picks a new painting right away.

Open the same port in a browser for a small web app showing today's painting,
where it came from and the recent log, so a frame that stays blank can be
explained without opening a terminal. It can also pick another painting or take
an upload, and the frame draws it on its next check.

Settings come from .env at the root of the repo, see .env.example.

Run: uv run daily_art.py [port]   (default FRAME_PORT, else 8765)
     uv run daily_art.py try         show one pick from every source, without converting it
"""

import collections
import datetime
import json
import math
import os
import random
import re
import shutil
import statistics
import subprocess
import sys
import tempfile
import threading
import urllib.error
import urllib.parse
import urllib.request
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from urllib.parse import urlsplit

import yaml
from dotenv import load_dotenv
from PIL import Image, ImageOps
from pillow_heif import register_heif_opener

register_heif_opener()  # an iPhone uploads HEIC

MET = "https://collectionapi.metmuseum.org/public/collection/v1"  # search lives on v1.1, v1/search retires 2026-10-01
AIC = "https://api.artic.edu/api/v1"
CMA = "https://openaccess-api.clevelandart.org/api/artworks/"
MET_KEYS = ("objectID", "title", "artistDisplayName", "artistDisplayBio", "objectDate",
            "medium", "dimensions", "culture", "department", "creditLine", "objectURL")
AIC_FIELDS = ["id", "title", "artist_display", "date_display", "medium_display", "dimensions",
              "place_of_origin", "department_title", "credit_line", "image_id", "thumbnail",
              "style_title", "classification_title", "short_description", "description"]
HERE = os.path.dirname(os.path.abspath(__file__))
load_dotenv(os.path.join(HERE, "..", ".env"))  # a missing .env is fine, every setting has a default
STATE = os.path.expanduser(os.environ.get("FRAME_STATE_DIR") or "~/.local/share/spectra6-frame")
SPECTRA6 = os.environ.get("SPECTRA6_BIN") or shutil.which("spectra6") or os.path.expanduser("~/bin/spectra6")
TODAY = os.path.join(STATE, "today.png")
ORIGINAL = os.path.join(STATE, "original.jpg")  # the painting as it is, for the web app
BOARD = os.path.join(STATE, "board.json")  # what the board told us when it last woke
PENDING = os.path.join(STATE, "pending")  # exists while the web app has a picture the board has not drawn
FETCHED = os.path.join(STATE, "fetched")  # touched when the board pulls the image, only its mtime counts
APP = os.path.join(HERE, "app.html")
CONFIG = os.path.join(HERE, "sources.yaml")
ICONS = {"/icon.svg": "image/svg+xml", "/apple-touch-icon.png": "image/png",
         "/icon-192.png": "image/png", "/icon-512.png": "image/png"}
PANEL = 1600 / 1200  # what spectra6 writes for a landscape source, it crops to fill
WAKE = os.environ.get("FRAME_WAKE") or "06:00"  # the board sleeps from BEDTIME to WAKE and checks every POLL seconds in between
BEDTIME = os.environ.get("FRAME_BEDTIME") or "23:00"
POLL = int(os.environ.get("FRAME_POLL") or 600)  # every check costs the battery about 0.05 mAh, see frame/README.md
MAX_UPLOAD_MB = int(os.environ.get("FRAME_MAX_UPLOAD_MB") or 40)
lock = threading.Lock()
lines = collections.deque(maxlen=200)  # what the web app shows, lost on restart


def get(url, body=None):
    # the Art Institute refuses its images without AIC-User-Agent
    req = urllib.request.Request(url, body, {"User-Agent": "spectra6-frame", "AIC-User-Agent": "spectra6-frame",
                                             "Content-Type": "application/json"})
    with urllib.request.urlopen(req, timeout=60) as r:
        return r.read()


def look(path):
    """Width, height and colourfulness, the Hasler and Süsstrunk metric. Grey is 0, ink
    wash on beige paper about 18, a Monet about 27, a Lichtenstein about 68."""
    with Image.open(path) as im:
        w, h = im.size
        px = im.convert("RGB").resize((64, 64)).tobytes()
    r, g, b = px[0::3], px[1::3], px[2::3]
    rg = [x - y for x, y in zip(r, g)]
    yb = [(x + y) / 2 - z for x, y, z in zip(r, g, b)]
    spread = math.hypot(statistics.pstdev(rg), statistics.pstdev(yb))
    return w, h, spread + 0.3 * math.hypot(statistics.mean(rg), statistics.mean(yb))


def wiki(url):
    """The opening of the Wikipedia article behind a Wikidata item, if it has one."""
    qid = url.rstrip("/").split("/")[-1]
    ent = json.loads(get("https://www.wikidata.org/w/api.php?action=wbgetentities"
                         f"&ids={qid}&props=sitelinks&sitefilter=enwiki&format=json"))
    title = ent["entities"][qid].get("sitelinks", {}).get("enwiki", {}).get("title")
    if not title:
        return None
    page = json.loads(get("https://en.wikipedia.org/api/rest_v1/page/summary/"
                          + urllib.parse.quote(title.replace(" ", "_"), safe="")))
    return {"text": page.get("extract"), "title": title, "source": "Wikipedia",
            "url": page["content_urls"]["desktop"]["page"]}


def about(urls):
    """A few sentences on the painting, or failing that on the painter."""
    for url in urls:
        try:
            if (found := wiki(url)) and found["text"]:
                return found
        except Exception as e:
            log(f"wikipedia lookup failed: {e}")
            return None
    return None


def query_string(query):
    return urllib.parse.urlencode({k: str(v).lower() if isinstance(v, bool) else v for k, v in query.items()})


def split_artist(display):
    """'Jean Charles Cazin (French, 1841–1901)' or 'Marc Chagall\nFrench, born Russia...' into
    name and bio, the way the Met splits them."""
    first, _, second = (display or "").partition("\n")
    name, _, bio = first.partition(" (")
    return name.strip(), bio.rstrip(")").strip() or second.strip()


assert split_artist("Jean Charles Cazin (French, 1841–1901)") == ("Jean Charles Cazin", "French, 1841–1901")
assert split_artist("James Rosenquist (American, 1933-2017)\nPublished by Gemini") == ("James Rosenquist", "American, 1933-2017")
assert split_artist("Marc Chagall\nFrench, born Belarus, 1887–1985") == ("Marc Chagall", "French, born Belarus, 1887–1985")


def text(html):
    return re.sub(r"<[^>]+>", "", html or "").strip() or None


def met(query, offset):
    found = json.loads(get(f"{MET}.1/search?{query_string(query)}&limit=1&offset={offset}"))
    if not found["objectIDs"]:
        return found["total"], None
    try:
        obj = json.loads(get(f"{MET}/objects/{found['objectIDs'][0]}"))
    except urllib.error.HTTPError:
        return found["total"], None  # the search index lists some objects that 404
    if not (obj.get("isPublicDomain") and obj.get("primaryImage")):
        return found["total"], None  # the Met has no image of anything under copyright
    m = ((obj.get("measurements") or [{}])[0].get("elementMeasurements") or {})
    return found["total"], {
        **{k: obj.get(k) for k in MET_KEYS},
        "tags": [t["term"] for t in (obj.get("tags") or [])][:6],
        "collection": "The Met",
        "image": obj["primaryImage"],
        # measurements can include the frame, so they only rule out, the photo decides
        "size": (m["Width"], m["Height"]) if m.get("Width") and m.get("Height") else None,
        "wikidata": [obj[k] for k in ("objectWikidata_URL", "artistWikidata_URL") if obj.get(k)],
    }


def aic(query, offset):
    """The Art Institute of Chicago. It serves copyrighted work too, but only 843 pixels wide.
    It pages no deeper than 1,000 results, so it shuffles instead and offset goes unused."""
    shuffled = {"function_score": {"query": query, "random_score": {"seed": random.randrange(1 << 30), "field": "id"},
                                   "boost_mode": "replace"}}
    body = json.dumps({"query": shuffled, "boost": False, "limit": 1, "fields": AIC_FIELDS}).encode()
    found = json.loads(get(f"{AIC}/artworks/search", body))
    total = found["pagination"]["total"]
    if not found["data"] or not found["data"][0].get("image_id"):
        return total, None
    a = found["data"][0]
    name, bio = split_artist(a.get("artist_display"))
    thumb = a.get("thumbnail") or {}
    desc = text(a.get("short_description")) or text(a.get("description"))
    url = f"https://www.artic.edu/artworks/{a['id']}"
    return total, {
        "objectID": a["id"], "title": a.get("title"), "artistDisplayName": name, "artistDisplayBio": bio,
        "objectDate": a.get("date_display"), "medium": a.get("medium_display"),
        "dimensions": a.get("dimensions"), "culture": a.get("place_of_origin"),
        "department": a.get("department_title"), "creditLine": a.get("credit_line"), "objectURL": url,
        "tags": [t for t in [a.get("style_title"), a.get("classification_title")] if t],
        "collection": "Art Institute of Chicago",
        "image": f"https://www.artic.edu/iiif/2/{a['image_id']}/full/1686,/0/default.jpg",
        "size": (thumb["width"], thumb["height"]) if thumb.get("width") else None,
        "about": desc and {"text": desc, "title": a.get("title"), "url": url, "source": "the Art Institute"},
    }


def cleveland(query, offset):
    found = json.loads(get(f"{CMA}?{query_string(query)}&limit=1&skip={offset}"))
    total = found["info"]["total"]
    if not found["data"]:
        return total, None
    a = found["data"][0]
    image = (a.get("images") or {}).get("print")
    if a.get("share_license_status") != "CC0" or not image:
        return total, None  # Cleveland only has images of its open access works
    name, bio = split_artist(((a.get("creators") or [{}])[0]).get("description"))
    desc = text(a.get("wikipedia_description")) or text(a.get("description"))
    return total, {
        "objectID": a["id"], "title": a.get("title"), "artistDisplayName": name, "artistDisplayBio": bio,
        "objectDate": a.get("creation_date"), "medium": a.get("technique"),
        "dimensions": a.get("measurements"), "culture": ", ".join(a.get("culture") or []),
        "department": a.get("department"), "creditLine": a.get("credit_line"), "objectURL": a.get("url"),
        "tags": [a["type"]] if a.get("type") else [],
        "collection": "Cleveland Museum of Art",
        "image": image["url"],
        "size": (int(image["width"]), int(image["height"])),
        "about": desc and {"text": desc, "title": a.get("title"), "url": a.get("url"), "source": "the Cleveland Museum of Art"},
    }


APIS = {"met": met, "aic": aic, "cleveland": cleveland}


def config():
    """sources.yaml, read on every pick so an edit applies to the next painting."""
    with open(CONFIG) as f:
        cfg = yaml.safe_load(f)
    cfg["sources"] = [s for s in cfg["sources"] if s.get("weight", 1) > 0]
    return cfg


def pick(tmp, source=None):
    """Download a random landscape painting from one of the sources, the frame hangs landscape."""
    cfg = config()
    if source is None:
        enabled = cfg["sources"]
        source = random.choices(enabled, [s.get("weight", 1) for s in enabled])[0]
    fetch, query = APIS[source["api"]], source.get("query") or {}
    total, _ = fetch(query, 0)
    for _ in range(40):
        # ponytail: the APIs page no deeper than 10,000 results, a bigger set is only sampled from its start
        _, obj = fetch(query, random.randrange(min(total, 10000)))
        if not obj or (obj["size"] and obj["size"][0] <= obj["size"][1]):
            continue
        path = os.path.join(tmp, "source.jpg")
        with open(path, "wb") as f:
            f.write(get(obj["image"]))
        w, h, colour = look(path)
        crop = 1 - min(w / h, PANEL) / max(w / h, PANEL)
        if w > h and crop <= cfg.get("max_crop", 1) and colour >= cfg.get("min_colourfulness", 0):
            obj["source"], obj["colourfulness"] = source["name"], round(colour)
            return path, obj
    raise RuntimeError(f"no colourful landscape painting that fits found in 40 tries from {source['name']}")


def install(tmp, src, meta, flags=()):
    """Convert src for the panel and make it today's picture."""
    # spectra6 sees a landscape source and writes 1600x1200, the board rotates it onto the panel
    subprocess.run([SPECTRA6, "-batch", *flags, "-out", tmp + "/out", src], check=True, capture_output=True)
    out = os.path.join(tmp, "out", os.listdir(os.path.join(tmp, "out"))[0])
    shutil.move(out, TODAY)
    # the web app shows the painting itself, not the version bent onto six inks
    with Image.open(src) as im:
        im.thumbnail((1600, 1600))
        im.convert("RGB").save(ORIGINAL, quality=90)
    with open(os.path.join(STATE, "today.json"), "w") as f:
        json.dump(meta, f, indent=2)


def refresh():
    with tempfile.TemporaryDirectory() as tmp:
        src, obj = pick(tmp)
        meta = {k: v for k, v in obj.items() if k not in ("image", "size", "wikidata")}
        meta["about"] = obj.get("about") or about(obj.get("wikidata", []))
        install(tmp, src, meta)
    log(f"new painting from {meta['source']}: {meta['title']} by {meta['artistDisplayName'] or 'unknown'}, "
        f"colourfulness {meta['colourfulness']}")


def upload(data, name):
    """Make an uploaded picture today's, whatever its shape."""
    title = os.path.splitext(name)[0] or "Upload"
    with lock, tempfile.TemporaryDirectory() as tmp:
        raw, src = os.path.join(tmp, "upload"), os.path.join(tmp, "source.jpg")
        with open(raw, "wb") as f:
            f.write(data)
        with Image.open(raw) as im:  # fails on anything that is not an image
            ImageOps.exif_transpose(im).convert("RGB").save(src, quality=95)  # spectra6 ignores EXIF rotation
        w, h, _ = look(src)
        # the frame hangs landscape, so a portrait picture is letterboxed onto it instead of cropped to a strip
        install(tmp, src, {"title": title, "source": "upload", "collection": "Upload"},
                () if w > h else ("-size", "1600x1200", "-contain"))
    log(f"uploaded {title}")


def ensure_today(force=False):
    """False if a new painting was needed and none could be had."""
    with lock:
        fresh = os.path.exists(TODAY) and \
            datetime.date.fromtimestamp(os.path.getmtime(TODAY)) == datetime.date.today()
        if force or not fresh:
            try:
                refresh()
            except Exception as e:
                log(f"refresh failed, serving previous image: {e}")
                return False
        return True


def due():
    """The board has something to draw: a picture from the web app, or the first of the day."""
    return os.path.exists(PENDING) or (iso(FETCHED) or "")[:10] != datetime.date.today().isoformat()


def sleep_for(now):
    """Seconds the board sleeps before its next check, POLL by day and until WAKE at night.
    The board's clock drifts a few percent in deep sleep, so each check asks again."""
    at = lambda hm: now.replace(hour=int(hm[:2]), minute=int(hm[3:]), second=0, microsecond=0)
    wake, bed = at(WAKE), at(BEDTIME)
    if wake <= now < bed:
        return POLL
    if now >= bed:
        wake += datetime.timedelta(days=1)
    return int((wake - now).total_seconds())


_d = datetime.datetime(2026, 9, 26)
if (WAKE, BEDTIME) == ("06:00", "23:00"):
    assert sleep_for(_d.replace(hour=12)) == POLL
    assert sleep_for(_d.replace(hour=23)) == 7 * 3600
    assert sleep_for(_d.replace(hour=5, minute=45)) == 15 * 60


# A LiPo's voltage says roughly how full it is. Rough is all anyone wants here.
CURVE = [(3.30, 0), (3.60, 10), (3.70, 20), (3.75, 30), (3.79, 40), (3.83, 50),
         (3.87, 60), (3.92, 70), (3.98, 80), (4.06, 90), (4.20, 100)]


def percent(volts):
    if volts <= CURVE[0][0]:
        return 0
    for (v1, p1), (v2, p2) in zip(CURVE, CURVE[1:]):
        if volts <= v2:
            return round(p1 + (p2 - p1) * (volts - v1) / (v2 - v1))
    return 100


def battery():
    try:
        with open(BOARD) as f:
            board = json.load(f)
    except OSError:
        return None
    return {"percent": percent(board["volts"]), **board}


def log(msg):
    now = datetime.datetime.now()
    bad = "fail" in msg.lower() or "error" in msg.lower() or re.search(r'" [45]\d\d ', msg)
    lines.append({"time": f"{now:%H:%M}", "msg": msg, "bad": bool(bad)})
    print(f"{now:%Y-%m-%d %H:%M:%S} {msg}", flush=True)


def status():
    """Everything the web app shows, in one response."""
    meta = {}
    try:
        with open(os.path.join(STATE, "today.json")) as f:
            meta = json.load(f)
    except OSError:
        pass
    keep = ("title", "artistDisplayName", "artistDisplayBio", "objectDate", "medium",
            "dimensions", "culture", "department", "creditLine", "objectURL", "tags", "about", "collection")
    return {
        **{k: meta.get(k) for k in keep},
        "picked": iso(TODAY),
        "battery": battery(),
        "last_fetch": iso(FETCHED),
        "last_check": checked[0] and checked[0].isoformat(),
        "next_check": checked[1] and checked[1].isoformat(),
        "pending": os.path.exists(PENDING),
        "log": list(lines)[::-1],
    }


def iso(path):
    try:
        return datetime.datetime.fromtimestamp(os.path.getmtime(path)).isoformat()
    except OSError:
        return None


class Handler(BaseHTTPRequestHandler):
    def do_GET(self):
        path = self.path.split("?")[0]
        if path in ("/", "/index.html"):
            return self.send(open(APP, "rb").read(), "text/html")
        if path == "/manifest.webmanifest":
            return self.send(json.dumps(MANIFEST).encode(), "application/manifest+json")
        if path in ICONS:
            with open(os.path.join(HERE, path[1:]), "rb") as f:
                return self.send(f.read(), ICONS[path])
        if path == "/board":
            return self.board()
        if path == "/pending":
            return self.pending()
        if path == "/status":
            return self.send(json.dumps(status()).encode(), "application/json")
        if path == "/original.jpg":
            if not os.path.exists(ORIGINAL):
                return self.send_error(404)
            with open(ORIGINAL, "rb") as f:
                return self.send(f.read(), "image/jpeg")
        if path not in ("/today.png", "/next.png"):
            return self.send_error(404)
        if path == "/next.png":
            log("another painting asked for")
        ensure_today(force=path == "/next.png")
        if not os.path.exists(TODAY):
            return self.send_error(503, "no image yet")
        with open(TODAY, "rb") as f:
            self.send(f.read(), "image/png")
        if self.from_board():  # a browser tab or a phone fetching its icon is not the frame waking up
            open(FETCHED, "w").close()
            if os.path.exists(PENDING):
                os.remove(PENDING)

    def do_POST(self):
        """The web app's buttons. Both answer once the picture is ready, the board draws it on its next check."""
        path = self.path.split("?")[0]
        if path == "/random":
            log("another painting asked for from the web app")
            if not ensure_today(force=True):
                return self.send_error(502, "no new painting, see the log")
        elif path == "/upload":
            size = int(self.headers.get("Content-Length") or 0)
            if not 0 < size <= MAX_UPLOAD_MB << 20:
                return self.send_error(413, f"pick an image under {MAX_UPLOAD_MB} MB")
            try:
                upload(self.rfile.read(size), urllib.parse.unquote(self.headers.get("X-Filename", "")))
            except Exception as e:
                log(f"upload failed: {e}")
                return self.send_error(400, "could not use that image")
        else:
            return self.send_error(404)
        open(PENDING, "w").close()
        self.send(json.dumps(status()).encode(), "application/json")

    def pending(self):
        """The board's check. It answers the seconds to sleep, 0 means fetch /today.png now."""
        now = datetime.datetime.now()
        secs = 0 if due() else sleep_for(now)
        checked[:] = now, now + datetime.timedelta(seconds=secs or POLL)
        self.send(str(secs).encode(), "text/plain")

    def board(self):
        """The board reports its battery when it wakes, before it fetches the image."""
        raw = urllib.parse.parse_qs(urlsplit(self.path).query).get("v", [""])[0]
        try:
            volts = float(raw)
        except ValueError:
            volts = 0.0
        if not 2.5 <= volts <= 4.4:  # no battery, or the divider misread
            log(f"battery reading out of range: {raw!r}")
            return self.send_error(400, "implausible voltage")
        with open(BOARD, "w") as f:
            json.dump({"volts": round(volts, 2), "at": datetime.datetime.now().isoformat()}, f)
        log(f"battery {percent(volts)}% ({volts:.2f}V)")
        self.send(b"ok", "text/plain")

    def send(self, body, kind):
        self.send_response(200)
        self.send_header("Content-Type", kind)
        self.send_header("Content-Length", str(len(body)))
        self.send_header("Cache-Control", "no-store")
        self.end_headers()
        self.wfile.write(body)

    def from_board(self):
        return (self.headers.get("User-Agent") or "").startswith("ESPHome")

    def log_message(self, fmt, *args):
        if not self.from_board() or self.path == "/pending":  # the web app polls, phones probe for icons, the board checks
            return
        log(f"{self.client_address[0]} {fmt % args}")


MANIFEST = {
    "name": "Art frame",
    "short_name": "Frame",
    "start_url": "/",
    "display": "standalone",
    "background_color": "#f6f2e9",
    "theme_color": "#f6f2e9",
    "icons": [
        {"src": "/icon-192.png", "sizes": "192x192", "type": "image/png", "purpose": "any maskable"},
        {"src": "/icon-512.png", "sizes": "512x512", "type": "image/png", "purpose": "any maskable"},
        {"src": "/icon.svg", "sizes": "any", "type": "image/svg+xml"},
    ],
}

checked = [None, None]  # when the board last asked /pending, and when it will ask again

if __name__ == "__main__":
    os.makedirs(STATE, exist_ok=True)
    if sys.argv[1:] == ["try"]:  # check sources.yaml after an edit
        for src in config()["sources"]:
            with tempfile.TemporaryDirectory() as tmp:
                _, obj = pick(tmp, src)
                print(f"{src['name']}: {obj['title']} by {obj['artistDisplayName']}, {obj['medium']}, "
                      f"colourfulness {obj['colourfulness']}\n  {obj['image']}")
        sys.exit()
    port = int(sys.argv[1] if len(sys.argv) > 1 else os.environ.get("FRAME_PORT") or 8765)
    log(f"serving on :{port}, spectra6 at {SPECTRA6}")
    ThreadingHTTPServer(("", port), Handler).serve_forever()
