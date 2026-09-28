package tui

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/TjarcoKerssens/spectra-6-paintings/internal/e6"
)

const calSteps = `Calibrating your panel

Every Spectra 6 unit mixes its pigments a little differently, and every gain this
program computes comes from those six numbers. Measuring your own panel is the
single biggest quality win available. It takes about ten minutes.

 1  Focus "write target" and press enter. It writes a PNG on this machine, at your
    panel's pixel size, to the path in the field above, and opens it locally so you
    can check it. Each patch carries index dots, 1 for black through 6 for green,
    in the order black, white, yellow, red, blue, green.
 2  Load that PNG onto the panel however you normally do, and let it finish a full
    refresh before you photograph it.
 3  Lay the panel flat. Light it with two lamps at 45 degrees on either side, both
    at 6500K, with nothing else lighting the room. Look at the glass and move the
    lamps until no reflection lands in it.
 4  Put a sheet of plain white printer paper next to the panel in the same light.
    That sheet is the reference that divides your lighting back out.
 5  Photograph the panel square on, in RAW, with the paper in frame. Keep your body
    and the camera out of the light path.
 6  Open the RAW with auto tone and auto white balance switched off. Sample the
    paper with a colour picker, macOS ships Digital Color Meter, and put that RGB
    in "paper reference".
 7  Crop a copy of the photo to exactly the panel edges, same orientation as the
    target, export PNG or JPEG, put its path in "photo" and run "sample". The six
    patches are read for you. You can also type all six by hand.
 8  Run "compute", check the numbers, then "save". They land in
    ~/.config/spectra6/palette.json and every later run picks them up.

If your RAW developer applied a linear tone curve, switch "linear light" on so the
numbers get gamma encoded before they are stored.`

// calibration form rows, in focus order
const (
	rowTarget = iota
	rowPhoto
	rowPaper
	rowInk     // six of these
	rowLinear  = rowInk + 6
	rowWrite   = rowLinear + 1
	rowSample  = rowLinear + 2
	rowCompute = rowLinear + 3
	rowSave    = rowLinear + 4
	rowCount   = rowLinear + 5
)

type calModel struct {
	inputs  [9]textinput.Model // target, photo, paper, then six inks
	linear  bool
	focus   int
	result  *e6.Palette
	message string
	params  e6.Params
	scroll  int
}

func newCalModel(p e6.Palette, prm e6.Params) calModel {
	var m calModel
	m.params = prm
	for i := range m.inputs {
		ti := textinput.New()
		ti.Prompt = ""
		ti.CharLimit = 200
		ti.Width = 30
		switch i {
		case 0:
			ti.SetValue("spectra6_patches.png")
		case 1:
			ti.Placeholder = "photo cropped to the panel edges"
		case 2:
			ti.SetValue("255, 255, 255")
		default:
			c := p[i-3]
			ti.SetValue(fmt.Sprintf("%d, %d, %d", c[0], c[1], c[2]))
		}
		m.inputs[i] = ti
	}
	m.inputs[0].Focus()
	return m
}

func (m *Model) updateCalibrate(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	c := &m.cal
	switch msg.String() {
	case "esc", "ctrl+c":
		m.screen = screenMain
		return m, m.rerender()
	case "tab", "down":
		c.setFocus(c.focus + 1)
		return m, nil
	case "shift+tab", "up":
		c.setFocus(c.focus - 1)
		return m, nil
	case "left", "right":
		if c.focus == rowLinear {
			c.linear = !c.linear
		}
		return m, nil
	case "pgup":
		c.scroll -= 5
		return m, nil
	case "pgdown":
		c.scroll += 5
		return m, nil
	case "enter":
		return m.calAction()
	}
	if c.focus < len(c.inputs) {
		var cmd tea.Cmd
		c.inputs[c.focus], cmd = c.inputs[c.focus].Update(msg)
		return m, cmd
	}
	return m, nil
}

func (c *calModel) setFocus(i int) {
	c.focus = ((i % rowCount) + rowCount) % rowCount
	for j := range c.inputs {
		if j == c.focus {
			c.inputs[j].Focus()
		} else {
			c.inputs[j].Blur()
		}
	}
}

func (m *Model) calAction() (tea.Model, tea.Cmd) {
	c := &m.cal
	switch c.focus {
	case rowLinear:
		c.linear = !c.linear
	case rowWrite:
		path := strings.TrimSpace(c.inputs[0].Value())
		if path == "" {
			path = "spectra6_patches.png"
		}
		abs, _ := filepath.Abs(path)
		if err := e6.Patches(abs, c.params.Width, c.params.Height); err != nil {
			c.message = err.Error()
			break
		}
		opener := "xdg-open"
		if runtime.GOOS == "darwin" {
			opener = "open"
		}
		_ = exec.Command(opener, abs).Start()
		c.message = "wrote " + abs + "\nload it onto the panel and let it fully refresh"
	case rowSample:
		path := strings.TrimSpace(c.inputs[1].Value())
		got, err := e6.SamplePatches(path)
		if err != nil {
			c.message = "could not sample: " + err.Error()
			break
		}
		for i, v := range got {
			c.inputs[3+i].SetValue(fmt.Sprintf("%d, %d, %d", v[0], v[1], v[2]))
		}
		c.message = "sampled six patches, check them, then compute"
	case rowCompute:
		p, err := c.compute()
		if err != nil {
			c.message = "cannot compute: " + err.Error()
			c.result = nil
			break
		}
		c.result = &p
		c.message = "computed, check the numbers then save"
	case rowSave:
		p, err := c.compute()
		if err != nil {
			c.message = "cannot save: " + err.Error()
			break
		}
		if err := e6.SavePalette(p, ""); err != nil {
			c.message = "cannot save: " + err.Error()
			break
		}
		m.palette = p
		c.result = &p
		c.message = "saved to " + e6.ConfigPath()
		m.screen = screenMain
		m.status = "palette saved to " + e6.ConfigPath()
		return m, m.rerender()
	}
	return m, nil
}

