// Command spectra6 prepares images for E Ink Spectra 6 panels.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/disintegration/imaging"

	"github.com/TjarcoKerssens/spectra-6-paintings/internal/e6"
	"github.com/TjarcoKerssens/spectra-6-paintings/internal/tui"
)

// version is stamped by the Makefile via -ldflags.
var version = "dev"

const usage = `spectra6 prepares images for E Ink Spectra 6 panels.

It measures each image against your panel's real pigment gamut, decides per hue
how much colour to add, compresses what the panel cannot reach, and dithers the
result to the six inks.

  spectra6 photo.jpg              open the TUI on that image
  spectra6 *.jpg                  TUI, n and p walk the set
  spectra6                        TUI with nothing loaded, o opens a file
  spectra6 -calibrate             TUI, straight to guided calibration
  spectra6 -batch *.jpg           convert and exit, no TUI
  spectra6 -batch photos/         convert every image in photos/, write to photos/epaper
  spectra6 -dry-run photo.jpg     print the analysis only
  spectra6 -patches target.png    write the calibration target

Calibration matters more than any knob here. Run spectra6 -calibrate.

Flags:
`

type opts struct {
	out        string
	size       string
	content    string
	dither     string
	tone       string
	gamut      string
	shadows    string
	palette    string
	paletteOut string
	patches    string
	sat        float64
	maxGain    float64
	vivid      float64
	light      float64
	target     float64
	sharp      float64
	highlights float64
	contain    bool
	bmp        bool
	sim        bool
	dryRun     bool
	batch      bool
	calibrate  bool
	version    bool
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "spectra6:", err)
		os.Exit(1)
	}
}

func run() error {
	var o opts
	fs := flag.NewFlagSet("spectra6", flag.ContinueOnError)
	fs.Usage = func() {
		fmt.Fprint(fs.Output(), usage)
		fs.PrintDefaults()
	}
	fs.StringVar(&o.out, "out", "epaper", "output directory")
	fs.StringVar(&o.size, "size", "1200x1600", "panel size in pixels, portrait")
	fs.StringVar(&o.content, "content", "auto", "auto, photo or graphic")
	fs.StringVar(&o.dither, "dither", string(e6.ModeBurkes), "dither: "+strings.Join(e6.Modes(), ", "))
	fs.StringVar(&o.tone, "tone", "auto", "dynamic range compression: auto, off or 0..1")
	fs.StringVar(&o.gamut, "gamut", "auto", "gamut compression: auto, off or 0..1")
	fs.StringVar(&o.shadows, "shadows", "auto", "shadow lift: auto or 0..1")
	fs.Float64Var(&o.highlights, "highlights", 0, "highlight squeeze, 0..1")
	fs.Float64Var(&o.sat, "sat", 1, "scale the automatic per hue boost, 0 disables it")
	fs.Float64Var(&o.maxGain, "max-gain", 1.6, "ceiling on any one hue's boost")
	fs.Float64Var(&o.vivid, "vivid", 1.15, "extra chroma on top of the boost, 1 disables it")
	fs.Float64Var(&o.light, "light", 0.35, "extra chroma for light colours, 0 disables it")
	fs.Float64Var(&o.target, "target", 90, "chroma percentile aimed at the gamut edge")
	fs.Float64Var(&o.sharp, "sharp", 0.6, "unsharp amount, 0 disables")
	fs.BoolVar(&o.contain, "contain", false, "letterbox instead of crop")
	fs.BoolVar(&o.bmp, "bmp", false, "write 24 bit BMP instead of indexed PNG")
	fs.StringVar(&o.paletteOut, "palette-out", "ideal", "RGB stored in the saved file: ideal or measured")
	fs.StringVar(&o.palette, "palette", "", "palette JSON, overrides "+e6.ConfigPath())
	fs.BoolVar(&o.sim, "sim", false, "also write a panel simulation next to each output")
	fs.BoolVar(&o.dryRun, "dry-run", false, "print the analysis and write nothing")
	fs.BoolVar(&o.batch, "batch", false, "convert and exit instead of opening the TUI")
	fs.BoolVar(&o.calibrate, "calibrate", false, "open the guided calibration screen")
	fs.StringVar(&o.patches, "patches", "", "write the calibration target to this file and exit")
	fs.BoolVar(&o.version, "version", false, "print the version and exit")
	if err := fs.Parse(os.Args[1:]); err != nil {
		return nil // flag already reported it
	}

	if o.version {
		fmt.Println("spectra6", version)
		return nil
	}

	prm, err := o.params()
	if err != nil {
		return err
	}
	palette, err := e6.LoadPalette(o.palette)
	if err != nil {
		return err
	}

	if o.patches != "" {
		if err := e6.Patches(o.patches, prm.Width, prm.Height); err != nil {
			return err
		}
		fmt.Printf("%s\nrun  spectra6 -calibrate  for the guided version of what to do with it\n", o.patches)
		return nil
	}

	files, err := expand(fs.Args())
	if err != nil {
		return err
	}
	if !outSet(fs) {
		if dir, ok := singleDir(fs.Args()); ok {
			o.out = filepath.Join(dir, o.out)
		}
	}

	if !o.batch && !o.dryRun && isTerminal() {
		return tui.Run(tui.Config{
			Files:     files,
			Params:    prm,
			Palette:   palette,
			Out:       o.out,
			BMP:       o.bmp,
			Sim:       o.sim,
			Calibrate: o.calibrate,
		})
	}
	if len(files) == 0 {
		return fmt.Errorf("no input images")
	}
	return batch(files, palette, prm, o)
}

