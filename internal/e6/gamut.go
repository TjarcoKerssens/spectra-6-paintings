package e6

import (
	"math"
	"sort"
)

type pt2 struct{ X, Y float64 }

// hull2d returns the convex hull of a point set, counter clockwise, by the
// monotone chain method.
func hull2d(pts []pt2) []pt2 {
	p := append([]pt2(nil), pts...)
	sort.Slice(p, func(i, j int) bool {
		if p[i].X != p[j].X {
			return p[i].X < p[j].X
		}
		return p[i].Y < p[j].Y
	})
	cross := func(o, a, b pt2) float64 {
		return (a.X-o.X)*(b.Y-o.Y) - (a.Y-o.Y)*(b.X-o.X)
	}
	half := func(in []pt2) []pt2 {
		var out []pt2
		for _, q := range in {
			for len(out) >= 2 && cross(out[len(out)-2], out[len(out)-1], q) <= 0 {
				out = out[:len(out)-1]
			}
			out = append(out, q)
		}
		return out[:len(out)-1]
	}
	rev := make([]pt2, len(p))
	for i := range p {
		rev[i] = p[len(p)-1-i]
	}
	return append(half(p), half(rev)...)
}

// HullSafety keeps the chroma target just inside the gamut edge. Aiming exactly
// at the edge leaves the dither no headroom and the result bands.
const HullSafety = 0.85

// HullChroma reports the largest chroma the palette can reach at each hue, by
// casting a ray from the neutral axis out to the palette hull.
//
// Known approximation: the hull is projected onto the ab plane, so it ignores
// that reachable chroma shrinks at very high and very low lightness. That
// overestimates, which is what HullSafety backs off. A per lightness hull slice
// would be exact, at the cost of a much larger computation per pixel.
func HullChroma(p Palette, hues []float64) []float64 {
	lab := p.Lab()
	pts := make([]pt2, len(lab))
	for i, c := range lab {
		pts[i] = pt2{c.A, c.B}
	}
	poly := hull2d(pts)
	out := make([]float64, len(hues))
	for i, h := range hues {
		dx, dy := math.Cos(h), math.Sin(h)
		best := math.Inf(1)
		for j, a := range poly {
			b := poly[(j+1)%len(poly)]
			ex, ey := b.X-a.X, b.Y-a.Y
			den := dx*ey - dy*ex
			if math.Abs(den) < 1e-12 {
				continue
			}
			// solve t*d = a + s*e for t along the ray and s along the edge
			s := (a.X*dy - a.Y*dx) / den
			t := (a.X*ey - a.Y*ex) / den
			if s >= 0 && s <= 1 && t > 0 && t < best {
				best = t
			}
		}
		if math.IsInf(best, 1) {
			best = 0
		}
		out[i] = best
	}
	return out
}

const (
	gamutThreshold    = 0.15
	gamutThresholdMax = 0.45
)

func smoothstep(e0, e1, x float64) float64 {
	t := clamp((x-e0)/(e1-e0), 0, 1)
	return t * t * (3 - 2*t)
}

// GamutCompress pulls colours the panel cannot make towards the nearest point on
// the palette hull, easing in with a smoothstep so nothing snaps.
//
// Without it, a saturated image hands the dither pixels it can only approximate
// by mixing inks that average out grey, and large regions turn to mud. Strength
// runs 0 to 1.
//
// Like HullChroma this walks hull edges rather than hull faces, so for a six
// colour palette the true nearest point can lie inside a triangular face and be
// missed. In practice the edge approximation moves the pixel to within a
// perceptual hair of the right place.
func GamutCompress(f *Frame, p Palette, strength float64) {
	if strength <= 0 {
		return
	}
	lin := p.Linear()
	lab := p.Lab()
	type edge struct {
		aLab, bLab OkLab
		aLin, bLin [3]float64
	}
	var edges []edge
	for i := 0; i < len(lab); i++ {
		for j := i + 1; j < len(lab); j++ {
			edges = append(edges, edge{lab[i], lab[j], lin[i], lin[j]})
		}
	}
	f.each(func(px *[3]float64) {
		c := LinearToOkLab(px[0], px[1], px[2])
		best, bestDist := *px, math.Inf(1)
		for _, e := range edges {
			t := nearestOnSegment(c, e.aLab, e.bLab)
			n := OkLab{
				L: e.aLab.L + t*(e.bLab.L-e.aLab.L),
				A: e.aLab.A + t*(e.bLab.A-e.aLab.A),
				B: e.aLab.B + t*(e.bLab.B-e.aLab.B),
			}
			dl, da, db := c.L-n.L, c.A-n.A, c.B-n.B
			if d := dl*dl + da*da + db*db; d < bestDist {
				bestDist = d
				for k := 0; k < 3; k++ {
					best[k] = e.aLin[k] + t*(e.bLin[k]-e.aLin[k])
				}
			}
		}
		blend := smoothstep(gamutThreshold, gamutThresholdMax, math.Sqrt(bestDist)) * strength
		for k := 0; k < 3; k++ {
			px[k] = clamp(px[k]+blend*(best[k]-px[k]), 0, 1)
		}
	})
}

// nearestOnSegment returns how far along [a,b] the closest point to p sits, as a
// fraction clamped into the segment. Clamping is what makes the vertices fall out
// of an edge only search.
func nearestOnSegment(p, a, b OkLab) float64 {
	ex, ey, ez := b.L-a.L, b.A-a.A, b.B-a.B
	lenSq := ex*ex + ey*ey + ez*ez
	if lenSq < 1e-10 {
		return 0
	}
	dot := (p.L-a.L)*ex + (p.A-a.A)*ey + (p.B-a.B)*ez
	return clamp(dot/lenSq, 0, 1)
}
