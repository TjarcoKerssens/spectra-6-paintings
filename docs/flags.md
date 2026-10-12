# Flag reference

Run `spectra6 -h` for the same list straight from the binary; this is that list with a bit more context.

## Output

| Flag | Default | Meaning |
|---|---|---|
| `-out` | `epaper` | output directory |
| `-size` | `1200x1600` | panel size in pixels, portrait |
| `-bmp` | off | write 24-bit BMP instead of indexed PNG |
| `-sim` | off | also write a panel simulation next to each output |
| `-palette-out` | `ideal` | colours stored in the saved file: `ideal` (clean six-colour palette) or `measured` (your panel's actual pigments) |
| `-contain` | off | letterbox instead of crop when the image doesn't match the panel's aspect ratio |

## Image processing

| Flag | Default | Meaning |
|---|---|---|
| `-content` | `auto` | `auto`, `photo`, or `graphic` (flat graphics skip the colour boost since their colours are already deliberate) |
| `-dither` | `burkes` | dither algorithm, see `spectra6 -h` for the full list |
| `-tone` | `auto` | dynamic range compression: `auto`, `off`, or a number 0 to 1 |
| `-gamut` | `auto` | gamut compression: `auto`, `off`, or a number 0 to 1 |
| `-shadows` | `auto` | shadow lift: `auto` or a number 0 to 1 |
| `-highlights` | `0` | highlight squeeze, 0 to 1 |
| `-sat` | `1` | scales the automatic per-hue colour boost; `0` disables it |
| `-max-gain` | `1.6` | ceiling on how much any one hue can be boosted |
| `-vivid` | `1.15` | extra chroma on every colour after tone compression, near greys excepted; `1` disables it |
| `-light` | `0.35` | extra chroma for light colours only, rising toward panel white; `0` disables it |
| `-target` | `90` | chroma percentile aimed at the gamut edge |
| `-sharp` | `0.6` | unsharp amount; `0` disables it |

## Palette and calibration

| Flag | Default | Meaning |
|---|---|---|
| `-palette` | none | palette JSON to use, overrides the calibrated one at `~/.config/spectra6/palette.json` |
| `-calibrate` | off | open the guided calibration screen in the TUI |
| `-patches` | none | write the calibration target image to this path and exit |

## Modes

| Flag | Default | Meaning |
|---|---|---|
| `-batch` | off | convert and exit, skip the TUI |
| `-dry-run` | off | print the analysis only, write nothing |
| `-version` | none | print the version and exit |
