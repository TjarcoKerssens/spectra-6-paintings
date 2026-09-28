package e6

import (
	"fmt"
	"sort"
	"strings"
)

// Mode selects how the six inks approximate everything in between.
type Mode string

const (
	ModeNone           Mode = "none"
	ModeOrdered        Mode = "ordered"
	ModeFloydSteinberg Mode = "floyd-steinberg"
	ModeBurkes         Mode = "burkes"
	ModeAtkinson       Mode = "atkinson"
	ModeSierra         Mode = "sierra"
	ModeSierraLite     Mode = "sierra-lite"
	ModeStucki         Mode = "stucki"
	ModeJarvis         Mode = "jarvis"
)

type tap struct {
	dx, dy int
	w      float64
}

type kernel struct {
	taps    []tap
	divisor float64
}

// Error diffusion kernels, each as its published weights over a divisor.
//
// The trade is reach against speed and artefacts. Floyd-Steinberg pushes error
// only one row down and is fast but leaves a visible weave on smooth gradients.
// Jarvis spreads over two rows and is the smoothest and slowest. Atkinson passes
// on only 6 of 8 parts, deliberately losing error, which lifts local contrast and
// suits line art more than photographs.
var kernels = map[Mode]kernel{
	ModeFloydSteinberg: {divisor: 16, taps: []tap{
		{1, 0, 7}, {-1, 1, 3}, {0, 1, 5}, {1, 1, 1},
	}},
	ModeBurkes: {divisor: 32, taps: []tap{
		{1, 0, 8}, {2, 0, 4},
		{-2, 1, 2}, {-1, 1, 4}, {0, 1, 8}, {1, 1, 4}, {2, 1, 2},
	}},
	ModeAtkinson: {divisor: 8, taps: []tap{
		{1, 0, 1}, {2, 0, 1},
		{-1, 1, 1}, {0, 1, 1}, {1, 1, 1},
		{0, 2, 1},
	}},
	ModeSierra: {divisor: 32, taps: []tap{
		{1, 0, 5}, {2, 0, 3},
		{-2, 1, 2}, {-1, 1, 4}, {0, 1, 5}, {1, 1, 4}, {2, 1, 2},
		{-1, 2, 2}, {0, 2, 3}, {1, 2, 2},
	}},
	ModeSierraLite: {divisor: 4, taps: []tap{
		{1, 0, 2}, {-1, 1, 1}, {0, 1, 1},
	}},
	ModeStucki: {divisor: 42, taps: []tap{
		{1, 0, 8}, {2, 0, 4},
		{-2, 1, 2}, {-1, 1, 4}, {0, 1, 8}, {1, 1, 4}, {2, 1, 2},
		{-2, 2, 1}, {-1, 2, 2}, {0, 2, 4}, {1, 2, 2}, {2, 2, 1},
	}},
	ModeJarvis: {divisor: 48, taps: []tap{
		{1, 0, 7}, {2, 0, 5},
		{-2, 1, 3}, {-1, 1, 5}, {0, 1, 7}, {1, 1, 5}, {2, 1, 3},
		{-2, 2, 1}, {-1, 2, 3}, {0, 2, 5}, {1, 2, 3}, {2, 2, 1},
	}},
}

// Modes lists every mode name, for help text and menus.
func Modes() []string {
	out := []string{string(ModeNone), string(ModeOrdered)}
	for m := range kernels {
		out = append(out, string(m))
	}
	sort.Strings(out)
	return out
}

// ParseMode validates a mode name.
func ParseMode(s string) (Mode, error) {
	m := Mode(strings.ToLower(strings.TrimSpace(s)))
	if m == ModeNone || m == ModeOrdered {
		return m, nil
	}
	if _, ok := kernels[m]; ok {
		return m, nil
	}
	return "", fmt.Errorf("unknown dither mode %q, pick one of %s", s, strings.Join(Modes(), ", "))
}

