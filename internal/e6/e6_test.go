package e6

import (
	"image"
	"image/color"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"testing"
)

func TestOkLabRoundTrip(t *testing.T) {
	for _, c := range [][3]float64{{1, 1, 1}, {0, 0, 0}, {0.5, 0.5, 0.5}, {0.8, 0.2, 0.1}, {0.1, 0.6, 0.9}} {
		got := OkLabToLinear(LinearToOkLab(c[0], c[1], c[2]))
		for i := range c {
			if math.Abs(got[i]-c[i]) > 1e-6 {
				t.Fatalf("round trip %v -> %v", c, got)
			}
		}
	}
	if l := LinearToOkLab(1, 1, 1); math.Abs(l.L-1) > 1e-6 || math.Abs(l.A) > 1e-6 {
		t.Fatalf("white should be L=1 on the neutral axis, got %+v", l)
	}
}

func TestSRGBTransferRoundTrip(t *testing.T) {
	for v := 0.0; v <= 1.0; v += 0.01 {
		if got := LinearToSRGB(SRGBToLinear(v)); math.Abs(got-v) > 1e-9 {
			t.Fatalf("sRGB round trip at %g gave %g", v, got)
		}
	}
}

// Hue must beat lightness when matching, or saturated pixels collapse onto black
// and white and the panel never uses its colour inks.
func TestNearestPrefersHueOverLightness(t *testing.T) {
	pal := []OkLab{
		LinearToOkLab(0.10, 0.012, 0.012), // darker red
		LinearToOkLab(0.30, 0.30, 0.30),   // matching lightness grey
	}
	target := LinearToOkLab(0.30, 0.04, 0.04)
	if got := Nearest(target, pal, ChromaWeight); got != 0 {
		t.Fatalf("medium red matched grey instead of red")
	}
}

func TestHullChromaIsPositiveAndBounded(t *testing.T) {
	hues := []float64{0, math.Pi / 2, math.Pi, -math.Pi / 2}
	for i, r := range HullChroma(Measured, hues) {
		if r <= 0 || r > 0.4 {
			t.Fatalf("hue %d reach %g is out of range", i, r)
		}
	}
}

func solidFrame(n int, r, g, b float64) *Frame {
	f := NewFrame(n, 1)
	for i := range f.Px {
		f.Px[i] = [3]float64{SRGBToLinear(r), SRGBToLinear(g), SRGBToLinear(b)}
	}
	return f
}

// The whole point of the analyser: lift what the panel can reach, leave alone what
// it cannot.
func TestAnalyseBoostsMutedLeavesVivid(t *testing.T) {
	dull, _ := Analyse(solidFrame(4000, 0.55, 0.48, 0.46), Measured, 1.6, 90, 1)
	vivid, _ := Analyse(solidFrame(4000, 0.85, 0.05, 0.05), Measured, 1.6, 90, 1)
	if maxOf(dull) <= 1.2 {
		t.Fatalf("muted image was not boosted, max gain %g", maxOf(dull))
	}
	if maxOf(vivid) >= 1.05 {
		t.Fatalf("already saturated image was boosted, max gain %g", maxOf(vivid))
	}
}

func TestAnalyseScaleZeroDisablesBoost(t *testing.T) {
	gains, _ := Analyse(solidFrame(4000, 0.55, 0.48, 0.46), Measured, 1.6, 90, 0)
	if maxOf(gains) != 1 {
		t.Fatalf("scale 0 should leave every gain at 1, got %g", maxOf(gains))
	}
}

func TestApplyGainsMovesChromaOnly(t *testing.T) {
	f := solidFrame(64, 150.0/255, 90.0/255, 70.0/255)
	before := LinearToOkLab(f.Px[0][0], f.Px[0][1], f.Px[0][2])
	gains := make([]float64, HueBins)
	for i := range gains {
		gains[i] = 1.4
	}
	ApplyGains(f, gains)
	after := LinearToOkLab(f.Px[0][0], f.Px[0][1], f.Px[0][2])

	if math.Abs(after.L-before.L) > 0.01 {
		t.Fatalf("lightness moved: %g -> %g", before.L, after.L)
	}
	if d := math.Abs(angleDiff(after.Hue(), before.Hue())); d > 0.02 {
		t.Fatalf("hue moved by %g rad", d)
	}
	if ratio := after.Chroma() / before.Chroma(); ratio < 1.3 || ratio > 1.5 {
		t.Fatalf("chroma gain was %g, wanted about 1.4", ratio)
	}
}

