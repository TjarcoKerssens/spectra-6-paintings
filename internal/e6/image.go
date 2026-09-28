package e6

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"os"
	"path/filepath"
	"sort"

	"github.com/disintegration/imaging"
	_ "golang.org/x/image/bmp"  // decode support
	_ "golang.org/x/image/tiff" // decode support
	_ "golang.org/x/image/webp" // decode support
)

// Open decodes an image and applies its EXIF orientation, so a phone photo comes
// out the way it was shot.
//
// Colour management stops at sRGB. An embedded ICC profile is ignored, because Go
// has no ICC engine in the standard library and pulling one in for the rare wide
// gamut input is not worth the weight. Convert to sRGB before feeding anything
// exotic in.
func Open(path string) (image.Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	img, err := imaging.Decode(f, imaging.AutoOrientation(true))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", filepath.Base(path), err)
	}
	return img, nil
}

// Fit sizes an image onto the panel.
//
// The panel size is given portrait. A landscape source flips it, so a wide photo
// fills a wide panel rather than being cropped to a slot. Crop fills the panel and
// loses the edges, contain letterboxes onto white and keeps everything.
func Fit(img image.Image, w, h int, contain bool) image.Image {
	b := img.Bounds()
	if b.Dx() > b.Dy() {
		w, h = h, w
	}
	if contain {
		fitted := imaging.Fit(img, w, h, imaging.Lanczos)
		bg := imaging.New(w, h, color.NRGBA{255, 255, 255, 255})
		return imaging.PasteCenter(bg, fitted)
	}
	return imaging.Fill(img, w, h, imaging.Center, imaging.Lanczos)
}

// ToFrame converts an image to linear RGB working pixels, compositing any
// transparency onto white the way a panel would show it.
func ToFrame(img image.Image) *Frame {
	b := img.Bounds()
	f := NewFrame(b.Dx(), b.Dy())
	nrgba := imaging.Clone(img)
	for y := 0; y < f.H; y++ {
		for x := 0; x < f.W; x++ {
			i := nrgba.PixOffset(x, y)
			a := float64(nrgba.Pix[i+3]) / 255
			for c := 0; c < 3; c++ {
				v := float64(nrgba.Pix[i+c]) / 255
				v = v*a + (1 - a) // straight onto white
				f.Px[y*f.W+x][c] = SRGBToLinear(v)
			}
		}
	}
	return f
}

// ToImage converts working pixels back to an 8 bit sRGB image.
func (f *Frame) ToImage() *image.NRGBA {
	out := image.NewNRGBA(image.Rect(0, 0, f.W, f.H))
	for i, px := range f.Px {
		o := i * 4
		for c := 0; c < 3; c++ {
			out.Pix[o+c] = uint8(clamp(LinearToSRGB(px[c])*255+0.5, 0, 255))
		}
		out.Pix[o+3] = 255
	}
	return out
}

// Downsample returns a smaller frame for analysis. The gains only need the
// image's colour statistics, and a quarter scale copy carries those with a
// sixteenth of the work.
func (f *Frame) Downsample(factor int) *Frame {
	if factor < 2 {
		return f
	}
	w, h := max(1, f.W/factor), max(1, f.H/factor)
	out := NewFrame(w, h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			var sum [3]float64
			var n float64
			for dy := 0; dy < factor; dy++ {
				for dx := 0; dx < factor; dx++ {
					sx, sy := x*factor+dx, y*factor+dy
					if sx >= f.W || sy >= f.H {
						continue
					}
					p := f.Px[sy*f.W+sx]
					sum[0] += p[0]
					sum[1] += p[1]
					sum[2] += p[2]
					n++
				}
			}
			if n > 0 {
				out.Px[y*w+x] = [3]float64{sum[0] / n, sum[1] / n, sum[2] / n}
			}
		}
	}
	return out
}

// Unsharp adds local contrast back after resizing. Dithering eats fine detail,
// so a little sharpening before it survives where none would.
func Unsharp(img image.Image, amount float64) image.Image {
	if amount <= 0 {
		return img
	}
	src := imaging.Clone(img)
	blurred := imaging.Blur(src, 1.2)
	out := imaging.Clone(src)
	for i := range out.Pix {
		if i%4 == 3 {
			continue
		}
		v := float64(src.Pix[i]) + amount*(float64(src.Pix[i])-float64(blurred.Pix[i]))
		out.Pix[i] = uint8(clamp(v, 0, 255))
	}
	return out
}

// IsGraphic guesses whether an image is flat colour art rather than a photograph.
//
// Art wants no chroma boost, no tone compression and no dithering: its colours are
// already deliberate and error diffusion would only add noise to clean areas. The
// test is coverage. If a handful of colours account for most of a thumbnail, it is
// art.
func IsGraphic(img image.Image) bool {
	small := imaging.Fit(img, 256, 256, imaging.Lanczos)
	counts := map[color.NRGBA]int{}
	b := small.Bounds()
	total := 0
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			c := color.NRGBAModel.Convert(small.At(x, y)).(color.NRGBA)
			counts[c]++
			total++
		}
	}
	if total == 0 {
		return false
	}
	vals := make([]int, 0, len(counts))
	for _, v := range counts {
		vals = append(vals, v)
	}
	sort.Sort(sort.Reverse(sort.IntSlice(vals)))
	top := 0
	for i := 0; i < len(vals) && i < 12; i++ {
		top += vals[i]
	}
	return float64(top)/float64(total) > 0.60
}

// Paletted wraps dither indices as an indexed image using the given colours.
func Paletted(indices []uint8, w, h int, p Palette) *image.Paletted {
	pal := make(color.Palette, len(p))
	for i, c := range p {
		pal[i] = color.NRGBA{c[0], c[1], c[2], 255}
	}
	out := image.NewPaletted(image.Rect(0, 0, w, h), pal)
	copy(out.Pix, indices)
	return out
}

// Simulate renders what the panel will look like: real ink colours, with a slight
// blur standing in for the eye blending the dots at viewing distance.
func Simulate(indices []uint8, w, h int, p Palette) image.Image {
	src := Paletted(indices, w, h, p)
	rgba := image.NewNRGBA(src.Bounds())
	draw.Draw(rgba, src.Bounds(), src, image.Point{}, draw.Src)
	return imaging.Blur(rgba, 0.6)
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// FitInside shrinks an image to fit a square of the given size, keeping aspect.
func FitInside(img image.Image, long int) image.Image {
	return imaging.Fit(img, long, long, imaging.Lanczos)
}
