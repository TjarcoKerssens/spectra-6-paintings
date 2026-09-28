package e6

import (
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"io"
	"strings"

	"golang.org/x/image/bmp"
)

// encode writes an image in the format the extension asks for. Indexed PNG keeps
// the palette indices intact, which is what most panel firmware wants; BMP is
// there because some loaders only take 24 bit BMP.
func encode(w io.Writer, img image.Image, ext string) error {
	switch strings.ToLower(ext) {
	case ".png", "":
		return png.Encode(w, img)
	case ".bmp":
		// bmp.Encode writes an 8 bit colour mapped file for a paletted image,
		// but the loaders that need BMP at all tend to only read 24 bit, which
		// is the whole reason the flag exists. Flatten before encoding.
		if pal, ok := img.(*image.Paletted); ok {
			flat := image.NewNRGBA(pal.Bounds())
			draw.Draw(flat, flat.Bounds(), pal, pal.Bounds().Min, draw.Src)
			return bmp.Encode(w, flat)
		}
		return bmp.Encode(w, img)
	}
	return fmt.Errorf("unsupported output format %q, use .png or .bmp", ext)
}
