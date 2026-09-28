package tui

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"strings"

	"github.com/disintegration/imaging"
)

// itermCapable reports whether the terminal understands the iTerm2 inline
// image protocol, OSC 1337. TERM_PROGRAM covers local shells; LC_TERMINAL is
// what iTerm2 sets over ssh. WezTerm speaks the same protocol.
func itermCapable() bool {
	for _, k := range []string{"TERM_PROGRAM", "LC_TERMINAL"} {
		switch os.Getenv(k) {
		case "iTerm.app", "WezTerm":
			return true
		}
	}
	return false
}

// encodeInline turns an image into the base64 PNG payload OSC 1337 carries.
func encodeInline(img image.Image) string {
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return ""
	}
	return base64.StdEncoding.EncodeToString(buf.Bytes())
}

// inlineImage draws pre-encoded pixels into a cell rectangle and puts the
// cursor back where it was, so the surrounding TUI never notices.
//
// The trick that makes this coexist with bubbletea: the sequence is embedded
// in a line near the bottom of the frame, so within any frame it is written
// after the text lines above it. When a sidebar line is rewritten it wipes the
// strip of image it crosses, and this repaints the whole image a few bytes
// later in the same flush.
func inlineImage(b64 string, x, y, cols, rows int) string {
	return fmt.Sprintf("\x1b7\x1b[%d;%dH\x1b]1337;File=inline=1;width=%d;height=%d;preserveAspectRatio=1:%s\a\x1b8",
		y+1, x+1, cols, rows, b64)
}

// renderImage draws an image with half block characters, the fallback for
// terminals without an inline image protocol.
//
// One cell carries two pixels, the upper half as the foreground of the block and
// the lower half as its background, which doubles the vertical resolution for
// free. This works in every terminal. Press O in the app to open the real
// simulation in an image viewer when the detail matters.
func renderImage(img image.Image, cols, rows int) string {
	if cols < 2 || rows < 2 || img == nil {
		return ""
	}
	// Two pixels per cell vertically, and terminal cells are about twice as tall
	// as they are wide, so the pixel grid is cols wide by rows*2 high.
	fitted := imaging.Fit(img, cols, rows*2, imaging.Lanczos)
	b := fitted.Bounds()
	w, h := b.Dx(), b.Dy()

	var sb strings.Builder
	padLeft := strings.Repeat(" ", maxInt(0, (cols-w)/2))
	for y := 0; y < h; y += 2 {
		sb.WriteString(padLeft)
		for x := 0; x < w; x++ {
			top := color.NRGBAModel.Convert(fitted.At(b.Min.X+x, b.Min.Y+y)).(color.NRGBA)
			bottom := top
			if y+1 < h {
				bottom = color.NRGBAModel.Convert(fitted.At(b.Min.X+x, b.Min.Y+y+1)).(color.NRGBA)
			}
			sb.WriteString(cell(top, bottom))
		}
		sb.WriteString("\x1b[0m")
		if y+2 < h {
			sb.WriteByte('\n')
		}
	}
	return sb.String()
}

// cell writes one half block with true colour foreground and background.
func cell(top, bottom color.NRGBA) string {
	var sb strings.Builder
	sb.WriteString("\x1b[38;2;")
	writeRGB(&sb, top)
	sb.WriteString(";48;2;")
	writeRGB(&sb, bottom)
	sb.WriteString("m▀")
	return sb.String()
}

func writeRGB(sb *strings.Builder, c color.NRGBA) {
	sb.WriteString(itoa(int(c.R)))
	sb.WriteByte(';')
	sb.WriteString(itoa(int(c.G)))
	sb.WriteByte(';')
	sb.WriteString(itoa(int(c.B)))
}

func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	var buf [3]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	return string(buf[i:])
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