// bayer4 is the ordered dither matrix, pre-centred on zero so it reads as a
// signed nudge rather than a threshold.
var bayer4 = [4][4]float64{
	{-0.46875, 0.03125, -0.34375, 0.15625},
	{0.28125, -0.21875, 0.40625, -0.09375},
	{-0.28125, 0.21875, -0.40625, 0.09375},
	{0.46875, -0.03125, 0.34375, -0.15625},
}

// orderedSpread scales the Bayer nudge to the palette's quantisation step. A dense
// grey ramp would be swamped by full range dither noise, while six widely spaced
// inks want the full swing.
func orderedSpread(p Palette) float64 {
	grey := true
	for _, c := range p {
		if c[0] != c[1] || c[1] != c[2] {
			grey = false
			break
		}
	}
	if grey && len(p) >= 3 {
		return 1 / float64(len(p)-1)
	}
	return 1
}

// Dither maps a frame onto palette indices.
//
// Two decisions here are worth knowing about.
//
// Matching happens in OkLab, because that is where "closest colour" means what
// the eye means, and with the chromatic axes weighted so hue beats lightness.
//
// Error is accumulated in sRGB space, not linear light, and the working value is
// clamped to [0,1] before every match. Both matter. Gamma space keeps dot density
// perceptually even across the tonal range, where a fixed linear step is enormous
// in the shadows and invisible in the highlights. The clamp is what stops the
// error running away: the panel's white is nowhere near 255 and its black nowhere
// near 0, so a bright region generates error that can never be absorbed, and
// unbounded accumulation turns whole areas into whatever ink sits at the extreme.
// That failure looks like large flat blobs of one colour.
//
// Serpentine scanning reverses every other row so the residue cannot organise
// itself into diagonal streaks.
func Dither(f *Frame, p Palette, mode Mode, serpentine bool) []uint8 {
	lab := p.Lab()
	var palSRGB [6][3]float64
	for i, c := range p {
		for k := 0; k < 3; k++ {
			palSRGB[i][k] = float64(c[k]) / 255
		}
	}

	// Working buffer in sRGB fractions, which is the space the error lives in.
	buf := make([][3]float64, len(f.Px))
	for i, px := range f.Px {
		for k := 0; k < 3; k++ {
			buf[i][k] = LinearToSRGB(px[k])
		}
	}

	match := func(v [3]float64) int {
		return Nearest(LinearToOkLab(
			SRGBToLinear(v[0]), SRGBToLinear(v[1]), SRGBToLinear(v[2])), lab, ChromaWeight)
	}

	out := make([]uint8, len(f.Px))
	switch mode {
	case ModeNone:
		for i, v := range buf {
			out[i] = uint8(match(v))
		}
		return out
	case ModeOrdered:
		spread := orderedSpread(p)
		for y := 0; y < f.H; y++ {
			for x := 0; x < f.W; x++ {
				v := buf[y*f.W+x]
				bump := bayer4[y%4][x%4] * spread
				for k := 0; k < 3; k++ {
					v[k] = clamp(v[k]+bump, 0, 1)
				}
				out[y*f.W+x] = uint8(match(v))
			}
		}
		return out
	}

	k := kernels[mode]
	for y := 0; y < f.H; y++ {
		reverse := serpentine && y%2 == 1
		for i := 0; i < f.W; i++ {
			x := i
			if reverse {
				x = f.W - 1 - i
			}
			idx := y*f.W + x

			var v [3]float64
			for c := 0; c < 3; c++ {
				v[c] = clamp(buf[idx][c], 0, 1)
			}
			best := match(v)
			out[idx] = uint8(best)

			var err [3]float64
			for c := 0; c < 3; c++ {
				err[c] = v[c] - palSRGB[best][c]
			}
			for _, t := range k.taps {
				dx := t.dx
				if reverse {
					dx = -dx
				}
				nx, ny := x+dx, y+t.dy
				if nx < 0 || nx >= f.W || ny < 0 || ny >= f.H {
					continue
				}
				n := &buf[ny*f.W+nx]
				for c := 0; c < 3; c++ {
					n[c] += err[c] * t.w / k.divisor
				}
			}
		}
	}
	return out
}
