# Using spectra6

spectra6 takes a photo or graphic and converts it to something an E Ink Spectra 6 panel can actually show well. The panel only has six inks, so most images need help: colours get pushed toward what the panel can reproduce, tones get compressed to fit its real black-to-white range, and the result gets dithered onto the six inks.

## Basic commands

```
spectra6 photo.jpg              open the interactive TUI on that image
spectra6 *.jpg                  same, use n and p to move through the set
spectra6 -batch *.jpg           convert everything and quit, no TUI
spectra6 -batch photos/         same, for every image in photos/, output to photos/epaper
spectra6 -dry-run photo.jpg     print the analysis, write nothing
spectra6 -calibrate             measure your own panel
```

If stdin or stdout isn't a terminal, spectra6 switches to batch mode automatically, so you can use it in scripts and pipes without passing `-batch` yourself.

## The interactive view (TUI)

Running spectra6 on one or more images opens a live preview of what the panel will show, with knobs down the side you can adjust before saving.

| Key | Action |
|---|---|
| `↑` / `↓` or `j` / `k` or `Tab` | move focus between knobs |
| `←` / `→` or `h` / `l` | adjust the focused knob |
| `v` | cycle preview: panel simulation, device colours, source image |
| `n` / `p` | next / previous file |
| `o` | open a file by typing a path or glob |
| `O` | open the current preview in your system image viewer |
| `s` | save the result at full panel resolution |
| `c` | open guided calibration |
| `r` | reset knobs to defaults |
| `q` | quit |

In iTerm2 and WezTerm the preview is drawn with real pixels through the terminal's inline image protocol. Other terminals fall back to a coarse block-character rendering.

## Output

Saved files land in `./epaper` by default, or `<folder>/epaper` when you point spectra6 at a folder instead of a glob. Pass `-out` to override either way. Each output is an indexed PNG using the panel's ink order: black, white, yellow, red, blue, green. Pass `-bmp` for a 24-bit BMP instead, and `-sim` to also save a preview PNG of what the panel will display, alongside the real output.

## Install

```
make install
```

Builds with Go 1.26 and installs the binary to `~/bin`. Or run `go install github.com/TjarcoKerssens/spectra-6-paintings/cmd/spectra6@latest`.
