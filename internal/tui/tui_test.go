package tui

import (
	"image"
	"image/color"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/TjarcoKerssens/spectra-6-paintings/internal/e6"
)

func testModel() *Model {
	m := newModel(Config{Params: e6.Defaults(), Palette: e6.Measured})
	m.w, m.h = 120, 40
	return m
}

func key(s string) tea.KeyMsg {
	switch s {
	case "left":
		return tea.KeyMsg{Type: tea.KeyLeft}
	case "right":
		return tea.KeyMsg{Type: tea.KeyRight}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func focusOn(m *Model, label string) {
	for i, k := range m.knobs {
		if k.label == label {
			m.focus = i
			return
		}
	}
	panic("no knob called " + label)
}

func TestKnobAdjustChangesParams(t *testing.T) {
	m := testModel()
	focusOn(m, "chroma boost")
	before := m.params.Sat
	m.Update(key("right"))
	if m.params.Sat <= before {
		t.Fatalf("right should raise the boost, %g -> %g", before, m.params.Sat)
	}
	m.Update(key("left"))
	m.Update(key("left"))
	if m.params.Sat >= before {
		t.Fatalf("left should lower the boost, got %g", m.params.Sat)
	}
}

func TestKnobsClampInsteadOfRunningAway(t *testing.T) {
	m := testModel()
	focusOn(m, "chroma boost")
	for i := 0; i < 100; i++ {
		m.Update(key("left"))
	}
	if m.params.Sat != 0 {
		t.Fatalf("boost should bottom out at 0, got %g", m.params.Sat)
	}
	focusOn(m, "boost ceiling")
	for i := 0; i < 100; i++ {
		m.Update(key("left"))
	}
	if m.params.MaxGain < 1 {
		t.Fatalf("the ceiling must never drop below 1, got %g", m.params.MaxGain)
	}
}

// Shadows is the one knob with an auto state, and stepping past either end has to
// land back on auto rather than on a silently wrong number.
func TestShadowKnobReturnsToAuto(t *testing.T) {
	m := testModel()
	focusOn(m, "shadow lift")
	if m.params.Shadows != nil {
		t.Fatal("shadows should start on auto")
	}
	m.Update(key("right"))
	if m.params.Shadows == nil {
		t.Fatal("one step off auto should give a number")
	}
	// keep stepping and auto has to come back around, or it is unreachable
	backToAuto := false
	for i := 0; i < 40 && !backToAuto; i++ {
		m.Update(key("right"))
		backToAuto = m.params.Shadows == nil
	}
	if !backToAuto {
		t.Fatal("auto became unreachable once you step off it")
	}
	m.Update(key("left"))
	if m.params.Shadows == nil || *m.params.Shadows != 1 {
		t.Fatal("stepping back from auto should land on the top stop")
	}
}

func TestDitherKnobCyclesEveryMode(t *testing.T) {
	m := testModel()
	focusOn(m, "dither")
	seen := map[string]bool{}
	for i := 0; i < len(e6.Modes())*2; i++ {
		seen[string(m.params.Dither)] = true
		m.Update(key("right"))
	}
	if len(seen) != len(e6.Modes()) {
		t.Fatalf("cycled through %d modes, there are %d", len(seen), len(e6.Modes()))
	}
	for _, name := range e6.Modes() {
		if _, err := e6.ParseMode(name); err != nil {
			t.Fatalf("mode %q from the knob does not parse: %v", name, err)
		}
	}
}

func TestViewCycles(t *testing.T) {
	m := testModel()
	for i := 0; i < viewCount; i++ {
		if m.view != i {
			t.Fatalf("expected view %d, got %d", i, m.view)
		}
		m.Update(key("v"))
	}
	if m.view != 0 {
		t.Fatalf("views should wrap back to the first, got %d", m.view)
	}
}

func TestFileNavigationWraps(t *testing.T) {
	m := testModel()
	m.files = []string{"a.png", "b.png", "c.png"}
	m.Update(key("n"))
	if m.idx != 1 {
		t.Fatalf("n should advance, got %d", m.idx)
	}
	m.Update(key("p"))
	m.Update(key("p"))
	if m.idx != 2 {
		t.Fatalf("p should wrap backwards to the last file, got %d", m.idx)
	}
}

func TestResetRestoresDefaults(t *testing.T) {
	m := testModel()
	focusOn(m, "chroma boost")
	m.Update(key("right"))
	m.Update(key("right"))
	m.Update(key("r"))
	if m.params.Sat != m.defaults.Sat {
		t.Fatalf("reset left the boost at %g", m.params.Sat)
	}
}

// A stale background render must not overwrite a newer one, or the preview shows
// the settings from two keystrokes ago.
func TestStaleRendersAreDropped(t *testing.T) {
	m := testModel()
	m.gen = 5
	m.report = e6.Report{MedianL: 0.5}
	m.Update(renderedMsg{gen: 4, report: e6.Report{MedianL: 0.9}})
	if m.report.MedianL != 0.5 {
		t.Fatal("an out of date render was applied")
	}
	m.Update(renderedMsg{gen: 5, report: e6.Report{MedianL: 0.9}})
	if m.report.MedianL != 0.9 {
		t.Fatal("the current render was ignored")
	}
}

func TestPromptEscapeGoesBack(t *testing.T) {
	m := testModel()
	m.Update(key("o"))
	if m.screen != screenPrompt {
		t.Fatal("o should open the prompt")
	}
	m.Update(key("esc"))
	if m.screen != screenMain {
		t.Fatal("esc should close the prompt")
	}
}

func TestPromptReportsNoMatch(t *testing.T) {
	m := testModel()
	m.Update(key("o"))
	m.prompt.SetValue("/definitely/not/here/*.png")
	m.Update(key("enter"))
	if !strings.Contains(m.status, "nothing matched") {
		t.Fatalf("expected a no match message, got %q", m.status)
	}
}

func TestParseRGB(t *testing.T) {
	got, err := parseRGB(" 12, 34 56 ")
	if err != nil || got != (e6.RGB{12, 34, 56}) {
		t.Fatalf("got %v, %v", got, err)
	}
	for _, bad := range []string{"1,2", "1,2,3,4", "1,2,banana", "1,2,300", "1,2,-5"} {
		if _, err := parseRGB(bad); err == nil {
			t.Fatalf("%q should not parse", bad)
		}
	}
}

func TestCalibrationComputesFromForm(t *testing.T) {
	m := testModel()
	m.Update(key("c"))
	if m.screen != screenCalibrate {
		t.Fatal("c should open calibration")
	}
	c := &m.cal
	c.inputs[2].SetValue("215, 217, 218")
	for i, ink := range e6.Measured {
		c.inputs[3+i].SetValue(scaled(ink, e6.RGB{215, 217, 218}))
	}
	got, err := c.compute()
	if err != nil {
		t.Fatal(err)
	}
	for i := range got {
		for k := 0; k < 3; k++ {
			if diff := int(got[i][k]) - int(e6.Measured[i][k]); diff > 2 || diff < -2 {
				t.Fatalf("%s came back as %v, wanted %v", e6.InkNames[i], got[i], e6.Measured[i])
			}
		}
	}
}

func TestCalibrationReportsBadInput(t *testing.T) {
	m := testModel()
	m.Update(key("c"))
	m.cal.inputs[2].SetValue("not numbers")
	m.cal.focus = rowCompute
	m.calAction()
	if !strings.Contains(m.cal.message, "cannot compute") {
		t.Fatalf("expected a complaint, got %q", m.cal.message)
	}
	if m.cal.result != nil {
		t.Fatal("a failed compute must not leave a result behind")
	}
}

func TestCalibrationEscapeReturns(t *testing.T) {
	m := testModel()
	m.Update(key("c"))
	m.Update(key("esc"))
	if m.screen != screenMain {
		t.Fatal("esc should leave calibration")
	}
}

func TestCalibrationFocusWraps(t *testing.T) {
	m := testModel()
	m.Update(key("c"))
	m.cal.setFocus(-1)
	if m.cal.focus != rowCount-1 {
		t.Fatalf("focus should wrap to the last row, got %d", m.cal.focus)
	}
	m.cal.setFocus(rowCount)
	if m.cal.focus != 0 {
		t.Fatalf("focus should wrap to the first row, got %d", m.cal.focus)
	}
}

func scaled(ink, paper e6.RGB) string {
	var out [3]int
	for k := 0; k < 3; k++ {
		out[k] = int(float64(ink[k]) * float64(paper[k]) / 255)
	}
	return strings.Join([]string{itoa(out[0]), itoa(out[1]), itoa(out[2])}, ", ")
}

func TestRenderImageFillsTheBox(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 40, 40))
	for y := 0; y < 40; y++ {
		for x := 0; x < 40; x++ {
			img.SetNRGBA(x, y, color.NRGBA{uint8(x * 6), uint8(y * 6), 100, 255})
		}
	}
	out := renderImage(img, 20, 10)
	lines := strings.Split(out, "\n")
	if len(lines) != 10 {
		t.Fatalf("asked for 10 rows, got %d", len(lines))
	}
	if !strings.Contains(out, "▀") {
		t.Fatal("no half blocks in the output")
	}
	if renderImage(nil, 20, 10) != "" {
		t.Fatal("a missing image should render as nothing, not panic")
	}
	if renderImage(img, 1, 1) != "" {
		t.Fatal("a box too small to draw in should render as nothing")
	}
}

