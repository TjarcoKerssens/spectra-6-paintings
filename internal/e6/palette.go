package e6

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Ink indices. This order is the firmware contract: the byte written for a pixel
// is its index here, so reordering these silently swaps pigments on real hardware.
const (
	Black = iota
	White
	Yellow
	Red
	Blue
	Green
)

// InkNames is the canonical order, matching the constants above.
var InkNames = [6]string{"black", "white", "yellow", "red", "blue", "green"}

// RGB is one 8 bit sRGB colour.
type RGB [3]uint8

// Palette holds one sRGB triple per ink.
type Palette [6]RGB

// Ideal is what the six inks are nominally meant to be. Files are written with
// these so anything downstream sees canonical colours, but nothing matches
// against them, because a reflective panel gets nowhere near them.
var Ideal = Palette{
	{0, 0, 0}, {255, 255, 255}, {255, 255, 0}, {255, 0, 0}, {0, 0, 255}, {0, 255, 0},
}

// Measured is a photographically calibrated 7.3 inch Spectra 6, the v2
// measurement from the OpenDisplay reference data: RAW under two 6500K lights,
// normalised against white paper, gamma encoded.
//
// It is the starting point, not the truth. Pigment mixing varies between units
// and between panel sizes, so run the calibration and replace it. Every gain the
// analyser computes is derived from these numbers, which makes this the single
// highest leverage constant in the program.
var Measured = Palette{
	{31, 24, 41},    // black
	{168, 180, 182}, // white
	{180, 173, 0},   // yellow
	{113, 24, 19},   // red
	{36, 70, 139},   // blue
	{50, 84, 60},    // green
}

// Linear returns the palette as linear RGB.
func (p Palette) Linear() [6][3]float64 {
	var out [6][3]float64
	for i, c := range p {
		out[i] = [3]float64{
			SRGBToLinear(float64(c[0]) / 255),
			SRGBToLinear(float64(c[1]) / 255),
			SRGBToLinear(float64(c[2]) / 255),
		}
	}
	return out
}

// Lab returns the palette in OkLab, ready for matching.
func (p Palette) Lab() []OkLab {
	lin := p.Linear()
	out := make([]OkLab, len(lin))
	for i, c := range lin {
		out[i] = LinearToOkLab(c[0], c[1], c[2])
	}
	return out
}

// ConfigPath is where a calibrated palette lives once saved.
func ConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "spectra6-palette.json"
	}
	return filepath.Join(home, ".config", "spectra6", "palette.json")
}

// LoadPalette reads a palette from JSON. An empty path falls back to the config
// file, and a missing config file falls back to the built in measurement.
func LoadPalette(path string) (Palette, error) {
	explicit := path != ""
	if !explicit {
		path = ConfigPath()
		if _, err := os.Stat(path); err != nil {
			return Measured, nil
		}
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return Measured, err
	}
	var m map[string][]int
	if err := json.Unmarshal(raw, &m); err != nil {
		return Measured, fmt.Errorf("%s: %w", path, err)
	}
	var out Palette
	for i, name := range InkNames {
		v, ok := m[name]
		if !ok {
			return Measured, fmt.Errorf("%s: missing %q", path, name)
		}
		if len(v) != 3 {
			return Measured, fmt.Errorf("%s: %q needs three values, got %d", path, name, len(v))
		}
		for c := 0; c < 3; c++ {
			if v[c] < 0 || v[c] > 255 {
				return Measured, fmt.Errorf("%s: %q channel %d out of range: %d", path, name, c, v[c])
			}
			out[i][c] = uint8(v[c])
		}
	}
	return out, nil
}

// SavePalette writes a palette as JSON, creating the directory if needed.
func SavePalette(p Palette, path string) error {
	if path == "" {
		path = ConfigPath()
	}
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	m := make(map[string][]int, 6)
	for i, name := range InkNames {
		m[name] = []int{int(p[i][0]), int(p[i][1]), int(p[i][2])}
	}
	raw, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(raw, '\n'), 0o644)
}

// Normalise turns raw sampled patch values into a usable palette.
//
// Raw camera numbers mean nothing on their own, they encode the lamps as much as
// the pigments. Scaling every channel so the white paper reference reads 255
// divides the lighting back out, which is what makes one person's measurement
// comparable to another's.
//
// Set linear when the RAW was developed with a linear tone curve: the numbers are
// then linear light and need the sRGB transfer function applied, or every ink
// reads far too dark.
func Normalise(raw Palette, paper RGB, linear bool) (Palette, error) {
	for c := 0; c < 3; c++ {
		if paper[c] == 0 {
			return Palette{}, fmt.Errorf("paper reference channel %d is zero", c)
		}
	}
	var out Palette
	for i, patch := range raw {
		for c := 0; c < 3; c++ {
			v := float64(patch[c]) * 255 / float64(paper[c])
			if linear {
				v = 255 * LinearToSRGB(clamp(v/255, 0, 1))
			}
			out[i][c] = uint8(clamp(v+0.5, 0, 255))
		}
	}
	return out, nil
}