func TestApplyGainsLeavesNeutralsAlone(t *testing.T) {
	f := solidFrame(16, 0.5, 0.5, 0.5)
	gains := make([]float64, HueBins)
	for i := range gains {
		gains[i] = 2.0
	}
	before := f.Px[0]
	ApplyGains(f, gains)
	for c := 0; c < 3; c++ {
		if math.Abs(f.Px[0][c]-before[c]) > 1e-9 {
			t.Fatalf("a neutral pixel was boosted: %v -> %v", before, f.Px[0])
		}
	}
}

func TestDitherEmitsOnlyPaletteIndices(t *testing.T) {
	f := NewFrame(64, 64)
	for y := 0; y < 64; y++ {
		for x := 0; x < 64; x++ {
			f.Px[y*64+x] = [3]float64{float64(x) / 63, float64(y) / 63, 0.5}
		}
	}
	for _, m := range Modes() {
		idx := Dither(f.Clone(), Measured, Mode(m), true)
		if len(idx) != 64*64 {
			t.Fatalf("%s returned %d indices", m, len(idx))
		}
		for _, v := range idx {
			if int(v) >= len(Measured) {
				t.Fatalf("%s emitted index %d, outside the six inks", m, v)
			}
		}
	}
}

// Dithering a flat mid grey has to actually mix inks. If it silently picked one
// ink for the whole area the output would be a solid block.
func TestDitherMixesInksOnFlatGrey(t *testing.T) {
	f := solidFrameSize(64, 64, 0.5)
	seen := map[uint8]bool{}
	for _, v := range Dither(f, Measured, ModeBurkes, true) {
		seen[v] = true
	}
	if len(seen) < 2 {
		t.Fatalf("flat grey dithered to a single ink: %v", seen)
	}
}

func solidFrameSize(w, h int, v float64) *Frame {
	f := NewFrame(w, h)
	for i := range f.Px {
		f.Px[i] = [3]float64{SRGBToLinear(v), SRGBToLinear(v), SRGBToLinear(v)}
	}
	return f
}

func TestGamutCompressLeavesPaletteColoursAlone(t *testing.T) {
	lin := Measured.Linear()
	f := NewFrame(1, 1)
	f.Px[0] = lin[Red]
	GamutCompress(f, Measured, 1)
	for c := 0; c < 3; c++ {
		if math.Abs(f.Px[0][c]-lin[Red][c]) > 1e-5 {
			t.Fatalf("an exact ink colour was moved: %v -> %v", lin[Red], f.Px[0])
		}
	}
}

func TestCompressDynamicRangeHitsPanelWhite(t *testing.T) {
	f := NewFrame(1, 1)
	f.Px[0] = [3]float64{1, 1, 1}
	CompressDynamicRange(f, Measured, 1)
	want := Luminance(Measured.Linear()[White])
	if got := Luminance(f.Px[0]); math.Abs(got-want) > 1e-6 {
		t.Fatalf("full strength compression put white at %g, wanted %g", got, want)
	}
}

func TestAutoToneSkipsImagesThatAlreadyFit(t *testing.T) {
	lin := Measured.Linear()
	black, white := Luminance(lin[Black]), Luminance(lin[White])
	lo := black + 0.05*(white-black)
	hi := white - 0.05*(white-black)
	f := NewFrame(3, 1)
	f.Px[0] = [3]float64{lo, lo, lo}
	f.Px[1] = [3]float64{(lo + hi) / 2, (lo + hi) / 2, (lo + hi) / 2}
	f.Px[2] = [3]float64{hi, hi, hi}
	before := append([][3]float64(nil), f.Px...)
	AutoCompressDynamicRange(f, Measured)
	for i := range before {
		for c := 0; c < 3; c++ {
			if math.Abs(f.Px[i][c]-before[i][c]) > 1e-9 {
				t.Fatalf("in range image was modified at pixel %d", i)
			}
		}
	}
}

// Regression: a high key image makes the log average overshoot, which used to
// produce a NaN strength and poison every pixel.
func TestAutoToneSurvivesHighKeyImages(t *testing.T) {
	f := NewFrame(100, 1)
	for i := 0; i < 98; i++ {
		v := 0.7 + 0.3*float64(i)/97
		f.Px[i] = [3]float64{v, v, v}
	}
	f.Px[98] = [3]float64{0.0004, 0.0004, 0.0004}
	f.Px[99] = [3]float64{0.0004, 0.0004, 0.0004}
	AutoCompressDynamicRange(f, Measured)
	for i, p := range f.Px {
		for c := 0; c < 3; c++ {
			if math.IsNaN(p[c]) {
				t.Fatalf("pixel %d went NaN", i)
			}
		}
	}
}

