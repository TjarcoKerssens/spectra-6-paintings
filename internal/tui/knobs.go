package tui

import (
	"fmt"
	"strconv"

	"github.com/TjarcoKerssens/spectra-6-paintings/internal/e6"
)

// knob is one adjustable setting. Left and right nudge it, which beats typing
// numbers into a form when you are watching a preview react.
type knob struct {
	label  string
	value  func(*Model) string
	adjust func(*Model, int)
	// reload is true when changing this needs the source image refitted, not just
	// re-rendered.
	reload bool
}

func numKnob(label string, step, lo, hi float64, get func(*e6.Params) *float64) knob {
	return knob{
		label: label,
		value: func(m *Model) string { return trim(*get(&m.params)) },
		adjust: func(m *Model, dir int) {
			p := get(&m.params)
			*p = clampF(*p+float64(dir)*step, lo, hi)
		},
	}
}

func enumKnob(label string, options []string, get func(*e6.Params) *string) knob {
	return knob{
		label: label,
		value: func(m *Model) string { return *get(&m.params) },
		adjust: func(m *Model, dir int) {
			p := get(&m.params)
			i := indexOf(options, *p) + dir
			*p = options[((i%len(options))+len(options))%len(options)]
		},
	}
}

// strengthKnob steps through off, a few fixed amounts, and auto.
func strengthKnob(label string, get func(*e6.Params) *e6.Strength) knob {
	steps := []e6.Strength{
		{}, {Value: 0.25}, {Value: 0.5}, {Value: 0.75}, {Value: 1}, {Auto: true},
	}
	return knob{
		label: label,
		value: func(m *Model) string { return get(&m.params).String() },
		adjust: func(m *Model, dir int) {
			p := get(&m.params)
			i := 0
			for j, s := range steps {
				if s == *p {
					i = j
				}
			}
			i += dir
			*p = steps[((i%len(steps))+len(steps))%len(steps)]
		},
	}
}

func knobs() []knob {
	return []knob{
		enumKnob("content", []string{"auto", "photo", "graphic"},
			func(p *e6.Params) *string { return &p.Content }),
		{
			label: "dither",
			value: func(m *Model) string { return string(m.params.Dither) },
			adjust: func(m *Model, dir int) {
				all := e6.Modes()
				i := indexOf(all, string(m.params.Dither)) + dir
				m.params.Dither = e6.Mode(all[((i%len(all))+len(all))%len(all)])
			},
		},
		numKnob("chroma boost", 0.1, 0, 3, func(p *e6.Params) *float64 { return &p.Sat }),
		numKnob("boost ceiling", 0.1, 1, 3, func(p *e6.Params) *float64 { return &p.MaxGain }),
		numKnob("vivid", 0.05, 0, 2, func(p *e6.Params) *float64 { return &p.Vivid }),
		numKnob("chroma pct", 1, 50, 100, func(p *e6.Params) *float64 { return &p.TargetPct }),
		numKnob("unsharp", 0.1, 0, 3, func(p *e6.Params) *float64 { return &p.Sharp }),
		{
			// The only knob with an auto state, so it steps through a fixed list
			// with auto as one of the stops. Doing this with float arithmetic and
			// a range check is how you end up one rounding error away from never
			// reaching auto again.
			label: "shadow lift",
			value: func(m *Model) string {
				if m.params.Shadows == nil {
					return "auto"
				}
				return trim(*m.params.Shadows)
			},
			adjust: func(m *Model, dir int) {
				stops := []float64{0, 0.1, 0.2, 0.3, 0.4, 0.5, 0.75, 1}
				i := 0 // index 0 is auto, the rest are stops[i-1]
				if m.params.Shadows != nil {
					for j, v := range stops {
						if v == *m.params.Shadows {
							i = j + 1
						}
					}
				}
				n := len(stops) + 1
				i = ((i+dir)%n + n) % n
				if i == 0 {
					m.params.Shadows = nil
					return
				}
				v := stops[i-1]
				m.params.Shadows = &v
			},
		},
		numKnob("highlight squeeze", 0.05, 0, 1,
			func(p *e6.Params) *float64 { return &p.Highlights }),
		strengthKnob("tone", func(p *e6.Params) *e6.Strength { return &p.Tone }),
		strengthKnob("gamut", func(p *e6.Params) *e6.Strength { return &p.Gamut }),
		{
			label:  "fit",
			reload: true,
			value: func(m *Model) string {
				if m.params.Contain {
					return "letterbox"
				}
				return "crop"
			},
			adjust: func(m *Model, _ int) { m.params.Contain = !m.params.Contain },
		},
		{
			label:  "panel",
			reload: true,
			value: func(m *Model) string {
				return fmt.Sprintf("%dx%d", m.params.Width, m.params.Height)
			},
			adjust: func(m *Model, dir int) {
				sizes := [][2]int{{600, 448}, {800, 480}, {960, 640}, {1200, 1600}, {1600, 1200}}
				i := 0
				for j, s := range sizes {
					if s[0] == m.params.Width && s[1] == m.params.Height {
						i = j
					}
				}
				i += dir
				s := sizes[((i%len(sizes))+len(sizes))%len(sizes)]
				m.params.Width, m.params.Height = s[0], s[1]
			},
		},
		enumKnob("saved RGB", []string{"ideal", "measured"},
			func(p *e6.Params) *string { return &p.PaletteOut }),
	}
}

func trim(v float64) string { return strconv.FormatFloat(v, 'g', 4, 64) }

func clampF(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func indexOf(list []string, v string) int {
	for i, s := range list {
		if s == v {
			return i
		}
	}
	return 0
}
