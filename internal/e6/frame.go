package e6

import (
	"runtime"
	"sync"
)

// Frame is an image held as linear RGB in [0,1]. Everything between decoding and
// dithering operates on one of these, because error diffusion and tone work are
// only correct on linear light.
type Frame struct {
	W, H int
	Px   [][3]float64
}

// NewFrame allocates a black frame.
func NewFrame(w, h int) *Frame {
	return &Frame{W: w, H: h, Px: make([][3]float64, w*h)}
}

// Clone returns an independent copy.
func (f *Frame) Clone() *Frame {
	out := &Frame{W: f.W, H: f.H, Px: make([][3]float64, len(f.Px))}
	copy(out.Px, f.Px)
	return out
}

// each applies fn to every pixel, spread over the available cores. Pixel work
// here is independent, so this is a straight speedup on a 1200x1600 panel image.
func (f *Frame) each(fn func(*[3]float64)) {
	n := runtime.NumCPU()
	if n > len(f.Px) {
		n = 1
	}
	chunk := (len(f.Px) + n - 1) / n
	var wg sync.WaitGroup
	for start := 0; start < len(f.Px); start += chunk {
		end := start + chunk
		if end > len(f.Px) {
			end = len(f.Px)
		}
		wg.Add(1)
		go func(lo, hi int) {
			defer wg.Done()
			for i := lo; i < hi; i++ {
				fn(&f.Px[i])
			}
		}(start, end)
	}
	wg.Wait()
}

// Luma returns the luminance of every pixel.
func (f *Frame) Luma() []float64 {
	out := make([]float64, len(f.Px))
	for i, p := range f.Px {
		out[i] = Luminance(p)
	}
	return out
}