func TestFitOrientationAndLetterbox(t *testing.T) {
	wide := image.NewNRGBA(image.Rect(0, 0, 400, 200))
	if b := Fit(wide, 1200, 1600, false).Bounds(); b.Dx() != 1600 || b.Dy() != 1200 {
		t.Fatalf("landscape source should flip the panel, got %v", b)
	}
	tall := image.NewNRGBA(image.Rect(0, 0, 200, 400))
	if b := Fit(tall, 1200, 1600, true).Bounds(); b.Dx() != 1200 || b.Dy() != 1600 {
		t.Fatalf("letterbox should fill the panel, got %v", b)
	}
}

func TestIsGraphic(t *testing.T) {
	flat := image.NewNRGBA(image.Rect(0, 0, 200, 200))
	for y := 0; y < 200; y++ {
		for x := 0; x < 200; x++ {
			c := color.NRGBA{200, 30, 30, 255}
			if x < 100 {
				c = color.NRGBA{30, 30, 200, 255}
			}
			flat.SetNRGBA(x, y, c)
		}
	}
	if !IsGraphic(flat) {
		t.Fatal("flat two colour art was not detected as a graphic")
	}

	rng := rand.New(rand.NewSource(1))
	noise := image.NewNRGBA(image.Rect(0, 0, 200, 200))
	for i := range noise.Pix {
		noise.Pix[i] = uint8(rng.Intn(256))
	}
	for i := 3; i < len(noise.Pix); i += 4 {
		noise.Pix[i] = 255
	}
	if IsGraphic(noise) {
		t.Fatal("noise was detected as a graphic")
	}
}

// Calibration is only meaningful if the paper reference really does divide the
// lighting back out.
func TestNormaliseUndoesLighting(t *testing.T) {
	paper := RGB{215, 217, 218}
	var raw Palette
	for i, c := range Measured {
		for k := 0; k < 3; k++ {
			raw[i][k] = uint8(float64(c[k]) * float64(paper[k]) / 255)
		}
	}
	got, err := Normalise(raw, paper, false)
	if err != nil {
		t.Fatal(err)
	}
	for i := range got {
		for k := 0; k < 3; k++ {
			if diff := int(got[i][k]) - int(Measured[i][k]); diff > 2 || diff < -2 {
				t.Fatalf("%s channel %d came back as %d, wanted %d",
					InkNames[i], k, got[i][k], Measured[i][k])
			}
		}
	}

	lit, err := Normalise(raw, paper, true)
	if err != nil {
		t.Fatal(err)
	}
	if lit[White][0] <= got[White][0] {
		t.Fatal("linear input should be gamma encoded on the way in, so it reads brighter")
	}
}

func TestNormaliseRejectsZeroPaper(t *testing.T) {
	if _, err := Normalise(Measured, RGB{0, 200, 200}, false); err == nil {
		t.Fatal("a zero paper channel must be rejected, not divided by")
	}
}

func TestSamplePatchesReadsBackTheTarget(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "target.png")
	if err := Patches(path, 120, 180); err != nil {
		t.Fatal(err)
	}
	got, err := SamplePatches(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != Ideal {
		t.Fatalf("sampled %v, wanted %v", got, Ideal)
	}
}

func TestPaletteJSONRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "palette.json")
	if err := SavePalette(Measured, path); err != nil {
		t.Fatal(err)
	}
	got, err := LoadPalette(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != Measured {
		t.Fatalf("loaded %v, wanted %v", got, Measured)
	}
}

func TestLoadPaletteRejectsBadFiles(t *testing.T) {
	dir := t.TempDir()
	partial := filepath.Join(dir, "partial.json")
	os.WriteFile(partial, []byte(`{"black":[0,0,0]}`), 0o644)
	if _, err := LoadPalette(partial); err == nil {
		t.Fatal("a palette missing five inks must be rejected")
	}
	bad := filepath.Join(dir, "bad.json")
	os.WriteFile(bad, []byte(`{"black":[0,0,999],"white":[1,1,1],"yellow":[1,1,1],"red":[1,1,1],"blue":[1,1,1],"green":[1,1,1]}`), 0o644)
	if _, err := LoadPalette(bad); err == nil {
		t.Fatal("an out of range channel must be rejected")
	}
}

