// Package tui is the interactive front end: tune the conversion while watching
// what the panel would actually show.
package tui

import (
	"fmt"
	"image"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/TjarcoKerssens/spectra-6-paintings/internal/e6"
)

// Config is what the command line hands over.
type Config struct {
	Files     []string
	Params    e6.Params
	Palette   e6.Palette
	Out       string
	BMP       bool
	Sim       bool
	Calibrate bool
}

type screen int

const (
	screenMain screen = iota
	screenPrompt
	screenCalibrate
)

const (
	viewSim = iota
	viewDevice
	viewSource
	viewCount
)

var viewNames = [viewCount]string{"panel simulation", "device colours", "source"}

// previewLong caps how big an image is dithered for the preview. The half block
// display is far smaller than this, but dithering at a realistic size and then
// shrinking is what makes the preview look like the panel rather than like a
// mosaic.
const previewLong = 600

// Model is the whole application state.
type Model struct {
	cfg      Config
	files    []string
	idx      int
	params   e6.Params
	defaults e6.Params
	palette  e6.Palette

	fitted  image.Image
	preview image.Image
	report  e6.Report
	gains   []float64

	screen screen
	focus  int
	view   int
	knobs  []knob

	prompt textinput.Model
	cal    calModel

	w, h   int
	status string
	gen    int
	busy   bool

	// iterm is true when the terminal can draw real pixels via OSC 1337.
	iterm      bool
	previewB64 string
	frame      int
}

type loadedMsg struct {
	gen    int
	fitted image.Image
	err    error
}

type renderedMsg struct {
	gen     int
	preview image.Image
	b64     string
	report  e6.Report
	gains   []float64
}

type statusMsg string

// newModel builds a ready to use model. Everything that has to be initialised
// lives here, so no caller can forget a piece and hit a nil widget at runtime.
func newModel(cfg Config) *Model {
	ti := textinput.New()
	ti.Placeholder = "path or glob, enter to load"
	ti.Prompt = "open: "
	return &Model{
		cfg:      cfg,
		files:    cfg.Files,
		params:   cfg.Params,
		defaults: cfg.Params,
		palette:  cfg.Palette,
		knobs:    knobs(),
		prompt:   ti,
		iterm:    itermCapable(),
	}
}

// Run starts the interface.
func Run(cfg Config) error {
	m := newModel(cfg)
	if cfg.Calibrate {
		m.screen = screenCalibrate
		m.cal = newCalModel(cfg.Palette, cfg.Params)
	}
	_, err := tea.NewProgram(m, tea.WithAltScreen()).Run()
	return err
}

func (m *Model) Init() tea.Cmd { return m.reload() }

// reload refits the current file, which is needed whenever the file or the panel
// geometry changes.
func (m *Model) reload() tea.Cmd {
	if len(m.files) == 0 {
		return nil
	}
	m.gen++
	gen := m.gen
	path := m.files[m.idx]
	prm := m.params
	m.busy = true
	return func() tea.Msg {
		img, err := e6.Open(path)
		if err != nil {
			return loadedMsg{gen: gen, err: err}
		}
		return loadedMsg{gen: gen, fitted: e6.Fit(img, prm.Width, prm.Height, prm.Contain)}
	}
}

// rerender redoes the analysis and the conversion at preview size.
func (m *Model) rerender() tea.Cmd {
	if m.fitted == nil {
		return nil
	}
	m.gen++
	gen := m.gen
	fitted := m.fitted
	prm := m.params
	pal := m.palette
	view := m.view
	iterm := m.iterm
	m.busy = true
	return func() tea.Msg {
		small := shrink(fitted, previewLong)
		gains, rep := e6.AnalyseImage(small, pal, prm)
		var out image.Image
		if view == viewSource {
			out = small
		} else {
			res := e6.Convert(small, pal, prm, gains, rep)
			if view == viewDevice {
				out = e6.Paletted(res.Indices, res.W, res.H, e6.Ideal)
			} else {
				out = e6.Simulate(res.Indices, res.W, res.H, pal)
			}
		}
		msg := renderedMsg{gen: gen, preview: out, report: rep, gains: gains}
		if iterm {
			// Encoding here keeps the PNG work off the UI loop.
			msg.b64 = encodeInline(out)
		}
		return msg
	}
}

func shrink(img image.Image, long int) image.Image {
	b := img.Bounds()
	if b.Dx() <= long && b.Dy() <= long {
		return img
	}
	return e6.FitInside(img, long)
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
		return m, nil

	case loadedMsg:
		if msg.gen != m.gen {
			return m, nil
		}
		m.busy = false
		if msg.err != nil {
			m.status = msg.err.Error()
			m.fitted = nil
			return m, nil
		}
		m.fitted = msg.fitted
		return m, m.rerender()

	case renderedMsg:
		if msg.gen != m.gen {
			return m, nil
		}
		m.busy = false
		m.preview, m.report, m.gains = msg.preview, msg.report, msg.gains
		m.previewB64 = msg.b64
		return m, nil

	case statusMsg:
		m.status = string(msg)
		return m, nil

	case tea.KeyMsg:
		switch m.screen {
		case screenPrompt:
			return m.updatePrompt(msg)
		case screenCalibrate:
			return m.updateCalibrate(msg)
		}
		return m.updateMain(msg)
	}
	return m, nil
}

