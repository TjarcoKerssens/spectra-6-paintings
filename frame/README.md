# Art frame

A random painting on the 13.3 inch EE02 every morning.

A server on your network runs `daily_art.py`, a small HTTP server. The first request of the day picks a random landscape painting from one of the museums in `sources.yaml`, runs it through `spectra6` and caches it. The board runs ESPHome. Between 06:00 and 23:00 it wakes every 10 minutes and asks `/pending` whether there is something new to draw. The server answers with the number of seconds to sleep, or 0 when the board should download `/today.png`. That happens on the first check of the day and whenever the web app has a new picture ready. At night the server tells it to sleep until 06:00. If wifi or the download fails it tries again an hour later. Press KEY2 to wake it and swap today's painting for another one. It asks for `/next.png`, the server picks a new painting and the board draws it and sleeps again. That painting stays up until the next morning. The board pulls instead of the server pushing, because a board that waits for a push has to keep wifi on and the battery would be gone in a day.

A check costs the battery about 0.05 mAh. The board joins wifi, does one small GET and sleeps again, all in a second or two. That is why it has a fixed IP and `fast_connect`, DHCP and the network scan would double every check. A full redraw costs about 2 mAh. With 10 minute checks by day the 2000 mAh pack should last around eight months, against more than a year with one wake a day. `FRAME_WAKE` and `FRAME_BEDTIME` in `.env` set the schedule, a restart of the server is enough. `FRAME_POLL` sets the time between checks. The board reads it too, as the sleep right after a redraw, so reflash after changing it.

## What you need

