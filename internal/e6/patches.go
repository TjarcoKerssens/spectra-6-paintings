package e6

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"os"
	"path/filepath"
)

// PatchCols and PatchRows lay out the calibration target. The sampler assumes the
// same grid, so these two constants are a contract between writing the target and
// reading a photograph of it back.
const (
	PatchCols = 2
	PatchRows = 3
)

// Patches writes the calibration target: six full bleed colour blocks in ink
// order, each marked with index dots so a photograph can never be misread.
//
// The dots sit in a corner and the sampler only reads the middle of each cell, so
// they cannot contaminate a measurement.
func Patches(path string, w, h int) error {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	cw, ch := w/PatchCols, h/PatchRows
	for i, c := range Ideal {
		x, y := (i%PatchCols)*cw, (i/PatchCols)*ch
		draw.Draw(img, image.Rect(x, y, x+cw, y+ch),
			&image.Uniform{color.NRGBA{c[0], c[1], c[2], 255}}, image.Point{}, draw.Src)

		mark := color.NRGBA{255, 255, 255, 255}
		if i == White || i == Yellow {
			mark = color.NRGBA{0, 0, 0, 255}
		}
		r := cw / 40
		if r < 4 {
			r = 4
		}
		for k := 0; k <= i; k++ {
			dx := x + r + k*r*3
			draw.Draw(img, image.Rect(dx, y+r, dx+r, y+2*r),
				&image.Uniform{mark}, image.Point{}, draw.Src)
		}
	}
	return Save(img, path)
}

// SamplePatches reads the six inks back out of a photograph that has been cropped
// to the panel edges.
//
// It takes the median of the middle 40% of each cell. The median throws out dust,
// glare specks and the index dots' neighbours, and staying well inside the cell
// avoids the bezel and any vignetting at the frame edge.
func SamplePatches(path string) (Palette, error) {
	img, err := Open(path)
	if err != nil {
		return Palette{}, err
	}
	b := img.Bounds()
	cw := float64(b.Dx()) / PatchCols
	chh := float64(b.Dy()) / PatchRows
	var out Palette
	for i := range out {
		x0 := b.Min.X + int(float64(i%PatchCols)*cw+0.3*cw)
		x1 := b.Min.X + int(float64(i%PatchCols)*cw+0.7*cw)
		y0 := b.Min.Y + int(float64(i/PatchCols)*chh+0.3*chh)
		y1 := b.Min.Y + int(float64(i/PatchCols)*chh+0.7*chh)
		if x1 <= x0 || y1 <= y0 {
			return Palette{}, fmt.Errorf("photo is too small to sample: %dx%d", b.Dx(), b.Dy())
		}
		var chans [3][]float64
		for y := y0; y < y1; y++ {
			for x := x0; x < x1; x++ {
				c := color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA)
				chans[0] = append(chans[0], float64(c.R))
				chans[1] = append(chans[1], float64(c.G))
				chans[2] = append(chans[2], float64(c.B))
			}
		}
		for c := 0; c < 3; c++ {
			out[i][c] = uint8(median(chans[c]) + 0.5)
		}
	}
	return out, nil
}

// Save writes a PNG or BMP, picked from the file extension, creating the
// directory if needed.
func Save(img image.Image, path string) error {
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return encode(f, img, filepath.Ext(path))
}
