# Calibrating your panel

Every Spectra 6 panel mixes its six pigments a bit differently. Everything spectra6 does, from the colour boost to the dither, is based on the six colours it thinks your panel can actually produce. So calibrating matters more than any of the tuning knobs.

Until you calibrate, spectra6 uses a built-in measurement taken from a 7.3 inch panel. It's close, but it's not your panel.

## Steps

1. Run `spectra6 -calibrate`. This opens the TUI straight into the guided calibration screen (press `c` from the main screen to get there manually).
2. spectra6 writes a test target image, a grid of patches covering the six inks.
3. Send that image to your panel and let it display.
4. Photograph the panel under even, diffuse light, with a sheet of plain white paper in frame. The white paper gives the tool a reference to correct for your lighting.
5. Load that photo back into spectra6. It reads the patches and computes your panel's real palette from them.

The result is saved to `~/.config/spectra6/palette.json` and used automatically from then on. Pass `-palette path/to/file.json` to use a different one, for example if you're preparing images for more than one panel.

## Writing the target without the TUI

```
spectra6 -patches target.png
```

Writes the calibration target to `target.png` and exits, if you'd rather drive the rest of the process yourself.
