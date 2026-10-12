package e6

import (
	"fmt"
	"image"
	"strconv"
	"strings"
)

// Strength is a knob that can be off, a fixed amount, or left to the analyser.
type Strength struct {
	Auto  bool
	Value float64
}

// ParseStrength reads "auto", "off", or a number in [0,1].
func ParseStrength(s string) (Strength, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "auto":
		return Strength{Auto: true}, nil
	case "off", "":
		return Strength{}, nil
	}
	v, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return Strength{}, fmt.Errorf("want auto, off or a number, got %q", s)
	}
	if v < 0 || v > 1 {
		return Strength{}, fmt.Errorf("strength must be between 0 and 1, got %g", v)
	}
	return Strength{Value: v}, nil
}

func (s Strength) String() string {
	if s.Auto {
		return "auto"
	}
	if s.Value == 0 {
		return "off"
	}
	return strconv.FormatFloat(s.Value, 'g', -1, 64)
}

// Params is everything the conversion needs beyond the image and the palette.
type Params struct {
	Width, Height int
	Contain       bool
	Content       string // auto, photo or graphic
	Dither        Mode
	Serpentine    bool
	Sat           float64  // scales the computed per hue boost, 0 disables it
	MaxGain       float64  // ceiling on any one hue's boost
	Vivid         float64  // flat chroma multiplier after tone compression, 1 leaves it
	Light         float64  // extra chroma for colours near panel white, 0 leaves them
	KeepHue       float64  // how far lightness gives way to keep an unreachable hue, 0 is off
	TargetPct     float64  // which chroma percentile is aimed at the gamut edge
	Sharp         float64  // unsharp amount
	Shadows       *float64 // nil means take the analyser's suggestion
	Highlights    float64
	Tone          Strength
	Gamut         Strength
	PaletteOut    string // ideal or measured
}

// Defaults are deliberately mild. The per hue analyser does the heavy lifting, so
// the fixed knobs only need to stay out of its way.
func Defaults() Params {
	return Params{
		Width: 1200, Height: 1600,
		Content:    "auto",
		Dither:     ModeBurkes,
		Serpentine: true,
		Sat:        1.0,
		MaxGain:    1.6,
		Vivid:      1.15,
		Light:      0.35,
		KeepHue:    0.6,
		TargetPct:  90,
		Sharp:      0.6,
		Tone:       Strength{Auto: true},
		Gamut:      Strength{Auto: true},
		PaletteOut: "ideal",
	}
}

// Result is a converted image, still as palette indices.
type Result struct {
	Indices []uint8
	W, H    int
	Report  Report
}

// Analyse looks at a fitted image and decides what to do with it, without
// changing anything. The TUI calls this on its own so it can show the reasoning.
func AnalyseImage(fitted image.Image, p Palette, prm Params) ([]float64, Report) {
	graphic := prm.Content == "graphic" || (prm.Content == "auto" && IsGraphic(fitted))
	f := ToFrame(fitted)
	scale := prm.Sat
	if graphic {
		scale = 0
	}
	gains, rep := Analyse(f.Downsample(4), p, prm.MaxGain, prm.TargetPct, scale)
	rep.Graphic = graphic
	if graphic {
		// Flat art has deliberate colours and no tonal range to rescue. Leave it be.
		for i := range gains {
			gains[i] = 1
		}
		rep.Shadows = 0
		rep.Hues = nil
	}
	return gains, rep
}

// Convert runs the whole pipeline on an already fitted image.
//
// Order matters and it is the order the reference implementation settled on:
// chroma shaping, then sharpening, then the tonal S curve, then dynamic range,
// then the vivid and light colour pushes, then the hue clip, then gamut, then
// dither. Chroma first because everything after it depends on
// where the colours ended up. Gamut last because it is the safety net that
// catches whatever the earlier steps pushed out of reach.
func Convert(fitted image.Image, p Palette, prm Params, gains []float64, rep Report) Result {
	f := ToFrame(fitted)
	if !rep.Graphic {
		ApplyGains(f, gains)
	}
	if prm.Sharp > 0 && !rep.Graphic {
		f = ToFrame(Unsharp(f.ToImage(), prm.Sharp))
	}

	shadows := rep.Shadows
	if prm.Shadows != nil {
		shadows = *prm.Shadows
	}
	ShadowsHighlights(f, shadows, prm.Highlights)

	if !rep.Graphic {
		switch {
		case prm.Tone.Auto:
			AutoCompressDynamicRange(f, p)
		case prm.Tone.Value > 0:
			CompressDynamicRange(f, p, prm.Tone.Value)
		}
		// The per hue boost only aims at the gamut edge, so a colourful image gets
		// nothing and dithers out paler than it looks on a screen. A flat extra
		// push, eased in above the neutral floor so greys and white clouds stay
		// clean, makes it read as vivid on the wall.
		if prm.Vivid != 1 {
			vivid := make([]float64, HueBins)
			for i := range vivid {
				vivid[i] = prm.Vivid
			}
			ApplyGains(f, vivid)
		}
		LightChroma(f, p, prm.Light)
	}
	if prm.KeepHue > 0 && !rep.Graphic {
		HueClip(f, p, 1-prm.KeepHue)
	}
	switch {
	case prm.Gamut.Auto:
		GamutCompress(f, p, 1)
	case prm.Gamut.Value > 0:
		GamutCompress(f, p, prm.Gamut.Value)
	}

	mode := prm.Dither
	if rep.Graphic {
		mode = ModeNone
	}
	return Result{
		Indices: Dither(f, p, mode, prm.Serpentine),
		W:       f.W,
		H:       f.H,
		Report:  rep,
	}
}

// Run is the whole job for one file: open, fit, analyse, convert.
func Run(path string, p Palette, prm Params) (Result, image.Image, error) {
	img, err := Open(path)
	if err != nil {
		return Result{}, nil, err
	}
	fitted := Fit(img, prm.Width, prm.Height, prm.Contain)
	gains, rep := AnalyseImage(fitted, p, prm)
	return Convert(fitted, p, prm, gains, rep), fitted, nil
}

// ReportLines formats an analysis the way the CLI prints it.
func (r Report) Lines(name string) []string {
	kind := "photo"
	if r.Graphic {
		kind = "graphic"
	}
	out := []string{fmt.Sprintf("%s: %s medianL=%.3f shadows=%.2f", name, kind, r.MedianL, r.Shadows)}
	for _, h := range r.Hues {
		bar := strings.Repeat("+", int((h.Gain-1)*20+0.5))
		out = append(out, fmt.Sprintf("    %-8s x%.2f %-12s %4.1f%% of chroma",
			h.Name, h.Gain, bar, h.Share*100))
	}
	return out
}