func TestParseStrengthAndMode(t *testing.T) {
	for _, in := range []string{"auto", "off", "0.5", "1"} {
		if _, err := ParseStrength(in); err != nil {
			t.Fatalf("%q should parse: %v", in, err)
		}
	}
	for _, in := range []string{"1.5", "-1", "banana"} {
		if _, err := ParseStrength(in); err == nil {
			t.Fatalf("%q should not parse", in)
		}
	}
	if _, err := ParseMode("burkes"); err != nil {
		t.Fatal(err)
	}
	if _, err := ParseMode("nope"); err == nil {
		t.Fatal("unknown mode should be rejected")
	}
}

// End to end: a real file in, palette indices out, with the analysis filled in.
func TestRunEndToEnd(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "in.png")
	img := image.NewNRGBA(image.Rect(0, 0, 90, 120))
	rng := rand.New(rand.NewSource(2))
	for y := 0; y < 120; y++ {
		for x := 0; x < 90; x++ {
			img.SetNRGBA(x, y, color.NRGBA{
				uint8(x * 2), uint8(y), uint8(rng.Intn(256)), 255})
		}
	}
	if err := Save(img, src); err != nil {
		t.Fatal(err)
	}

	prm := Defaults()
	prm.Width, prm.Height = 90, 120
	res, fitted, err := Run(src, Measured, prm)
	if err != nil {
		t.Fatal(err)
	}
	if res.W != 90 || res.H != 120 {
		t.Fatalf("got %dx%d", res.W, res.H)
	}
	if len(res.Indices) != 90*120 {
		t.Fatalf("got %d indices", len(res.Indices))
	}
	if fitted.Bounds().Dx() != 90 {
		t.Fatalf("fitted image is %v", fitted.Bounds())
	}
	if len(res.Report.Lines("in.png")) == 0 {
		t.Fatal("report produced no lines")
	}

	out := filepath.Join(dir, "out.png")
	if err := Save(Paletted(res.Indices, res.W, res.H, Ideal), out); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(out); err != nil {
		t.Fatal(err)
	}
}

func maxOf(v []float64) float64 {
	out := v[0]
	for _, x := range v {
		if x > out {
			out = x
		}
	}
	return out
}

// blockError averages the dithered result over tiles and compares each tile to
// the same tile of the source, in OkLab. Tile averaging is what the eye does at
// viewing distance, so this measures the thing that actually matters.
func blockError(t *testing.T, src *Frame, indices []uint8, p Palette, tile int) float64 {
	t.Helper()
	lin := p.Linear()
	var total float64
	var n int
	for ty := 0; ty+tile <= src.H; ty += tile {
		for tx := 0; tx+tile <= src.W; tx += tile {
			var want, got [3]float64
			for y := ty; y < ty+tile; y++ {
				for x := tx; x < tx+tile; x++ {
					i := y*src.W + x
					for c := 0; c < 3; c++ {
						want[c] += src.Px[i][c]
						got[c] += lin[indices[i]][c]
					}
				}
			}
			cells := float64(tile * tile)
			for c := 0; c < 3; c++ {
				want[c] /= cells
				got[c] /= cells
			}
			a := LinearToOkLab(want[0], want[1], want[2])
			b := LinearToOkLab(got[0], got[1], got[2])
			dl, da, db := a.L-b.L, a.A-b.A, a.B-b.B
			total += math.Sqrt(dl*dl + da*da + db*db)
			n++
		}
	}
	return total / float64(n)
}

