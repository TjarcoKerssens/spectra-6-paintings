package e6

import (
	"math"
	"sort"
)

// Exposure multiplies linear light. Above 1 brightens, below 1 darkens.
func Exposure(f *Frame, factor float64) {
	if math.Abs(factor-1) < 1e-9 {
		return
	}
	f.each(func(p *[3]float64) {
		for k := 0; k < 3; k++ {
			p[k] = clamp(p[k]*factor, 0, 1)
		}
	})
}

// Saturation scales OkLab chroma, leaving hue and lightness alone. Scaling a and
// b together is what keeps the hue angle fixed.
func Saturation(f *Frame, factor float64) {
	if math.Abs(factor-1) < 1e-9 {
		return
	}
	f.each(func(p *[3]float64) {
		c := LinearToOkLab(p[0], p[1], p[2])
		c.A = clamp(c.A*factor, -1, 1)
		c.B = clamp(c.B*factor, -1, 1)
		*p = OkLabToLinear(c)
	})
}

// ShadowsHighlights lifts the lower half of the tone range and squeezes the upper
// half, as an S curve on luminance alone.
//
// The curve pivots at perceptual mid grey, meaning sRGB 0.5, not linear 0.5.
// Pivoting in linear light would put roughly three quarters of the visible tonal
// range in the "shadows" half and the controls would feel wrong. Hue survives
// because all three channels are scaled by the same ratio.
func ShadowsHighlights(f *Frame, shadows, highlights float64) {
	if shadows <= 0 && highlights <= 0 {
		return
	}
	shadows = clamp(shadows, 0, 1)
	highlights = clamp(highlights, 0, 1)
	f.each(func(p *[3]float64) {
		y := Luminance(*p)
		if y <= 1e-6 {
			return
		}
		g := LinearToSRGB(y)
		var gp float64
		if g <= 0.5 {
			gp = 0.5 * math.Pow(g/0.5, 1-shadows)
		} else {
			gp = 0.5 + 0.5*math.Pow((g-0.5)/0.5, 1+highlights)
		}
		scale := clamp(SRGBToLinear(gp)/y, 0, 1e6)
		for k := 0; k < 3; k++ {
			p[k] = clamp(p[k]*scale, 0, 1)
		}
	})
}

// scaleDark handles pixels too dark to divide by, preserving their channel ratios
// instead of letting a target/current scale explode.
func scaleDark(p *[3]float64, targetY float64) {
	max := math.Max(p[0], math.Max(p[1], p[2]))
	if max > 1e-12 {
		s := targetY / max
		for k := 0; k < 3; k++ {
			p[k] = clamp(p[k]*s, 0, 1)
		}
		return
	}
	p[0], p[1], p[2] = targetY, targetY, targetY
}

func percentilePair(values []float64, lo, hi float64) (float64, float64) {
	s := append([]float64(nil), values...)
	sort.Float64s(s)
	n := float64(len(s) - 1)
	at := func(p float64) float64 {
		i := int(math.Round(p / 100 * n))
		if i < 0 {
			i = 0
		}
		if i >= len(s) {
			i = len(s) - 1
		}
		return s[i]
	}
	return at(lo), at(hi)
}

// CompressDynamicRange remaps luminance from [0,1] into the panel's real black to
// white range at a fixed strength.
//
// A reflective panel's white is far darker than 255 and its black far lighter
// than 0. Skip this and the dither spends its whole error budget chasing
// brightnesses that do not exist, which shows up as noise.
func CompressDynamicRange(f *Frame, p Palette, strength float64) {
	if strength <= 0 {
		return
	}
	lin := p.Linear()
	blackY, whiteY := Luminance(lin[Black]), Luminance(lin[White])
	span := whiteY - blackY
	if span <= 0 {
		return
	}
	f.each(func(px *[3]float64) {
		y := Luminance(*px)
		target := y + strength*((blackY+y*span)-y)
		if y > 1e-6 {
			s := clamp(target/y, 0, 1e6)
			for k := 0; k < 3; k++ {
				px[k] = clamp(px[k]*s, 0, 1)
			}
			return
		}
		scaleDark(px, blackY*strength)
	})
}

// AutoCompressDynamicRange compresses only when the image genuinely overflows the
// panel, and picks its own strength.
//
// The image range is taken from the 2nd and 98th percentiles so a handful of
// specular pixels cannot dictate the mapping. If that range already fits inside
// the panel, within a tenth of the panel's span, nothing happens at all.
//
// The strength comes from Reinhard's log average skew: where the log mean sits
// between the log minimum and maximum tells you whether the image is mostly dark
// with bright highlights, which needs strong compression, or evenly spread, which
// does not.
func AutoCompressDynamicRange(f *Frame, p Palette) {
	lin := p.Linear()
	blackY, whiteY := Luminance(lin[Black]), Luminance(lin[White])
	span := whiteY - blackY
	if span <= 0 {
		return
	}
	lum := f.Luma()
	pLow, pHigh := percentilePair(lum, 2, 98)
	imageRange := pHigh - pLow
	if imageRange < 1e-6 {
		CompressDynamicRange(f, p, 1)
		return
	}
	const tolerance = 0.10
	if pLow >= blackY-tolerance*span && pHigh <= whiteY+tolerance*span {
		return
	}

	strength := 1.0
	var sum float64
	var count int
	for _, y := range lum {
		if y > 1e-6 {
			sum += math.Log(y + 1e-5)
			count++
		}
	}
	if count > 0 {
		logAvg := math.Exp(sum / float64(count))
		logMin := math.Log(math.Max(pLow, 1e-5))
		logMax := math.Log(math.Max(pHigh, 1e-5))
		if logMax-logMin > 1e-6 {
			// A high key image can put the log average above the log maximum,
			// making skew negative. Clamping before the power keeps it real.
			skew := clamp((logMax-math.Log(logAvg+1e-5))/(logMax-logMin), 0, 1)
			strength = math.Pow(skew, 1.4)
		}
	}

	f.each(func(px *[3]float64) {
		y := Luminance(*px)
		// Clamping the normalised position stops sub percentile pixels from
		// extrapolating below panel black and crushing darker than they started.
		norm := clamp((y-pLow)/imageRange, 0, 1)
		target := y + strength*((blackY+norm*span)-y)
		if y > 1e-6 {
			s := clamp(target/y, 0, 1e6)
			for k := 0; k < 3; k++ {
				px[k] = clamp(px[k]*s, 0, 1)
			}
			return
		}
		scaleDark(px, target)
	})
}
