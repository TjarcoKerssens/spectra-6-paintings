package e6

import (
	"math"
	"sort"
)

// HueBins is how finely the hue circle is split when deciding what to boost.
// Enough to tell orange from yellow, coarse enough that each bin holds a usable
// sample of pixels.
const HueBins = 36

// A pixel below ChromaFloor is treated as neutral and left alone, so sensor noise
// in a grey sky does not get amplified into confetti. Between there and
// ChromaKnee the boost eases in.
const (
	ChromaFloor = 0.012
	ChromaKnee  = 0.045
)

type hueFamily struct {
	name string
	deg  float64
}

var hueFamilies = []hueFamily{
	{"red", 20}, {"orange", 55}, {"yellow", 100}, {"green", 160},
	{"cyan", 200}, {"blue", 265}, {"purple", 310}, {"magenta", 350},
}

// HueGain is one line of the analysis: how much a hue family was boosted and how
// much of the image's colour lives there.
type HueGain struct {
	Name  string
	Gain  float64
	Share float64
}

// Report is what the analyser concluded about an image.
type Report struct {
	Graphic bool
	MedianL float64
	Shadows float64
	Hues    []HueGain
}

func binCenters() []float64 {
	out := make([]float64, HueBins)
	for i := range out {
		out[i] = (float64(i)+0.5)/HueBins*2*math.Pi - math.Pi
	}
	return out
}

// Analyse decides, per hue, how much chroma to add before dithering.
//
// The idea is simple. For each hue, work out the most chroma the panel can
// actually produce, then look at how colourful the image already is at that hue.
// If the image is well short of what the panel can do, the colour would dither
// into a weak wash, so push it up. If the image is already past what the panel can
// reach, leave it and let gamut compression fold it back cleanly. Gains never go
// below 1, because desaturating is gamut compression's job, not this one's.
//
// scale multiplies the computed boost, so a caller can dial the whole effect back
// to nothing without changing the analysis.
func Analyse(f *Frame, p Palette, maxGain, targetPct, scale float64) ([]float64, Report) {
	centers := binCenters()
	reach := HullChroma(p, centers)
	for i := range reach {
		reach[i] *= HullSafety
	}

	type bin struct{ chroma []float64 }
	bins := make([]bin, HueBins)
	lums := make([]float64, 0, len(f.Px))
	chromatic := 0
	for _, px := range f.Px {
		c := LinearToOkLab(px[0], px[1], px[2])
		lums = append(lums, c.L)
		ch := c.Chroma()
		if ch <= ChromaFloor {
			continue
		}
		chromatic++
		i := int((c.Hue() + math.Pi) / (2 * math.Pi) * HueBins)
		if i < 0 {
			i = 0
		}
		if i >= HueBins {
			i = HueBins - 1
		}
		bins[i].chroma = append(bins[i].chroma, ch)
	}

	gains := make([]float64, HueBins)
	weights := make([]float64, HueBins)
	minSamples := int(math.Max(32, 0.0005*float64(len(f.Px))))
	for i := range gains {
		gains[i] = 1
		weights[i] = 0.35 // an empty bin should follow its neighbours, not fight them
		if len(bins[i].chroma) < minSamples {
			continue
		}
		weights[i] = 1
		sort.Float64s(bins[i].chroma)
		idx := int(math.Round(targetPct / 100 * float64(len(bins[i].chroma)-1)))
		current := bins[i].chroma[clampInt(idx, 0, len(bins[i].chroma)-1)]
		if current > 1e-4 {
			gains[i] = clamp(reach[i]/current, 1, maxGain)
		}
	}
	gains = smoothCircular(gains, weights)
	for i := range gains {
		gains[i] = 1 + (gains[i]-1)*scale
	}

	rep := Report{MedianL: median(lums)}
	// A dark image is where a reflective panel struggles most, since its black is
	// really a dark grey. Lifting the shadows a little buys back separation that
	// would otherwise dither into a flat mass.
	rep.Shadows = clamp((0.38-rep.MedianL)*1.4, 0, 0.40)

	if chromatic > 0 {
		for _, fam := range hueFamilies {
			target := fam.deg * math.Pi / 180
			if target > math.Pi {
				target -= 2 * math.Pi
			}
			var sum, count, pixels float64
			for i, c := range centers {
				if math.Abs(angleDiff(c, target)) < math.Pi/float64(len(hueFamilies)) {
					sum += gains[i]
					count++
					pixels += float64(len(bins[i].chroma))
				}
			}
			if count > 0 && pixels > 0 {
				rep.Hues = append(rep.Hues, HueGain{
					Name:  fam.name,
					Gain:  sum / count,
					Share: pixels / float64(chromatic),
				})
			}
		}
		sort.Slice(rep.Hues, func(i, j int) bool { return rep.Hues[i].Share > rep.Hues[j].Share })
	}
	return gains, rep
}

// ApplyGains scales chroma per hue, leaving hue angle and lightness untouched.
func ApplyGains(f *Frame, gains []float64) {
	centers := binCenters()
	f.each(func(px *[3]float64) {
		c := LinearToOkLab(px[0], px[1], px[2])
		ch := c.Chroma()
		if ch <= ChromaFloor {
			return
		}
		g := interpCircular(centers, gains, c.Hue())
		// Ease the boost in so there is no visible edge where the floor sits.
		t := smoothstep(ChromaFloor, ChromaKnee, ch)
		g = 1 + (g-1)*t
		c.A *= g
		c.B *= g
		*px = OkLabToLinear(c)
	})
}

// smoothCircular runs a weighted three tap blur around the hue circle, so
// neighbouring hues cannot end up with wildly different gains and shear against
// each other across a gradient.
func smoothCircular(v, w []float64) []float64 {
	k := [3]float64{0.15, 0.7, 0.15}
	out := make([]float64, len(v))
	for i := range v {
		var num, den float64
		for j := -1; j <= 1; j++ {
			idx := ((i+j)%len(v) + len(v)) % len(v)
			num += v[idx] * w[idx] * k[j+1]
			den += w[idx] * k[j+1]
		}
		if den > 0 {
			out[i] = num / den
		} else {
			out[i] = v[i]
		}
	}
	return out
}

// interpCircular linearly interpolates a per bin value at an arbitrary hue,
// wrapping around the circle.
func interpCircular(centers, values []float64, h float64) float64 {
	n := len(centers)
	pos := (h+math.Pi)/(2*math.Pi)*float64(n) - 0.5
	i := int(math.Floor(pos))
	t := pos - float64(i)
	a := values[((i%n)+n)%n]
	b := values[(((i+1)%n)+n)%n]
	return a + t*(b-a)
}

func angleDiff(a, b float64) float64 {
	d := math.Mod(a-b+math.Pi, 2*math.Pi)
	if d < 0 {
		d += 2 * math.Pi
	}
	return d - math.Pi
}

func median(v []float64) float64 {
	if len(v) == 0 {
		return 0
	}
	s := append([]float64(nil), v...)
	sort.Float64s(s)
	return s[len(s)/2]
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