// Error diffusion has to earn its keep: averaged over tiles, the way the eye
// averages at viewing distance, it should land much closer to the source than
// plain nearest colour does.
//
// This checks that every kernel is wired up and diffusing, not that the diffusion
// is bounded. Whether it stays bounded is what TestGoldenInkMix guards, since a
// runaway shows up as the ink mix shifting rather than as tile averaged error.
func TestDitherStaysCloseToTheSource(t *testing.T) {
	const w, h = 128, 128
	// Three sources a panel actually gets: a neutral ramp, a warm ramp, and a sky
	// gradient. All are inside or near the gamut after the usual preparation, so
	// error diffusion has room to do its job and its failure is unambiguous.
	sources := map[string]func(x, y int) (float64, float64, float64){
		"grey ramp": func(x, _ int) (float64, float64, float64) {
			v := float64(x) / (w - 1)
			return v, v, v
		},
		"warm ramp": func(x, _ int) (float64, float64, float64) {
			v := float64(x) / (w - 1)
			return v, v*0.92 + 0.04, v*0.85 + 0.05
		},
		"sky": func(_, y int) (float64, float64, float64) {
			v := 0.35 + 0.5*float64(y)/(h-1)
			return v * 0.75, v * 0.85, v
		},
	}

	for name, gen := range sources {
		src := NewFrame(w, h)
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				r, g, b := gen(x, y)
				src.Px[y*w+x] = [3]float64{SRGBToLinear(r), SRGBToLinear(g), SRGBToLinear(b)}
			}
		}
		// The panel can only be judged after the image is mapped into its range,
		// which is what the real pipeline does before dithering.
		CompressDynamicRange(src, Measured, 1)
		GamutCompress(src, Measured, 1)

		// Plain nearest colour, no diffusion at all, is the baseline. Comparing
		// against it keeps this self calibrating, with no magic threshold to
		// keep up to date as the palette or the tone curve changes.
		baseline := blockError(t, src, Dither(src.Clone(), Measured, ModeNone, false), Measured, 8)
		if baseline <= 0 {
			t.Fatalf("%s: baseline error should be positive on a six colour palette", name)
		}

		for _, m := range []Mode{ModeFloydSteinberg, ModeBurkes, ModeAtkinson, ModeStucki,
			ModeSierra, ModeSierraLite, ModeJarvis} {
			got := blockError(t, src, Dither(src.Clone(), Measured, m, true), Measured, 8)
			if got >= baseline*0.75 {
				t.Errorf("%s / %s: tile averaged error %.4f against a no dither baseline "+
					"of %.4f, this kernel is barely diffusing", name, m, got, baseline)
			}
		}
	}
}

// A bright flat field is the worst case for runaway, since every pixel generates
// error the palette cannot absorb.
func TestDitherDoesNotBlowUpOnBrightFlatFields(t *testing.T) {
	f := solidFrameSize(96, 96, 0.95)
	CompressDynamicRange(f, Measured, 1)
	counts := map[uint8]int{}
	for _, v := range Dither(f.Clone(), Measured, ModeBurkes, true) {
		counts[v]++
	}
	// It should be almost all white paper, with at most a light sprinkle of
	// anything else. A runaway shows up as a large share of a saturated ink.
	for ink, n := range counts {
		share := float64(n) / float64(96*96)
		if ink != White && share > 0.15 {
			t.Fatalf("%s took %.0f%% of a near white field", InkNames[ink], share*100)
		}
	}
}

// The -bmp flag exists for loaders that only read 24 bit BMP, so a paletted
// image must be flattened before encoding, not written colour mapped.
func TestSaveBMPWrites24Bit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "out.bmp")
	if err := Save(Paletted(make([]uint8, 16), 4, 4, Ideal), path); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// Bits per pixel lives at byte 28 of the header.
	if bpp := int(raw[28]) | int(raw[29])<<8; bpp != 24 {
		t.Fatalf("BMP was written at %d bits per pixel, wanted 24", bpp)
	}
}

// Regression: a bright image reads as needing almost no compression by its log
// average, so its sky stayed above panel white and printed as flat white.
func TestAutoToneBringsHighKeyImagesIntoRange(t *testing.T) {
	lin := Measured.Linear()
	white := Luminance(lin[White])
	f := NewFrame(100, 1)
	for i := range f.Px {
		v := 0.6 + 0.4*float64(i)/99
		f.Px[i] = [3]float64{v, v, v}
	}
	AutoCompressDynamicRange(f, Measured)
	if y := Luminance(f.Px[50]); y > white {
		t.Fatalf("mid pixel stayed above panel white, %.3f > %.3f", y, white)
	}
}

func TestLightChromaLeavesDarkColoursAlone(t *testing.T) {
	f := NewFrame(2, 1)
	f.Px[0] = OkLabToLinear(OkLab{L: 0.30, A: 0.05, B: 0.05})
	f.Px[1] = OkLabToLinear(OkLab{L: Measured.Lab()[White].L, A: 0.05, B: 0.05})
	before := append([][3]float64(nil), f.Px...)
	LightChroma(f, Measured, 0.5)
	for c := 0; c < 3; c++ {
		if math.Abs(f.Px[0][c]-before[0][c]) > 1e-6 {
			t.Fatalf("dark colour changed: %v -> %v", before[0], f.Px[0])
		}
	}
	lo := LinearToOkLab(before[1][0], before[1][1], before[1][2]).Chroma()
	hi := LinearToOkLab(f.Px[1][0], f.Px[1][1], f.Px[1][2]).Chroma()
	if hi <= lo*1.2 {
		t.Fatalf("light colour barely moved, chroma %.3f -> %.3f", lo, hi)
	}
}