func (o opts) params() (e6.Params, error) {
	prm := e6.Defaults()
	var err error
	if prm.Width, prm.Height, err = parseSize(o.size); err != nil {
		return prm, err
	}
	switch o.content {
	case "auto", "photo", "graphic":
		prm.Content = o.content
	default:
		return prm, fmt.Errorf("content must be auto, photo or graphic, got %q", o.content)
	}
	if prm.Dither, err = e6.ParseMode(o.dither); err != nil {
		return prm, err
	}
	if prm.Tone, err = e6.ParseStrength(o.tone); err != nil {
		return prm, fmt.Errorf("tone: %w", err)
	}
	if prm.Gamut, err = e6.ParseStrength(o.gamut); err != nil {
		return prm, fmt.Errorf("gamut: %w", err)
	}
	if s := strings.ToLower(strings.TrimSpace(o.shadows)); s != "auto" {
		v, err := e6.ParseStrength(s)
		if err != nil {
			return prm, fmt.Errorf("shadows: %w", err)
		}
		prm.Shadows = &v.Value
	}
	switch o.paletteOut {
	case "ideal", "measured":
		prm.PaletteOut = o.paletteOut
	default:
		return prm, fmt.Errorf("palette-out must be ideal or measured, got %q", o.paletteOut)
	}
	prm.Sat, prm.MaxGain, prm.TargetPct, prm.Vivid, prm.Light = o.sat, o.maxGain, o.target, o.vivid, o.light
	prm.Sharp, prm.Highlights = o.sharp, o.highlights
	prm.Contain = o.contain
	if prm.MaxGain < 1 {
		return prm, fmt.Errorf("max-gain must be at least 1, got %g", prm.MaxGain)
	}
	if prm.Vivid < 0 {
		return prm, fmt.Errorf("vivid must not be negative, got %g", prm.Vivid)
	}
	if prm.Light < 0 {
		return prm, fmt.Errorf("light must not be negative, got %g", prm.Light)
	}
	if prm.TargetPct <= 0 || prm.TargetPct > 100 {
		return prm, fmt.Errorf("target must be a percentile between 0 and 100, got %g", prm.TargetPct)
	}
	return prm, nil
}

func batch(files []string, palette e6.Palette, prm e6.Params, o opts) error {
	for _, path := range files {
		res, _, err := e6.Run(path, palette, prm)
		if err != nil {
			return err
		}
		for _, line := range res.Report.Lines(filepath.Base(path)) {
			fmt.Println(line)
		}
		if o.dryRun {
			continue
		}

		shown := e6.Ideal
		if prm.PaletteOut == "measured" {
			shown = palette
		}
		stem := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
		ext := ".png"
		if o.bmp {
			ext = ".bmp"
		}
		dst := filepath.Join(o.out, stem+"_e6"+ext)
		if err := e6.Save(e6.Paletted(res.Indices, res.W, res.H, shown), dst); err != nil {
			return err
		}
		fmt.Println("  ->", dst)
		if o.sim {
			sim := filepath.Join(o.out, stem+"_e6_sim.png")
			if err := e6.Save(e6.Simulate(res.Indices, res.W, res.H, palette), sim); err != nil {
				return err
			}
		}
	}
	return nil
}

func parseSize(s string) (int, int, error) {
	var w, h int
	if _, err := fmt.Sscanf(strings.ToLower(strings.TrimSpace(s)), "%dx%d", &w, &h); err != nil {
		return 0, 0, fmt.Errorf("size must look like 1200x1600, got %q", s)
	}
	if w < 8 || h < 8 {
		return 0, 0, fmt.Errorf("size %dx%d is too small", w, h)
	}
	return w, h, nil
}

// expand turns arguments into a file list, resolving any glob the shell left
// alone, listing a directory's images, and keeping the order stable.
func expand(args []string) ([]string, error) {
	var out []string
	for _, a := range args {
		if strings.HasPrefix(a, "-") {
			return nil, fmt.Errorf("%q looks like a flag; flags go before the file arguments", a)
		}
		if fi, err := os.Stat(a); err == nil && fi.IsDir() {
			images, err := dirImages(a)
			if err != nil {
				return nil, err
			}
			out = append(out, images...)
			continue
		}
		matches, err := filepath.Glob(a)
		if err != nil {
			return nil, fmt.Errorf("bad pattern %q: %w", a, err)
		}
		if len(matches) == 0 {
			if _, err := os.Stat(a); err != nil {
				return nil, fmt.Errorf("no such file: %s", a)
			}
			matches = []string{a}
		}
		sort.Strings(matches)
		out = append(out, matches...)
	}
	return out, nil
}

// dirImages lists a directory's files that look like images by extension,
// skipping subdirectories so a previous run's output folder isn't fed back in.
func dirImages(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if _, err := imaging.FormatFromFilename(e.Name()); err != nil {
			continue
		}
		out = append(out, filepath.Join(dir, e.Name()))
	}
	sort.Strings(out)
	if len(out) == 0 {
		return nil, fmt.Errorf("no images in %s", dir)
	}
	return out, nil
}

// singleDir reports whether args names exactly one directory, so batch mode
// can default its output to a subfolder of it instead of ./epaper.
func singleDir(args []string) (string, bool) {
	if len(args) != 1 {
		return "", false
	}
	fi, err := os.Stat(args[0])
	if err != nil || !fi.IsDir() {
		return "", false
	}
	return args[0], true
}

// outSet reports whether -out was passed explicitly, as opposed to holding
// its default value.
func outSet(fs *flag.FlagSet) bool {
	set := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "out" {
			set = true
		}
	})
	return set
}

func isTerminal() bool {
	for _, f := range []*os.File{os.Stdin, os.Stdout} {
		fi, err := f.Stat()
		if err != nil || fi.Mode()&os.ModeCharDevice == 0 {
			return false
		}
	}
	return true
}