func (m *Model) updateMain(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "up", "k":
		m.focus = (m.focus - 1 + len(m.knobs)) % len(m.knobs)
	case "down", "j", "tab":
		m.focus = (m.focus + 1) % len(m.knobs)
	case "left", "h", "right", "l":
		dir := 1
		if msg.String() == "left" || msg.String() == "h" {
			dir = -1
		}
		k := m.knobs[m.focus]
		k.adjust(m, dir)
		if k.reload {
			return m, m.reload()
		}
		return m, m.rerender()
	case "v":
		m.view = (m.view + 1) % viewCount
		return m, m.rerender()
	case "n":
		if len(m.files) > 0 {
			m.idx = (m.idx + 1) % len(m.files)
			return m, m.reload()
		}
	case "p":
		if len(m.files) > 0 {
			m.idx = (m.idx - 1 + len(m.files)) % len(m.files)
			return m, m.reload()
		}
	case "r":
		m.params = m.defaults
		return m, m.reload()
	case "s":
		return m, m.save()
	case "O":
		return m, m.openExternally()
	case "o":
		m.screen = screenPrompt
		m.prompt.SetValue("")
		m.prompt.Focus()
		return m, textinput.Blink
	case "c":
		m.screen = screenCalibrate
		m.cal = newCalModel(m.palette, m.params)
		return m, tea.ClearScreen
	}
	return m, nil
}

func (m *Model) updatePrompt(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "ctrl+c":
		m.screen = screenMain
		return m, nil
	case "tab":
		m.completePrompt()
		return m, nil
	case "enter":
		pattern := strings.TrimSpace(m.prompt.Value())
		m.screen = screenMain
		if pattern == "" {
			return m, nil
		}
		if strings.HasPrefix(pattern, "~/") {
			if home, err := os.UserHomeDir(); err == nil {
				pattern = filepath.Join(home, pattern[2:])
			}
		}
		found, err := filepath.Glob(pattern)
		if err != nil || len(found) == 0 {
			m.status = "nothing matched " + pattern
			return m, nil
		}
		sort.Strings(found)
		m.files, m.idx = found, 0
		return m, m.reload()
	}
	var cmd tea.Cmd
	m.prompt, cmd = m.prompt.Update(msg)
	return m, cmd
}

// completePrompt expands the prompt's value to the longest path all matches
// share, shell tab completion style. A "~/" prefix is expanded to glob and
// collapsed back afterwards so the input stays readable.
func (m *Model) completePrompt() {
	val := m.prompt.Value()
	pattern, home := val, ""
	if strings.HasPrefix(val, "~/") {
		if h, err := os.UserHomeDir(); err == nil {
			home = h
			pattern = filepath.Join(h, val[2:])
		}
	}
	matches, err := filepath.Glob(pattern + "*")
	if err != nil || len(matches) == 0 {
		return
	}
	sort.Strings(matches)
	completed := commonPrefix(matches)
	if len(matches) == 1 {
		if fi, err := os.Stat(matches[0]); err == nil && fi.IsDir() {
			completed += string(filepath.Separator)
		}
	}
	if home != "" {
		completed = "~" + strings.TrimPrefix(completed, home)
	}
	m.prompt.SetValue(completed)
	m.prompt.CursorEnd()
}

// commonPrefix returns the longest string every entry starts with.
func commonPrefix(s []string) string {
	prefix := s[0]
	for _, v := range s[1:] {
		for !strings.HasPrefix(v, prefix) {
			prefix = prefix[:len(prefix)-1]
		}
	}
	return prefix
}

// save writes the current settings at full panel resolution.
func (m *Model) save() tea.Cmd {
	if len(m.files) == 0 || m.fitted == nil {
		return func() tea.Msg { return statusMsg("nothing loaded to save") }
	}
	path := m.files[m.idx]
	prm := m.params
	pal := m.palette
	cfg := m.cfg
	fitted := m.fitted
	return func() tea.Msg {
		gains, rep := e6.AnalyseImage(fitted, pal, prm)
		res := e6.Convert(fitted, pal, prm, gains, rep)
		shown := e6.Ideal
		if prm.PaletteOut == "measured" {
			shown = pal
		}
		stem := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
		ext := ".png"
		if cfg.BMP {
			ext = ".bmp"
		}
		dst := filepath.Join(cfg.Out, stem+"_e6"+ext)
		if err := e6.Save(e6.Paletted(res.Indices, res.W, res.H, shown), dst); err != nil {
			return statusMsg("save failed: " + err.Error())
		}
		if cfg.Sim {
			sim := filepath.Join(cfg.Out, stem+"_e6_sim.png")
			if err := e6.Save(e6.Simulate(res.Indices, res.W, res.H, pal), sim); err != nil {
				return statusMsg("simulation failed: " + err.Error())
			}
		}
		return statusMsg("wrote " + dst)
	}
}