- A Seeed Studio [XIAO EE02](https://wiki.seeedstudio.com/) board with the 13.3 inch Spectra 6 panel and its LiPo pack. The numbers here are for a 2000 mAh pack.
- A computer on the same network that stays on, for the server. A Mac or a Linux box like a Raspberry Pi.
- A USB C cable for the first flash.

## The web app

<img src="../docs/webapp.jpg" width="360" alt="The web app">

Open `http://<server ip>:8765/` on a phone on the same wifi. It shows today's painting with a wall label, when the image was picked, when the board last pulled it, when it last checked and checks next, and the recent log with failures marked. The checks themselves stay out of the log, there are nearly two hundred a day.

"Another painting" picks a new one from the sources. "Upload" takes a picture from your phone, HEIC included. A portrait picture is letterboxed onto white, since the frame hangs landscape. Both take a minute or so to convert, then the frame draws the result at its next check, within 10 minutes. The picture stays up until the next morning, like one picked with KEY2. "Add to Home Screen" on iOS or "Install" on Android gives it its own icon.

"Favorite" keeps what is on the frame now, both the ink version the board draws and the original. The favorites show up as a gallery under the buttons, with the original as thumbnail. Tap one and "Put on the frame" sends it back to the board. It was converted when it was kept, so this is instant and the frame draws it at its next check. It stays up until the next morning, like an upload. "Add a picture" under Favorites takes a picture from your phone, converts it like an upload and keeps it, without touching what is on the frame now. Tapping "Favorited" again, or "Remove" in the gallery, deletes a favorite.

The picture is the museum's own photograph, not the version bent onto six inks, since the ink version is made for the panel and looks wrong on a screen. A downscaled copy of the source is kept next to the converted one.

"More" opens a description of the painting. The Art Institute and Cleveland have their own. For the Met it is the first paragraph of the Wikipedia article about the painting, or about the painter when the painting has no article of its own. The Met API carries no description and its website answers 429 to a plain fetch, so the text comes from the Wikidata id on the object. If the lookup fails the box stays hidden and a line lands in the log.

The battery line is whatever the board last reported. It reads the pack through the divider on GPIO1, which GPIO6 switches on for the measurement, and posts the voltage to `/board` right after it connects, before it fetches the image. It only does that on a wake that draws a picture, not on every check, so the number is from the last redraw. That is at least once a day, which is as often as anyone needs it. The percentage is a rough read off a LiPo discharge curve, not a fuel gauge.

If the voltage looks wrong, measure the pack with a multimeter while the board is awake and change `FRAME_BATTERY_MULTIPLY` in `.env` and reflash, it is the divider ratio. A board on USB with no pack reports something implausible, the server refuses it with a 400 and logs a line.

The log in the page is the last 200 lines kept in memory, so it starts empty after a restart. The full history stays in the service log, see step 7 below.

## The paintings

They come from three museum APIs, none needs a key. [The Met](https://metmuseum.github.io/), [the Art Institute of Chicago](https://api.artic.edu/docs/) and [the Cleveland Museum of Art](https://openaccess-api.clevelandart.org/). `sources.yaml` lists which searches to draw from and how often. Every pick draws one source by weight, then a random painting from it. The file is read on every pick, so an edit needs no restart. After an edit, `uv run daily_art.py try` shows one pick from every source.

The Met and Cleveland only have images of public domain work. The Art Institute serves copyrighted work too, Warhol and Lichtenstein included, but only 843 pixels wide, about half the panel. The dither hides some of that.

Portrait paintings are skipped entirely, first on the catalogue measurements and then on the real pixel size of the photo, since the measurements sometimes include the frame. Dull paintings are skipped too. Every download gets a colourfulness score, the Hasler and Süsstrunk metric, and anything under `min_colourfulness` is passed over. The panel makes a muted painting look bleaker still, and ink wash on beige paper turns into yellow and red noise. The log shows the score of every painting that made it, raise the minimum for more pop. The panel is 4:3 and `spectra6` crops to fill it, so a square or very wide painting loses its edges. Anything that would lose more than `max_crop` of its picture is skipped, 15% by default.

Everything about today lives in `FRAME_STATE_DIR`, `~/.local/share/spectra6-frame` by default: `today.png` for the board, `original.jpg` for the web app, `today.json` with the title, artist, medium, credit line and the description, `board.json` with the last battery reading, and `favorites/` with one folder per favorite holding `frame.png` for the board, `original.jpg`, `thumb.jpg` and `meta.json` with its label. A favorite keeps the ink version it was converted with, so a later calibration does not change it.

`spectra6` sees a landscape source and writes a 1600x1200 image. The board rotates it onto the panel, which is 1200x1600 natively. If the painting shows up upside down, switch `FRAME_ROTATION` in `.env` between `270` and `90` and reflash.

## Setting up the server

The server is any computer that stays on and sits on the same network as the board. A Mac or a Linux box like a Raspberry Pi both work. Do this once, on that machine.

1. Keep it awake. On a Mac, go to System Settings, Energy, and turn on "Prevent automatic sleeping when the display is off", or run `sudo pmset -a sleep 0`. A Linux server usually stays awake already.
2. Give it a fixed address. Reserve its IP in your router's DHCP settings, the board has the address baked in. `ipconfig getifaddr en0` shows the current one on a Mac, `hostname -I` on Linux.
3. Install Go 1.26, [uv](https://docs.astral.sh/uv/) and ESPHome.

   On a Mac

   ```
   xcode-select --install
   brew install go
   curl -LsSf https://astral.sh/uv/install.sh | sh
   uv tool install esphome
   ```

   On Linux, get Go from [go.dev/dl](https://go.dev/dl/) if your distribution ships an older one, then

   ```
   curl -LsSf https://astral.sh/uv/install.sh | sh
   uv tool install esphome
   ```

4. Get the repo and install `spectra6` into `~/bin`.

   ```
   git clone https://github.com/TjarcoKerssens/spectra-6-paintings.git
   cd spectra-6-paintings
   make install
   ```

5. Copy `.env.example` to `.env` and fill it in. The server part works as is. The board part needs your wifi, the server's address from step 2 and a fixed address for the board.

   ```
   cp .env.example .env
   ```

   Pick `FRAME_IP` outside your router's DHCP pool, or reserve it there for the board. `FRAME_GATEWAY` is the router's address. `.env` is gitignored, so the wifi password stays out of git.

6. If you calibrated your panel on another machine, copy `~/.config/spectra6/palette.json` over. Without it the built in palette is used, which is close but not exact.
7. Start the server as a service. It starts on boot or login and restarts if it crashes. The first request takes a minute or two, it downloads a painting and converts it.

   On a Mac, as a launchd agent

   ```
   cd frame
   sed -e "s|__DIR__|$PWD|" -e "s|__HOME__|$HOME|g" spectra6-frame.plist > ~/Library/LaunchAgents/spectra6-frame.plist
   launchctl load ~/Library/LaunchAgents/spectra6-frame.plist
   curl -o /tmp/today.png localhost:8765/today.png && open /tmp/today.png
   ```

   Allow incoming connections for Python when the firewall asks, otherwise the board and your phone reach nothing. Logs go to `~/Library/Logs/spectra6-frame.log`.

   On Linux, as a systemd user service. `enable-linger` keeps it running when you are not logged in.

   ```
   cd frame
   mkdir -p ~/.config/systemd/user
   sed -e "s|__DIR__|$PWD|" -e "s|__HOME__|$HOME|g" spectra6-frame.service > ~/.config/systemd/user/spectra6-frame.service
   systemctl --user daemon-reload
   systemctl --user enable --now spectra6-frame
   sudo loginctl enable-linger $USER
   curl -o /tmp/today.png localhost:8765/today.png
   ```

   Logs go to `journalctl --user -u spectra6-frame`. Open port 8765 if you run a firewall.

   Then open `http://<server ip>:8765/` on your phone to check it from another machine, and add it to your home screen.

8. Flash the board. Plug it into the server over USB, press KEY1 to keep it awake, then from the root of the repo run

   ```
   make frame-upload
   ```

   It compiles the firmware with the settings from `.env` and uploads it to `FRAME_DEVICE`. The first build downloads the ESP-IDF toolchain and takes a few minutes. The port name differs per machine. `ls /dev/cu.usbmodem*` shows it on a Mac, `ls /dev/ttyACM*` on Linux, where your user may need to be in the `dialout` group.

The board keeps working if the server is off for a day. The download fails and it tries again every hour until the server is back.

To move the server to another machine, set it up there, change `FRAME_SERVER`, reflash the board and stop the old service.

## Updating the server

After pulling a change to `daily_art.py`, restart the service. uv installs any new Python packages on the next start.

```
git pull
launchctl kickstart -k gui/$(id -u)/spectra6-frame   # Mac
systemctl --user restart spectra6-frame              # Linux
```

`app.html` is read on every request, so a change to the page alone only needs a reload in the browser. Today's painting is not picked again by a restart, the cached one keeps being served until tomorrow.

## Flashing the board

The board sleeps nearly all the time, and while it sleeps it is off wifi and its USB port is gone. Press KEY1 to wake it. It draws today's painting and then stays awake for 10 minutes, and pressing KEY1 again restarts the 10 minutes. After that it goes back to its checks on its own. RESET also wakes it, but then it only does a normal check.

While it is awake, flash it over USB with `make frame-upload`. To flash over wifi instead, set `FRAME_DEVICE` to `art-frame.local`, or whatever `FRAME_NAME` is. On a Mac, wifi flashing only works once macOS lets Python talk to the local network. Without that it fails with `No route to host` even though the board answers ping. Turn it on under System Settings, Privacy & Security, Local Network, for the terminal app you run esphome from.

Only one program can hold the USB port. If an upload fails with `multiple access on port`, close any serial monitor first. `make frame-config` prints the config with `.env` filled in, without building anything.

The display driver is [esphome-epaper-spectra6-133](https://github.com/philippwaller/esphome-epaper-spectra6-133). It maps every pixel to the nearest ink, and `spectra6` writes the exact ink colours, so nothing shifts.

## Going back to SenseCraft

Take a full flash dump before the first ESPHome flash, so you can go back to the stock firmware. Press KEY1 and run

```
mkdir -p frame/backup
esptool --port /dev/cu.usbmodem101 read-flash 0 ALL frame/backup/ee02-sensecraft.bin
```

`frame/backup` is gitignored, since the dump holds your wifi password and SenseCraft keys. To restore it, press KEY1 and run

```
esptool --port /dev/cu.usbmodem101 write-flash 0 frame/backup/ee02-sensecraft.bin
```
