// Package e6 prepares images for E Ink Spectra 6 panels.
//
// The panel shows six pigments and nothing else, so every intermediate colour is
// a dither pattern of those six. Everything here works towards one goal: decide
// what the six inks can actually reproduce, push the image into that range, then
// diffuse the remaining error.
package e6

import "math"

// OkLab is Ottosson's perceptual colour space. Distances in it line up with what
// the eye calls "a similar colour" far better than distances in RGB, which is why
// palette matching and chroma work both happen here.
type OkLab struct{ L, A, B float64 }

// Chroma is the distance from the neutral axis. Hue is the angle around it.
func (c OkLab) Chroma() float64 { return math.Hypot(c.A, c.B) }
func (c OkLab) Hue() float64    { return math.Atan2(c.B, c.A) }

var (
	mRGBtoLMS = [3][3]float64{
		{0.4122214708, 0.5363325363, 0.0514459929},
		{0.2119034982, 0.6806995451, 0.1073969566},
		{0.0883024619, 0.2817188376, 0.6299787005},
	}
	mLMStoLab = [3][3]float64{
		{0.2104542553, 0.7936177850, -0.0040720468},
		{1.9779984951, -2.4285922050, 0.4505937099},
		{0.0259040371, 0.7827717662, -0.8086757660},
	}
	mLabToLMS = [3][3]float64{
		{1.0, 0.3963377774, 0.2158037573},
		{1.0, -0.1055613458, -0.0638541728},
		{1.0, -0.0894841775, -1.2914855480},
	}
	mLMStoRGB = [3][3]float64{
		{4.0767416621, -3.3077115913, 0.2309699292},
		{-1.2684380046, 2.6097574011, -0.3413193965},
		{-0.0041960863, -0.7034186147, 1.7076147010},
	}
)

// SRGBToLinear undoes the sRGB transfer function. Every calculation in this
// package needs linear light; averaging or diffusing error in gamma space is the
// classic way to get muddy midtones.
func SRGBToLinear(c float64) float64 {
	if c <= 0.04045 {
		return c / 12.92
	}
	return math.Pow((c+0.055)/1.055, 2.4)
}

// LinearToSRGB is the inverse, for handing pixels back to an image file.
func LinearToSRGB(c float64) float64 {
	c = clamp(c, 0, 1)
	if c <= 0.0031308 {
		return c * 12.92
	}
	return 1.055*math.Pow(c, 1.0/2.4) - 0.055
}

// LinearToOkLab converts linear RGB in [0,1].
func LinearToOkLab(r, g, b float64) OkLab {
	l := math.Cbrt(mRGBtoLMS[0][0]*r + mRGBtoLMS[0][1]*g + mRGBtoLMS[0][2]*b)
	m := math.Cbrt(mRGBtoLMS[1][0]*r + mRGBtoLMS[1][1]*g + mRGBtoLMS[1][2]*b)
	s := math.Cbrt(mRGBtoLMS[2][0]*r + mRGBtoLMS[2][1]*g + mRGBtoLMS[2][2]*b)
	return OkLab{
		L: mLMStoLab[0][0]*l + mLMStoLab[0][1]*m + mLMStoLab[0][2]*s,
		A: mLMStoLab[1][0]*l + mLMStoLab[1][1]*m + mLMStoLab[1][2]*s,
		B: mLMStoLab[2][0]*l + mLMStoLab[2][1]*m + mLMStoLab[2][2]*s,
	}
}

// OkLabToLinear is the inverse, clamped into the unit cube.
func OkLabToLinear(c OkLab) [3]float64 {
	l := mLabToLMS[0][0]*c.L + mLabToLMS[0][1]*c.A + mLabToLMS[0][2]*c.B
	m := mLabToLMS[1][0]*c.L + mLabToLMS[1][1]*c.A + mLabToLMS[1][2]*c.B
	s := mLabToLMS[2][0]*c.L + mLabToLMS[2][1]*c.A + mLabToLMS[2][2]*c.B
	l, m, s = l*l*l, m*m*m, s*s*s
	return [3]float64{
		clamp(mLMStoRGB[0][0]*l+mLMStoRGB[0][1]*m+mLMStoRGB[0][2]*s, 0, 1),
		clamp(mLMStoRGB[1][0]*l+mLMStoRGB[1][1]*m+mLMStoRGB[1][2]*s, 0, 1),
		clamp(mLMStoRGB[2][0]*l+mLMStoRGB[2][1]*m+mLMStoRGB[2][2]*s, 0, 1),
	}
}

// ChromaWeight scales the a and b axes against L when matching a pixel to an ink.
//
// Above 1.0 it makes hue matter more than lightness, so a saturated pixel reaches
// for a coloured ink instead of collapsing onto black or white. 1.5 is the value
// the reference implementation settled on after sweeping it against a fixture set.
const ChromaWeight = 1.5

// Nearest returns the index of the closest palette entry under the weighted
// distance dL^2 + w^2*(da^2 + db^2).
func Nearest(px OkLab, pal []OkLab, w float64) int {
	best, bestDist := 0, math.Inf(1)
	w2 := w * w
	for i, p := range pal {
		dl := px.L - p.L
		da := px.A - p.A
		db := px.B - p.B
		d := dl*dl + w2*(da*da+db*db)
		if d < bestDist {
			best, bestDist = i, d
		}
	}
	return best
}

// Luminance uses the BT.709 coefficients, on linear light.
func Luminance(p [3]float64) float64 {
	return 0.2126729*p[0] + 0.7151522*p[1] + 0.0721750*p[2]
}

func clamp(v, lo, hi float64) float64 { return math.Min(math.Max(v, lo), hi) }