func (c *calModel) compute() (e6.Palette, error) {
	paper, err := parseRGB(c.inputs[2].Value())
	if err != nil {
		return e6.Palette{}, fmt.Errorf("paper reference: %w", err)
	}
	var raw e6.Palette
	for i := range raw {
		v, err := parseRGB(c.inputs[3+i].Value())
		if err != nil {
			return e6.Palette{}, fmt.Errorf("%s: %w", e6.InkNames[i], err)
		}
		raw[i] = v
	}
	return e6.Normalise(raw, paper, c.linear)
}

func parseRGB(s string) (e6.RGB, error) {
	parts := strings.FieldsFunc(s, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t'
	})
	if len(parts) != 3 {
		return e6.RGB{}, fmt.Errorf("want three numbers, got %q", strings.TrimSpace(s))
	}
	var out e6.RGB
	for i, p := range parts {
		v, err := strconv.ParseFloat(p, 64)
		if err != nil {
			return e6.RGB{}, fmt.Errorf("%q is not a number", p)
		}
		if v < 0 || v > 255 {
			return e6.RGB{}, fmt.Errorf("%g is outside 0..255", v)
		}
		out[i] = uint8(v + 0.5)
	}
	return out, nil
}

var styleAction = lipgloss.NewStyle().Foreground(lipgloss.Color("117"))

func (m *Model) viewCalibrate() string {
	c := &m.cal
	form := m.calForm()
	formLines := strings.Count(form, "\n") + 1

	// The form always stays on screen. The instructions get whatever is left and
	// scroll, because they are reference material you read once and then work
	// from, while the form is what you are actually operating.
	steps := strings.Split(calSteps, "\n")
	avail := m.h - formLines - 3
	if avail < 3 {
		avail = 3
	}
	head := steps
	scrollNote := ""
	if len(steps) > avail {
		maxScroll := len(steps) - avail
		if c.scroll < 0 {
			c.scroll = 0
		}
		if c.scroll > maxScroll {
			c.scroll = maxScroll
		}
		head = steps[c.scroll : c.scroll+avail]
		scrollNote = fmt.Sprintf("  (lines %d-%d of %d, pgup/pgdn)",
			c.scroll+1, c.scroll+avail, len(steps))
	}

	return styleLabel.Render(strings.Join(head, "\n")) +
		styleHelp.Render(scrollNote) + "\n" + form
}

func (m *Model) calForm() string {
	c := &m.cal
	var b strings.Builder
	field := func(row int, label string, in textinput.Model) {
		marker := "  "
		if c.focus == row {
			marker = styleFocus.Render("▸ ")
		}
		b.WriteString(fmt.Sprintf("%s%-16s %s\n", marker, label, in.View()))
	}
	field(rowTarget, "target file", c.inputs[0])
	field(rowPhoto, "photo", c.inputs[1])
	field(rowPaper, "paper reference", c.inputs[2])
	for i, name := range e6.InkNames {
		field(rowInk+i, name, c.inputs[3+i])
	}

	marker := "  "
	if c.focus == rowLinear {
		marker = styleFocus.Render("▸ ")
	}
	state := "off"
	if c.linear {
		state = "on"
	}
	b.WriteString(fmt.Sprintf("%s%-16s %s\n", marker, "linear light", state))

	var actions []string
	for _, a := range []struct {
		row  int
		name string
	}{{rowWrite, "write target"}, {rowSample, "sample from photo"},
		{rowCompute, "compute"}, {rowSave, "save"}} {
		label := styleAction.Render(a.name)
		if c.focus == a.row {
			label = styleFocus.Render("▸ " + a.name)
		} else {
			label = "  " + label
		}
		actions = append(actions, label)
	}
	b.WriteString(strings.Join(actions, "   ") + "\n")

	if c.result != nil {
		for i, name := range e6.InkNames {
			b.WriteString(fmt.Sprintf("  %-7s %3d,%3d,%3d  was %3d,%3d,%3d\n",
				name, c.result[i][0], c.result[i][1], c.result[i][2],
				m.palette[i][0], m.palette[i][1], m.palette[i][2]))
		}
	}
	if c.message != "" {
		b.WriteString(styleStatus.Render(c.message) + "\n")
	}
	b.WriteString(styleHelp.Render("↑↓ move  ←→ toggle  enter run  pgup/pgdn steps  esc back"))
	return b.String()
}