func TestViewRendersWithoutAnImage(t *testing.T) {
	m := testModel()
	if out := m.View(); !strings.Contains(out, "press o to open") {
		t.Fatalf("empty state should say what to do, got %q", out)
	}
	m.Update(key("c"))
	if out := m.View(); !strings.Contains(out, "Calibrating your panel") {
		t.Fatal("calibration screen did not render its instructions")
	}
}

// The instructions are the whole point of the calibration screen, so they must
// survive a short terminal instead of scrolling off behind the form.
func TestCalibrationStepsStayVisible(t *testing.T) {
	m := testModel()
	m.Update(key("c"))
	for _, height := range []int{24, 40, 60} {
		m.h = height
		out := m.View()
		if !strings.Contains(out, "Calibrating your panel") {
			t.Fatalf("at height %d the instructions vanished", height)
		}
		if !strings.Contains(out, "paper reference") {
			t.Fatalf("at height %d the form vanished", height)
		}
		if lines := strings.Count(out, "\n") + 1; lines > height {
			t.Fatalf("at height %d the screen rendered %d lines", height, lines)
		}
	}
}

func TestCalibrationStepsScroll(t *testing.T) {
	m := testModel()
	m.h = 24
	m.Update(key("c"))
	first := m.View()
	m.Update(tea.KeyMsg{Type: tea.KeyPgDown})
	m.Update(tea.KeyMsg{Type: tea.KeyPgDown})
	if second := m.View(); second == first {
		t.Fatal("page down did not move the instructions")
	}
	for i := 0; i < 50; i++ {
		m.Update(tea.KeyMsg{Type: tea.KeyPgDown})
		m.View()
	}
	if m.cal.scroll < 0 {
		t.Fatal("scroll went negative")
	}
	for i := 0; i < 100; i++ {
		m.Update(tea.KeyMsg{Type: tea.KeyPgUp})
	}
	m.View()
	if m.cal.scroll != 0 {
		t.Fatalf("scrolling back up should stop at the top, got %d", m.cal.scroll)
	}
}