// openExternally hands the current preview to the system image viewer, for when
// the half block version is not enough to judge by.
func (m *Model) openExternally() tea.Cmd {
	if m.preview == nil {
		return func() tea.Msg { return statusMsg("nothing to open yet") }
	}
	img := m.preview
	return func() tea.Msg {
		dst := filepath.Join(os.TempDir(), "spectra6-preview.png")
		if err := e6.Save(img, dst); err != nil {
			return statusMsg(err.Error())
		}
		opener := "xdg-open"
		if runtime.GOOS == "darwin" {
			opener = "open"
		}
		if err := exec.Command(opener, dst).Start(); err != nil {
			return statusMsg("could not open a viewer: " + err.Error())
		}
		return statusMsg("opened " + dst)
	}
}

var (
	styleLabel   = lipgloss.NewStyle().Faint(true)
	styleFocus   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("212"))
	styleValue   = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	styleHelp    = lipgloss.NewStyle().Faint(true)
	styleStatus  = lipgloss.NewStyle().Foreground(lipgloss.Color("120"))
	stylePanel   = lipgloss.NewStyle().Border(lipgloss.NormalBorder(), false, true, false, false).PaddingRight(1)
	styleHeading = lipgloss.NewStyle().Bold(true)
)

const sidebarWidth = 28

func (m *Model) View() string {
	if m.screen == screenCalibrate {
		return m.viewCalibrate()
	}

	w, h := m.w, m.h
	if w == 0 || h == 0 {
		return "starting..."
	}

	previewW := w - sidebarWidth - 3
	analysis := m.analysis()
	analysisH := strings.Count(analysis, "\n") + 1
	previewH := h - analysisH - 3

	previewH = maxInt(previewH, 2)
	inline := m.iterm && m.previewB64 != ""
	cells := ""
	if !inline {
		cells = renderImage(m.preview, previewW, previewH)
	}
	side := stylePanel.Width(sidebarWidth).Height(previewH).Render(m.sidebar())
	// Both columns get the same height so the divider runs the full way down
	// instead of stopping where the shorter column happens to end.
	body := lipgloss.JoinHorizontal(lipgloss.Top,
		side,
		lipgloss.NewStyle().Width(previewW).Height(previewH).Render(cells),
	)

	footer := styleHelp.Render(
		"↑↓ pick  ←→ adjust  s save  v view  n/p file  o open  O viewer  c calibrate  r reset  q quit")
	if m.screen == screenPrompt {
		footer = m.prompt.View()
	}

	status := m.status
	if m.busy {
		status = "working..."
	}
	overlay := ""
	if inline {
		// Real pixels drawn over the blank preview box. The frame counter
		// changes this line every render, forcing bubbletea to rewrite it and
		// so repaint the image after any text line that crossed it.
		m.frame++
		imgX := lipgloss.Width(strings.Split(side, "\n")[0])
		overlay = inlineImage(m.previewB64, imgX, 0, previewW, previewH) +
			strings.Repeat("\x1b[0m", 1+m.frame%2)
	}
	return strings.Join([]string{
		body,
		analysis,
		overlay + styleStatus.Render(status),
		footer,
	}, "\n")
}

func (m *Model) sidebar() string {
	var b strings.Builder
	title := "no image"
	if len(m.files) > 0 {
		title = fmt.Sprintf("%s  %d/%d", filepath.Base(m.files[m.idx]), m.idx+1, len(m.files))
	}
	b.WriteString(styleHeading.Render(truncate(title, sidebarWidth-1)) + "\n\n")
	for i, k := range m.knobs {
		marker := "  "
		label := styleLabel.Render(k.label)
		if i == m.focus {
			marker = styleFocus.Render("▸ ")
			label = styleFocus.Render(k.label)
		}
		b.WriteString(marker + label + "\n")
		b.WriteString("    " + styleValue.Render(k.value(m)) + "\n")
	}
	return b.String()
}

func (m *Model) analysis() string {
	if m.fitted == nil {
		return styleLabel.Render("press o to open an image")
	}
	name := "image"
	if len(m.files) > 0 {
		name = filepath.Base(m.files[m.idx])
	}
	lines := m.report.Lines(name)
	lines = append(lines, styleLabel.Render(fmt.Sprintf(
		"view %s   palette %s", viewNames[m.view], paletteSource(m.palette))))
	return strings.Join(lines, "\n")
}

func paletteSource(p e6.Palette) string {
	if p == e6.Measured {
		return "built in measurement, press c to calibrate yours"
	}
	return "calibrated"
}

func truncate(s string, n int) string {
	r := []rune(s)
	if n <= 1 || len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}