func TestInlineImageEscape(t *testing.T) {
	s := inlineImage("QUJD", 30, 0, 80, 40)
	for _, want := range []string{
		"\x1b7", "\x1b[1;31H", "width=80", "height=40", ":QUJD\a", "\x1b8",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("escape is missing %q", want)
		}
	}
}

func TestItermCapableReadsEnv(t *testing.T) {
	t.Setenv("TERM_PROGRAM", "Apple_Terminal")
	t.Setenv("LC_TERMINAL", "")
	if itermCapable() {
		t.Fatal("Apple Terminal cannot draw inline images")
	}
	t.Setenv("TERM_PROGRAM", "iTerm.app")
	if !itermCapable() {
		t.Fatal("iTerm2 should be detected")
	}
	t.Setenv("TERM_PROGRAM", "")
	t.Setenv("LC_TERMINAL", "iTerm.app")
	if !itermCapable() {
		t.Fatal("iTerm2 over ssh should be detected via LC_TERMINAL")
	}
}

// With inline images on, the preview box must stay blank half-block wise and the
// frame must carry exactly one OSC 1337 sequence. The calibration screen must
// carry none, or a stale image would float over the form.
func TestViewEmbedsInlineImageWhenCapable(t *testing.T) {
	m := testModel()
	m.iterm = true
	m.previewB64 = "QUJD"
	m.preview = image.NewNRGBA(image.Rect(0, 0, 8, 8))
	out := m.View()
	if got := strings.Count(out, "1337;File=inline=1"); got != 1 {
		t.Fatalf("wanted exactly one inline image, got %d", got)
	}
	if strings.Contains(out, "▀") {
		t.Fatal("half blocks should not be drawn under an inline image")
	}
	first := m.View()
	if second := m.View(); second == first {
		t.Fatal("the frame nonce must change every render or the image is never repainted")
	}
	m.Update(key("c"))
	if strings.Contains(m.View(), "1337") {
		t.Fatal("the calibration screen must not draw the preview image")
	}
}

func TestPromptTabCompletes(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"photo1.jpg", "photo2.jpg"} {
		if err := os.WriteFile(filepath.Join(dir, name), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	m := testModel()
	m.screen = screenPrompt
	m.prompt.SetValue(filepath.Join(dir, "pho"))
	m.updatePrompt(key("tab"))
	if got, want := m.prompt.Value(), filepath.Join(dir, "photo"); got != want {
		t.Fatalf("shared prefix: got %q, want %q", got, want)
	}

	m.prompt.SetValue(filepath.Join(dir, "photo1"))
	m.updatePrompt(key("tab"))
	if got, want := m.prompt.Value(), filepath.Join(dir, "photo1.jpg"); got != want {
		t.Fatalf("single match: got %q, want %q", got, want)
	}

	m.prompt.SetValue(filepath.Join(dir, "nope"))
	m.updatePrompt(key("tab"))
	if got := m.prompt.Value(); got != filepath.Join(dir, "nope") {
		t.Fatalf("no match should leave the value alone, got %q", got)
	}
}
