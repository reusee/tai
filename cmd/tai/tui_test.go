package main

import (
	"context"
	"fmt"
	"io"
	"iter"
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v3/color"
	"github.com/reusee/tai/flags"
	"github.com/reusee/tai/generators"
	"github.com/reusee/tai/pipeline"
	"github.com/reusee/tai/taiui"
)

func newTUIForTest() *TUI {
	tabs := taiui.NewTabs(3)
	// Tests exercise the production layout, including the Logs tab's
	// unfocused height cap. See logsMaxBoxHeight.
	tabs.MaxSizes = []int{0, 0, logsMaxBoxHeight}
	return &TUI{
		output:  taiui.NewLineBuffer(0),
		logs:    taiui.NewStringBuffer(0),
		tabs:    tabs,
		scrolls: [3]taiui.ScrollState{},
		// The default display policy shows raw thoughts, matching the
		// non-TUI default; tests that exercise thought suppression set
		// showThoughts to false explicitly. See TheoryOfTUI.
		showThoughts: true,
	}
}

func TestTUILogsBoxCappedWhenUnfocused(t *testing.T) {
	tui := newTUIForTest()
	// All tabs expanded, Output focused: Logs is capped to
	// logsMaxBoxHeight rows and the freed rows go to the other tabs by
	// weight.
	tui.tabs.Expanded = []bool{true, true, true}
	tui.tabs.Focus = 0
	boxes := tui.tabs.Boxes(80, 40)
	if boxes[2].Height() != logsMaxBoxHeight {
		t.Fatalf("unfocused Logs box must be capped to %d rows, got %+v", logsMaxBoxHeight, boxes[2])
	}
	if boxes[0].Height()+boxes[1].Height() != 40-logsMaxBoxHeight {
		t.Fatalf("freed rows must go to the other tabs: %+v", boxes)
	}

	// Focusing Logs lifts the cap and restores the usual 1:1:3 ratio.
	tui.tabs.Focus = 2
	boxes = tui.tabs.Boxes(80, 40)
	if boxes[2].Height() != 24 {
		t.Fatalf("focused Logs must keep the usual ratio, got %+v", boxes[2])
	}
}

// writeModelOutput feeds a text chunk through captureContent as model
// output — the TUI's model-output display path. The Events tab renders
// pipeline events only (handleEvent); captureContent performs no block
// extraction.
func writeModelOutput(tui *TUI, text string) {
	tui.captureContent(&generators.Content{
		Role:  generators.RoleModel,
		Parts: []generators.Part{generators.Text(text)},
	})
}

func plainLines(lines []string) []taiui.Line {
	out := make([]taiui.Line, 0, len(lines))
	for _, line := range lines {
		out = append(out, taiui.Line{Text: line})
	}
	return out
}

func TestTuiFlagHandle(t *testing.T) {
	f := Tui(false)
	newDef, remainArgs, err := f.Handle("-tui", []string{"chat", "hello"})
	if err != nil {
		t.Fatal(err)
	}
	if len(remainArgs) != 2 || remainArgs[0] != "chat" || remainArgs[1] != "hello" {
		t.Fatalf("unexpected remain: %v", remainArgs)
	}
	ret, ok := newDef.(*Tui)
	if !ok {
		t.Fatalf("expected *Tui, got %T", newDef)
	}
	if !bool(*ret) {
		t.Fatal("expected Tui(true)")
	}
}

func TestTuiCliFlagHandle(t *testing.T) {
	f := Tui(true)
	newDef, remainArgs, err := f.Handle("-cli", []string{"chat", "hello"})
	if err != nil {
		t.Fatal(err)
	}
	if len(remainArgs) != 2 || remainArgs[0] != "chat" || remainArgs[1] != "hello" {
		t.Fatalf("unexpected remain: %v", remainArgs)
	}
	ret, ok := newDef.(*Tui)
	if !ok {
		t.Fatalf("expected *Tui, got %T", newDef)
	}
	if bool(*ret) {
		t.Fatal("expected Tui(false)")
	}
}

func TestTuiDefaultEnabled(t *testing.T) {
	var m Module
	if !bool(m.Tui()) {
		t.Fatal("expected TUI mode by default")
	}
}

func TestTuiStateWriteLines(t *testing.T) {
	tui := newTUIForTest()
	tui.write([]byte("hello\nworld\n"))
	lines := tui.output.Lines()
	if len(lines) != 2 || lines[0].Text != "hello" || lines[1].Text != "world" {
		t.Fatalf("unexpected lines: %v", lines)
	}
}

func TestTuiOutputHasNoLineLimit(t *testing.T) {
	// The Output tab's line buffer is wired without a line limit (see
	// newTUI): a bounded buffer would drop the oldest lines past its
	// ceiling, so a view scrolled back to earlier output would shift by
	// one row on every newly appended line — experienced as continuous
	// scrolling. The former ceiling was 10000 lines; the unbounded
	// buffer must retain every line far past it, oldest first.
	const lines = 20000 // past the former 10000-line ceiling
	buf := taiui.NewLineBuffer(0)
	for i := 0; i < lines; i++ {
		buf.Append(taiui.NoColor, fmt.Sprintf("line %d\n", i))
	}
	got := buf.Lines()
	if len(got) != lines {
		t.Fatalf("expected %d lines retained, got %d", lines, len(got))
	}
	if got[0].Text != "line 0" {
		t.Fatalf("expected the very first line retained, got %q", got[0].Text)
	}
}

func TestTuiLogsHasNoLineLimit(t *testing.T) {
	// The Logs tab's buffer is wired without a line limit (see newTUI):
	// a bounded buffer would silently drop the oldest log records past
	// its ceiling, making the session's log record incomplete. The
	// former ceiling was 10000 lines; the unbounded buffer must retain
	// every record far past it, oldest first. See
	// TheoryOfTUINoTruncation.
	const lines = 20000 // past the former 10000-line ceiling
	buf := taiui.NewStringBuffer(0)
	for i := 0; i < lines; i++ {
		buf.Append([]byte(fmt.Sprintf("log %d\n", i)))
	}
	got := buf.Lines()
	if len(got) != lines {
		t.Fatalf("expected %d records retained, got %d", lines, len(got))
	}
	if got[0] != "log 0" {
		t.Fatalf("expected the very first record retained, got %q", got[0])
	}
}

func TestTuiSignalsHasNoLimit(t *testing.T) {
	const lines = 5000
	tui := newTUIForTest()
	var body strings.Builder
	for i := 0; i < lines; i++ {
		if i > 0 {
			body.WriteString("\n")
		}
		fmt.Fprintf(&body, "- line %d", i)
	}
	tui.handleEvent(pipeline.Event{Kind: pipeline.EventAttemptCompleted, Attempt: 1, Summary: body.String()})
	// The event group carries the emoji header line plus one line per
	// summary line.
	if len(tui.events) != 1 || len(tui.events[0]) != lines+1 {
		t.Fatalf("expected 1 event group of %d lines plus the header, got %d groups", lines, len(tui.events))
	}
	if tui.events[0][0].Text != "✅ [Attempt 1 complete]" {
		t.Fatalf("expected the event header first, got %q", tui.events[0][0].Text)
	}
	if tui.events[0][1].Text != "- line 0" {
		t.Fatalf("expected the very first event line retained, got %q", tui.events[0][1].Text)
	}
	if tui.events[0][lines].Text != fmt.Sprintf("- line %d", lines-1) {
		t.Fatalf("expected the last event line retained, got %q", tui.events[0][lines].Text)
	}

	tui2 := newTUIForTest()
	const finishes = 3000
	for i := 0; i < finishes; i++ {
		tui2.handleEvent(pipeline.Event{Kind: pipeline.EventFinish, Detail: "stop"})
	}
	if len(tui2.events) != finishes {
		t.Fatalf("expected %d finish groups, got %d", finishes, len(tui2.events))
	}
}

func TestDisplayChatInput(t *testing.T) {
	tui := newTUIForTest()
	displayChatInput(tui, flags.Chats{"hello", "world"})
	tui.mu.Lock()
	defer tui.mu.Unlock()
	lines := tui.output.Lines()
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d: %v", len(lines), lines)
	}
	if lines[0].Text != "hello" || lines[0].Color != outputColorUserLine {
		t.Fatalf("unexpected first line: %+v", lines[0])
	}
	if lines[1].Text != "world" || lines[1].Color != outputColorUserLine {
		t.Fatalf("unexpected second line: %+v", lines[1])
	}
	if !tui.tabs.Expanded[0] {
		t.Fatal("output tab should auto-expand on chat input")
	}
	if tui.tabs.Focus != 0 {
		t.Fatalf("expected focus on the output tab, got %d", tui.tabs.Focus)
	}
	if !tui.scrolls[0].Follow {
		t.Fatal("output tab should follow the tail")
	}
}

func TestDisplayChatInputEmpty(t *testing.T) {
	tui := newTUIForTest()
	displayChatInput(tui, nil)
	tui.mu.Lock()
	defer tui.mu.Unlock()
	if len(tui.output.Lines()) != 0 {
		t.Fatalf("expected no lines for empty chats, got %v", tui.output.Lines())
	}
	if tui.tabs.Expanded[0] {
		t.Fatal("output tab must not expand for empty chats")
	}
}

func TestDisplayChatInputSetsOutputRole(t *testing.T) {
	tui := newTUIForTest()
	displayChatInput(tui, flags.Chats{"prompt text"})
	if !tui.hasOutput {
		t.Fatal("expected hasOutput to be true")
	}
	if tui.lastOutputRole != generators.RoleUser {
		t.Fatalf("expected lastOutputRole to be RoleUser, got %v", tui.lastOutputRole)
	}
	tui.captureContent(&generators.Content{
		Role:  generators.RoleModel,
		Parts: []generators.Part{generators.Text("response text\n")},
	})
	lines := tui.output.Lines()
	if len(lines) != 3 || lines[0].Text != "prompt text" || lines[1].Text != "" || lines[2].Text != "response text" {
		t.Fatalf("expected proper separation between user and model output, got %v", lines)
	}
}

func TestTuiStateWriteLogs(t *testing.T) {
	tui := newTUIForTest()
	tui.writeLogs([]byte("hello\nworld\n"))
	logs := tui.logs.Lines()
	if len(logs) != 2 || logs[0] != "hello" || logs[1] != "world" {
		t.Fatalf("unexpected logs: %v", logs)
	}
}

func TestTuiStateRequesting(t *testing.T) {
	tui := newTUIForTest()
	if label, highlight := outputTabLabel(tui.finished, tui.generating, tui.handoff); label != "Output" || highlight {
		t.Fatalf("expected plain Output label before any activity, got label %q highlight %v", label, highlight)
	}
	tui.writeLogs([]byte("level=INFO msg=generating name=model\n"))
	if !tui.generating {
		t.Fatal("expected generating after the generating log")
	}
	if label, highlight := outputTabLabel(tui.finished, tui.generating, tui.handoff); label != "Output (generating...)" || !highlight {
		t.Fatalf("expected generating hint with highlight, got label %q highlight %v", label, highlight)
	}
	tui.handleEvent(pipeline.Event{Kind: pipeline.EventFinish, Detail: "stop"})
	if tui.generating {
		t.Fatal("expected not generating after the finish event")
	}
	if label, highlight := outputTabLabel(tui.finished, tui.generating, tui.handoff); label != "Output" || highlight {
		t.Fatalf("expected plain Output label after the finish event, got label %q highlight %v", label, highlight)
	}
	tui.finished = true
	if label, highlight := outputTabLabel(tui.finished, tui.generating, tui.handoff); label != "Output (done)" || highlight {
		t.Fatalf("expected done hint without highlight, got label %q highlight %v", label, highlight)
	}
}

func TestTuiStateRequestingLogsWrite(t *testing.T) {
	tui := newTUIForTest()
	tui.writeLogs([]byte("msg=\"generating\"\n"))
	if !tui.generating {
		t.Fatal("expected generating after log write")
	}
	if label, highlight := outputTabLabel(tui.finished, tui.generating, tui.handoff); label != "Output (generating...)" || !highlight {
		t.Fatalf("expected generating hint with highlight, got label %q highlight %v", label, highlight)
	}
}

func TestIsGeneratingLog(t *testing.T) {
	tests := []struct {
		line string
		want bool
	}{
		{`time=2026-01-01T00:00:00+08:00 level=INFO msg=generating name=gemini model=foo`, true},
		{`level=INFO msg="generating" name=model`, true},
		{`level=INFO msg=generating`, true},
		{`level=INFO msg=generatingX name=model`, false},
		{`level=INFO msg="generating failed" name=model`, false},
		{`level=INFO msg=applying changes`, false},
	}
	for _, tt := range tests {
		if got := isGeneratingLog(tt.line); got != tt.want {
			t.Fatalf("isGeneratingLog(%q) = %v, want %v", tt.line, got, tt.want)
		}
	}
}

func TestTuiStateRequestingClearedByFinish(t *testing.T) {
	tui := newTUIForTest()
	tui.writeLogs([]byte("level=INFO msg=generating name=model\n"))
	if !tui.generating {
		t.Fatal("expected generating after the generating log")
	}
	tui.handleEvent(pipeline.Event{Kind: pipeline.EventFinish, Detail: "stop"})
	if tui.generating {
		t.Fatal("expected not generating after the finish event")
	}
	if label, _ := outputTabLabel(tui.finished, tui.generating, tui.handoff); label != "Output" {
		t.Fatalf("expected plain Output label after the finish event, got %q", label)
	}
}

func TestTUIOutputTabLabel(t *testing.T) {
	tui := newTUIForTest()
	if label, highlight := outputTabLabel(tui.finished, tui.generating, tui.handoff); label != "Output" || highlight {
		t.Fatalf("expected plain Output label, got label %q highlight %v", label, highlight)
	}
	tui.writeLogs([]byte("level=INFO msg=generating name=model\n"))
	if label, highlight := outputTabLabel(tui.finished, tui.generating, tui.handoff); label != "Output (generating...)" || !highlight {
		t.Fatalf("expected generating hint with highlight, got label %q highlight %v", label, highlight)
	}
	tui.finished = true
	if label, highlight := outputTabLabel(tui.finished, tui.generating, tui.handoff); label != "Output (done)" || highlight {
		t.Fatalf("expected done hint without highlight, got label %q highlight %v", label, highlight)
	}
}

func TestTUIPanelTitleHighlightedDuringRequest(t *testing.T) {
	renderTitle := func(tui *TUI, focus bool) taiui.Frame {
		label, highlight := outputTabLabel(tui.finished, tui.generating, tui.handoff)
		element := taiui.Panel(
			taiui.Box{Top: 0, Left: 0, Bottom: 2, Right: 12},
			label, highlight,
			[]taiui.Line{{Text: "content"}},
			0, focus, true, panelStyle,
		)
		screen := &panelTestScreen{width: 12, height: 2}
		taiui.Render(element, screen)
		if len(screen.frames) == 0 {
			t.Fatal("expected a rendered frame")
		}
		return screen.frames[len(screen.frames)-1]
	}

	tui := newTUIForTest()
	tui.writeLogs([]byte("level=INFO msg=generating name=model\n"))
	frame := renderTitle(tui, false)
	cell := frame.Cells[2]
	if cell.Rune != 'O' {
		t.Fatalf("expected title 'O' at (2,0), got %v", cell.Rune)
	}
	wantR, wantG, wantB := color.PaletteColor(int(tabActiveLabelFg)).RGB()
	if r, g, b := cell.Style.Fg().RGB(); r != wantR || g != wantG || b != wantB {
		t.Fatalf("expected highlighted title foreground %#x %#x %#x, got %#x %#x %#x", wantR, wantG, wantB, r, g, b)
	}

	idle := newTUIForTest()
	idleFrame := renderTitle(idle, false)
	idleCell := idleFrame.Cells[2]
	if idleCell.Rune != 'O' {
		t.Fatalf("expected title 'O' at (2,0), got %v", idleCell.Rune)
	}
	if r, g, b := idleCell.Style.Fg().RGB(); r == wantR && g == wantG && b == wantB {
		t.Fatal("expected the idle title to keep the ordinary foreground color")
	}
}

func TestTUIHandoffState(t *testing.T) {
	tui := newTUIForTest()
	if label, highlight := outputTabLabel(tui.finished, tui.generating, tui.handoff); label != "Output" || highlight {
		t.Fatalf("expected plain Output label, got label %q highlight %v", label, highlight)
	}
	tui.handoff = true
	if label, highlight := outputTabLabel(tui.finished, tui.generating, tui.handoff); label != "Output (handoff...)" || !highlight {
		t.Fatalf("expected handoff label with highlight, got label %q highlight %v", label, highlight)
	}
	// Handoff takes precedence over generating.
	tui.generating = true
	if label, highlight := outputTabLabel(tui.finished, tui.generating, tui.handoff); label != "Output (handoff...)" || !highlight {
		t.Fatalf("expected handoff label to take precedence over generating, got label %q highlight %v", label, highlight)
	}
	tui.handoff = false
	if label, highlight := outputTabLabel(tui.finished, tui.generating, tui.handoff); label != "Output (generating...)" || !highlight {
		t.Fatalf("expected generating label after handoff cleared, got label %q highlight %v", label, highlight)
	}
	tui.generating = false
	if label, highlight := outputTabLabel(tui.finished, tui.generating, tui.handoff); label != "Output" || highlight {
		t.Fatalf("expected plain Output label after handoff, got label %q highlight %v", label, highlight)
	}
}

func TestTUIHandoffLifecycle(t *testing.T) {
	tui := newTUIForTest()
	tui.HandoffStart()
	if !tui.handoff {
		t.Fatal("expected handoff flag set")
	}
	tui.HandoffEnd()
	if tui.handoff {
		t.Fatal("expected handoff flag cleared")
	}
}

func TestTuiStateLogsPartialLines(t *testing.T) {
	tui := newTUIForTest()
	tui.writeLogs([]byte("foo"))
	tui.writeLogs([]byte("bar\n"))
	logs := tui.logs.Lines()
	if len(logs) != 1 || logs[0] != "foobar" {
		t.Fatalf("unexpected logs: %v", logs)
	}
	if tui.logs.HasPartial() {
		t.Fatalf("unexpected partial: %q", tui.logs.Lines())
	}
	tui.writeLogs([]byte("baz"))
	if !tui.logs.HasPartial() {
		t.Fatal("expected partial line")
	}
	lines := tui.logs.Lines()
	if len(lines) != 2 || lines[1] != "baz" {
		t.Fatalf("unexpected rendered log lines: %v", lines)
	}
}

func TestTUICaptureContentShowsThoughtsByDefault(t *testing.T) {
	// The default display policy shows raw thoughts, matching the
	// non-TUI default. newTUIForTest sets showThoughts to true.
	tui := newTUIForTest()
	state := generators.NewPrompts("", nil)
	s := tuiOutputState{upstream: state, tui: tui}
	if _, err := s.AppendContent(&generators.Content{
		Role:  generators.RoleModel,
		Parts: []generators.Part{generators.Thought("deep thinking\n")},
	}); err != nil {
		t.Fatal(err)
	}
	tui.mu.Lock()
	defer tui.mu.Unlock()
	lines := tui.output.Lines()
	if len(lines) != 1 || lines[0].Text != "deep thinking" {
		t.Fatalf("expected the thought line, got %v", lines)
	}
}

func TestTuiLogsWriterWritesToLogs(t *testing.T) {
	tui := newTUIForTest()
	writer := logsWriter{t: tui}
	if _, err := writer.Write([]byte("hello\n")); err != nil {
		t.Fatal(err)
	}
	tui.mu.Lock()
	defer tui.mu.Unlock()
	logs := tui.logs.Lines()
	if len(logs) != 1 || logs[0] != "hello" {
		t.Fatalf("unexpected logs: %v", logs)
	}
}

func TestPlainOutputLinesAlternatesBackgrounds(t *testing.T) {
	base := taiui.HexColor(tabUnfocusBG)
	lines := taiui.PlainLines([]string{"a", "b", "c"}, base)
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d", len(lines))
	}
	for i, line := range lines {
		want := base
		if i%2 == 1 {
			want = taiui.AltBG(base)
		}
		if line.BGColor != want {
			t.Fatalf("line %d: expected background %#x, got %#x", i, want, line.BGColor)
		}
		if line.Color != taiui.NoColor {
			t.Fatalf("line %d: expected no foreground color, got %#x", i, line.Color)
		}
	}
	if taiui.AltBG(base) == base {
		t.Fatal("alternate background must differ from the base")
	}
}

func TestLogAltBG(t *testing.T) {
	for _, base := range []taiui.Color{taiui.HexColor(tabUnfocusBG), taiui.HexColor(tabFocusBG)} {
		r1, g1, b1 := base.RGB()
		r2, g2, b2 := taiui.AltBG(base).RGB()
		if !(r2 > r1 && g2 > g1 && b2 > b1) {
			t.Fatalf("expected alternate lighter than base %#x, got %#x %#x %#x -> %#x %#x %#x",
				base, r1, g1, b1, r2, g2, b2)
		}
	}
}

func TestTUIEventsAlternateBackgrounds(t *testing.T) {
	tui := newTUIForTest()
	tui.handleEvent(pipeline.Event{Kind: pipeline.EventFinish, Detail: "stop"})
	usage := generators.Usage{}
	usage.Prompt.TokenCount = 1
	tui.handleEvent(pipeline.Event{Kind: pipeline.EventUsage, Attempt: 1, Usage: usage})

	box := taiui.Box{Top: 0, Left: 0, Bottom: 10, Right: 80}
	base := panelStyle.BaseBG
	alt := taiui.AltBG(base)

	// Each event renders one text line, so the two events produce two
	// display lines: base, alt.
	tui.tabs.Focus = -1
	display := wrappedDisplay(tui, 1, box)
	if len(display) != 2 {
		t.Fatalf("expected 2 display lines, got %d", len(display))
	}
	for i, want := range []taiui.Color{base, alt} {
		if display[i].BGColor != want {
			t.Fatalf("line %d: expected shade %#v, got %#v", i, want, display[i].BGColor)
		}
	}

	tui.tabs.Focus = 1
	base = panelStyle.FocusBG
	alt = taiui.AltBG(base)
	display = wrappedDisplay(tui, 1, box)
	for i, want := range []taiui.Color{base, alt} {
		if display[i].BGColor != want {
			t.Fatalf("focused line %d: expected shade %#v, got %#v", i, want, display[i].BGColor)
		}
	}
}

func TestColoredTextAlternatingBackgrounds(t *testing.T) {
	alt := taiui.AltBG(taiui.HexColor(tabUnfocusBG))
	lines := []taiui.Line{
		{Text: "first", BGColor: taiui.HexColor(tabUnfocusBG)},
		{Text: "second", BGColor: alt},
	}
	element := taiui.LinesElement(lines, taiui.Box{Top: 0, Left: 0, Bottom: 2, Right: 10})
	screen := &panelTestScreen{width: 10, height: 2}
	taiui.Render(element, screen)
	if len(screen.frames) == 0 {
		t.Fatal("expected a rendered frame")
	}
	frame := screen.frames[len(screen.frames)-1]

	wantR, wantG, wantB := taiui.HexColor(tabUnfocusBG).RGB()
	cell := frame.Cells[9]
	if !cell.Set {
		t.Fatal("expected the first row painted with its background")
	}
	if r, g, b := cell.Style.Bg().RGB(); r != wantR || g != wantG || b != wantB {
		t.Fatalf("expected base background %#x, got %#x %#x %#x", tabUnfocusBG, r, g, b)
	}

	wantR, wantG, wantB = alt.RGB()
	cell = frame.Cells[19]
	if !cell.Set {
		t.Fatal("expected the second row painted with its background")
	}
	if r, g, b := cell.Style.Bg().RGB(); r != wantR || g != wantG || b != wantB {
		t.Fatalf("expected alternate background %#x, got %#x %#x %#x", alt, r, g, b)
	}
}

func TestTuiStatePartialLines(t *testing.T) {
	tui := newTUIForTest()
	tui.write([]byte("foo"))
	tui.write([]byte("bar\n"))
	lines := tui.output.Lines()
	if len(lines) != 1 || lines[0].Text != "foobar" {
		t.Fatalf("unexpected lines: %v", lines)
	}
	if tui.output.HasPartial() {
		t.Fatalf("unexpected partial: %q", tui.output.Lines())
	}
	tui.write([]byte("baz"))
	if !tui.output.HasPartial() {
		t.Fatal("expected partial line")
	}
	lines = tui.output.Lines()
	if len(lines) != 2 || lines[1].Text != "baz" {
		t.Fatalf("unexpected rendered lines: %v", lines)
	}
}

func TestTuiOutputPreservesIndentation(t *testing.T) {
	tui := newTUIForTest()
	tui.write([]byte("    func main() {\n        fmt.Println(1)\n    }\n"))
	lines := tui.output.Lines()
	wrapped := taiui.WrapLinesColored(lines, 80)
	want := []string{
		"    func main() {",
		"        fmt.Println(1)",
		"    }",
	}
	if len(wrapped) != len(want) {
		t.Fatalf("expected %d lines, got %d: %q", len(want), len(wrapped), wrapped)
	}
	for i := range want {
		if wrapped[i].Text != want[i] {
			t.Fatalf("line %d: got %q, want %q", i, wrapped[i].Text, want[i])
		}
	}
}

func TestTUICaptureContentSuppressesThoughtsWhenNotShown(t *testing.T) {
	// The TUI must mirror the non-TUI Output layer's display policy:
	// when -no-thoughts is set or -summarize-thoughts is enabled
	// (showThoughts false), raw reasoning thoughts must not appear in
	// the Output tab; they are replaced by periodic summaries that
	// render in the Events tab from EventThoughtSummary. See TheoryOfTUI.
	tui := newTUIForTest()
	tui.showThoughts = false
	state := generators.NewPrompts("", nil)
	s := tuiOutputState{upstream: state, tui: tui}

	if _, err := s.AppendContent(&generators.Content{
		Role:  generators.RoleModel,
		Parts: []generators.Part{generators.Thought("deep thinking\n")},
	}); err != nil {
		t.Fatal(err)
	}
	// Non-thought content still streams to the Output tab.
	if _, err := s.AppendContent(&generators.Content{
		Role:  generators.RoleModel,
		Parts: []generators.Part{generators.Text("the answer\n")},
	}); err != nil {
		t.Fatal(err)
	}

	tui.mu.Lock()
	defer tui.mu.Unlock()
	lines := tui.output.Lines()
	if len(lines) != 1 || lines[0].Text != "the answer" {
		t.Fatalf("expected only the answer line, got %v", lines)
	}
}

func TestTuiShowThoughtsNotSuppressedBySummarizeThoughts(t *testing.T) {
	// The TUI's raw-thought display is governed by -no-thoughts alone:
	// -summarize-thoughts adds periodic summaries in the Events tab but
	// never blanks the Output tab's raw thought stream. The helper takes
	// only flags.Thoughts, so -st cannot affect it. See TheoryOfTUI.
	if !tuiShowThoughts(flags.Thoughts{}) {
		t.Fatal("raw thoughts must display by default")
	}
	shown := true
	if !tuiShowThoughts(flags.Thoughts{Value: &shown}) {
		t.Fatal("raw thoughts must display when -thoughts is set")
	}
	hidden := false
	if tuiShowThoughts(flags.Thoughts{Value: &hidden}) {
		t.Fatal("raw thoughts must be suppressed when -no-thoughts is set")
	}
}

func TestTuiStateSummaryTabTitle(t *testing.T) {
	if tabNames[1] != "Events" {
		t.Fatalf("expected the events tab title, got %q", tabNames[1])
	}
}

func TestTUIPanelShowsTailOfWrappedContent(t *testing.T) {
	var src []string
	for i := 0; i < 20; i++ {
		src = append(src, strings.Repeat("x", 20))
	}
	src = append(src, "THE-END")
	display := plainLines(taiui.WrapLines(src, 9))
	last := display[len(display)-1].Text
	if last != "THE-END" {
		t.Fatalf("expected the last display line to be THE-END, got %q", last)
	}

	paneHeight := 9
	element := taiui.Panel(
		taiui.Box{Top: 0, Left: 0, Bottom: 10, Right: 12},
		"Output", false, display,
		taiui.ClampOffset(1<<30, len(display), paneHeight),
		false, true, panelStyle,
	)

	screen := &panelTestScreen{width: 12, height: 10}
	taiui.Render(element, screen)

	if len(screen.frames) == 0 {
		t.Fatal("expected a rendered frame")
	}
	frame := screen.frames[len(screen.frames)-1]
	if cell := frame.Cells[9*frame.Width+0]; cell.Rune != 'T' {
		t.Fatalf("expected THE-END at the pane's bottom row (9,0), got %v", cell.Rune)
	}
}

func TestTUIPanelBackgroundColors(t *testing.T) {
	renderPanel := func(focus bool) taiui.Frame {
		element := taiui.Panel(
			taiui.Box{Top: 0, Left: 0, Bottom: 4, Right: 12},
			"Output", false,
			[]taiui.Line{{Text: "content"}},
			0, focus, true, panelStyle,
		)
		screen := &panelTestScreen{width: 12, height: 4}
		taiui.Render(element, screen)
		if len(screen.frames) == 0 {
			t.Fatal("expected a rendered frame")
		}
		return screen.frames[len(screen.frames)-1]
	}

	cases := []struct {
		focus bool
		want  [3]int32
	}{
		{false, [3]int32{0x0a, 0x14, 0x28}},
		{true, [3]int32{0x2e, 0x2e, 0x2e}},
	}
	for _, tc := range cases {
		frame := renderPanel(tc.focus)
		for _, y := range []int{0, 1} {
			cell := frame.Cells[y*frame.Width+0]
			if !cell.Set {
				t.Fatalf("focus=%v: expected row %d to be painted", tc.focus, y)
			}
			r, g, b := cell.Style.Bg().RGB()
			if r != tc.want[0] || g != tc.want[1] || b != tc.want[2] {
				t.Fatalf("focus=%v row %d: expected background %#x %#x %#x, got %#x %#x %#x",
					tc.focus, y, tc.want[0], tc.want[1], tc.want[2], r, g, b)
			}
		}
	}
}

func TestReadTUIKeys(t *testing.T) {
	ch := make(chan string, 10)
	go taiui.ReadKeys(strings.NewReader("\x1b[Aq\x1b[5~\x1b[6~"), ch)
	var got []string
	for len(got) < 4 {
		select {
		case k := <-ch:
			got = append(got, mapTUIKey(k))
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for keys")
		}
	}
	want := []string{"up", "quit", "pageup", "pagedown"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("key %d: expected %q, got %q", i, want[i], got[i])
		}
	}
}

func TestReadTUIMouseKeys(t *testing.T) {
	// SGR mouse sequences: ESC [ < Cb ; Cx ; Cy M for press, drag, and
	// wheel events, m for releases. Wire coordinates are 1-based and
	// emitted 0-based. See taiui.TheoryOfMouseInput.
	pr, pw := io.Pipe()
	ch := make(chan string, 10)
	go taiui.ReadKeys(pr, ch)
	go func() {
		pw.Write([]byte("\x1b[<0;11;6M")) // left press at (10,5)
		pw.Write([]byte("\x1b[<3;11;6m")) // release at (10,5)
		pw.Write([]byte("\x1b[<64;8;9M")) // wheel up at (7,8)
		pw.Write([]byte("\x1b[<65;8;9M")) // wheel down at (7,8)
		pw.Write([]byte("\x1b[<32;5;5M")) // left drag at (4,4)
		pw.Write([]byte("\x1b[<35;5;5M")) // no-button motion: ignored
		pw.Close()
	}()
	var got []string
	for len(got) < 5 {
		select {
		case k := <-ch:
			got = append(got, k)
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for mouse keys")
		}
	}
	want := []string{
		"mouse-left@10,5",
		"mouse-release@10,5",
		"mouse-wheel-up@7,8",
		"mouse-wheel-down@7,8",
		"mouse-leftdrag@4,4",
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("key %d: expected %q, got %q", i, want[i], got[i])
		}
	}
	// The no-button motion event must be ignored.
	select {
	case k := <-ch:
		t.Fatalf("unexpected key for ignored motion: %q", k)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestReadTUIKeysTabAndSplit(t *testing.T) {
	ch := make(chan string, 10)
	go taiui.ReadKeys(strings.NewReader("123sS"), ch)
	var got []string
	for len(got) < 5 {
		select {
		case k := <-ch:
			got = append(got, mapTUIKey(k))
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for keys")
		}
	}
	want := []string{"1", "2", "3", "split", "split"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("key %d: expected %q, got %q", i, want[i], got[i])
		}
	}
}

func TestReadTUIMouseKeysStreamed(t *testing.T) {
	// A mouse sequence may arrive split across reads; the parser must
	// wait for the terminator before emitting. See
	// taiui.TheoryOfMouseInput.
	pr, pw := io.Pipe()
	ch := make(chan string, 10)
	go taiui.ReadKeys(pr, ch)
	go func() {
		pw.Write([]byte("\x1b[<0;1"))
		time.Sleep(10 * time.Millisecond)
		pw.Write([]byte("1;6M"))
		pw.Close()
	}()
	select {
	case k := <-ch:
		if k != "mouse-left@10,5" {
			t.Fatalf("unexpected key: %q", k)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for the streamed mouse key")
	}
}

func TestReadTUIKeysTransitions(t *testing.T) {
	ch := make(chan string, 10)
	go taiui.ReadKeys(strings.NewReader("[]"), ch)
	var got []string
	for len(got) < 2 {
		select {
		case k := <-ch:
			got = append(got, mapTUIKey(k))
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for keys")
		}
	}
	want := []string{"prev-transition", "next-transition"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("key %d: expected %q, got %q", i, want[i], got[i])
		}
	}
}

func TestTUINumberKeySemantics(t *testing.T) {
	tui := newTUIForTest()
	tui.tabs.AutoExpand(0)
	tui.tabs.AutoExpand(1)
	if tui.tabs.Focus != 0 {
		t.Fatalf("expected focus on tab 0, got %d", tui.tabs.Focus)
	}

	tui.toggleTab(0)
	if tui.tabs.Expanded[0] {
		t.Fatal("focused output tab should be collapsed")
	}
	if tui.tabs.Focus != 1 {
		t.Fatalf("focus should move to the last-focused expanded tab, got %d", tui.tabs.Focus)
	}

	tui.toggleTab(2)
	if !tui.tabs.Expanded[2] {
		t.Fatal("collapsed logs tab should expand")
	}
	if tui.tabs.Focus != 2 {
		t.Fatalf("focus should switch to the logs tab, got %d", tui.tabs.Focus)
	}

	tui.toggleTab(1)
	if !tui.tabs.Expanded[1] {
		t.Fatal("expanded summary tab must stay expanded")
	}
	if tui.tabs.Focus != 1 {
		t.Fatalf("focus should switch to the summary tab, got %d", tui.tabs.Focus)
	}

	tui.toggleTab(1)
	if tui.tabs.Expanded[1] {
		t.Fatal("focused summary tab should collapse")
	}
	if tui.tabs.Focus != 2 {
		t.Fatalf("focus should move to the last-focused expanded tab, got %d", tui.tabs.Focus)
	}

	tui.toggleTab(2)
	if tui.tabs.Expanded[2] {
		t.Fatal("focused logs tab should collapse")
	}
	if tui.tabs.Focus != -1 {
		t.Fatalf("focus should be -1 when no tab is expanded, got %d", tui.tabs.Focus)
	}
}

func TestTUIMousePress(t *testing.T) {
	// The subtests drive the public behavior through handleMouseKey, the
	// same path the session's key loop takes.
	t.Run("CollapsedStripExpandsAndFocuses", func(t *testing.T) {
		tui := newTUIForTest()
		tui.width, tui.height = 80, 45
		tui.tabs.Expanded = []bool{true, false, false}
		tui.tabs.HasContent = []bool{true, false, false}
		tui.tabs.Focus = 0
		// Horizontal split: the output tab occupies rows 0..42, the
		// collapsed summary tab row 43, the collapsed logs tab row 44.
		tui.handleMouseKey("mouse-left@5,43")
		if !tui.tabs.Expanded[1] {
			t.Fatal("pressing a collapsed tab's strip must expand it")
		}
		if tui.tabs.Focus != 1 {
			t.Fatalf("expected the focus on the pressed tab, got %d", tui.tabs.Focus)
		}
		if !tui.scrolls[1].Follow {
			t.Fatal("expanding must resume following the tail")
		}
	})

	t.Run("FocusedStripCollapses", func(t *testing.T) {
		tui := newTUIForTest()
		tui.width, tui.height = 80, 45
		tui.tabs.Expanded = []bool{true, false, false}
		tui.tabs.HasContent = []bool{true, false, false}
		tui.tabs.Focus = 0
		tui.handleMouseKey("mouse-left@5,0")
		if tui.tabs.Expanded[0] {
			t.Fatal("pressing the focused tab's label strip must collapse it")
		}
		if tui.tabs.Focus != -1 {
			t.Fatalf("expected no focused tab after collapsing, got %d", tui.tabs.Focus)
		}
	})

	t.Run("NonFocusedStripFocuses", func(t *testing.T) {
		tui := newTUIForTest()
		tui.width, tui.height = 80, 45
		tui.tabs.Expanded = []bool{true, true, false}
		tui.tabs.HasContent = []bool{true, true, false}
		tui.tabs.Focus = 0
		// The summary tab's label strip is its top row (row 33); the
		// output tab occupies rows 0..32.
		tui.handleMouseKey("mouse-left@5,33")
		if !tui.tabs.Expanded[1] {
			t.Fatal("the summary tab must stay expanded")
		}
		if tui.tabs.Focus != 1 {
			t.Fatalf("expected the focus on the summary tab, got %d", tui.tabs.Focus)
		}
	})

	t.Run("ScrollAreaFocusesAndDragScrolls", func(t *testing.T) {
		tui := newTUIForTest()
		tui.width, tui.height = 80, 45
		tui.tabs.Expanded = []bool{true, true, false}
		tui.tabs.HasContent = []bool{true, true, false}
		tui.tabs.Focus = 1
		tui.scrolls[0].MaxOffset = 100
		tui.scrolls[0].Offset = 10

		tui.handleMouseKey("mouse-left@5,10")
		if tui.tabs.Focus != 0 {
			t.Fatalf("expected the focus on the output tab, got %d", tui.tabs.Focus)
		}
		// Dragging up reveals earlier content.
		tui.handleMouseKey("mouse-leftdrag@5,5")
		if tui.scrolls[0].Offset != 15 {
			t.Fatalf("expected offset 15 after dragging up, got %d", tui.scrolls[0].Offset)
		}
		// Dragging down reveals the tail.
		tui.handleMouseKey("mouse-leftdrag@5,15")
		if tui.scrolls[0].Offset != 5 {
			t.Fatalf("expected offset 5 after dragging down, got %d", tui.scrolls[0].Offset)
		}
		// The drag offset clamps at the content extent.
		tui.handleMouseKey("mouse-leftdrag@5,200")
		if tui.scrolls[0].Offset != 0 {
			t.Fatalf("expected offset 0 after clamping, got %d", tui.scrolls[0].Offset)
		}
		tui.handleMouseKey("mouse-release@5,10")
		// A drag after the release is a no-op.
		tui.handleMouseKey("mouse-leftdrag@5,0")
		if tui.scrolls[0].Offset != 0 {
			t.Fatalf("expected the offset unchanged after release, got %d", tui.scrolls[0].Offset)
		}
	})
}

func TestTuiStateCollapseFocusLastExpanded(t *testing.T) {
	tui := newTUIForTest()
	tui.tabs.AutoExpand(0)
	tui.tabs.AutoExpand(1)
	tui.tabs.AutoExpand(2)
	if tui.tabs.Focus != 0 {
		t.Fatalf("expected focus on tab 0, got %d", tui.tabs.Focus)
	}
	tui.toggleTab(2)
	tui.toggleTab(1)
	if tui.tabs.Focus != 1 {
		t.Fatalf("expected focus on tab 1, got %d", tui.tabs.Focus)
	}
	tui.toggleTab(1)
	if tui.tabs.Expanded[1] {
		t.Fatal("tab 1 should be collapsed")
	}
	if tui.tabs.Focus != 2 {
		t.Fatalf("expected focus to return to tab 2, got %d", tui.tabs.Focus)
	}
}

func TestTUINumberKeySwitchKeepsFollowState(t *testing.T) {
	tui := newTUIForTest()
	tui.tabs.AutoExpand(0)
	tui.tabs.AutoExpand(1)
	tui.scrolls[0].Follow = false
	tui.scrolls[1].Follow = true

	tui.toggleTab(1)
	if tui.tabs.Focus != 1 {
		t.Fatalf("focus should switch to the summary tab, got %d", tui.tabs.Focus)
	}
	if !tui.scrolls[1].Follow {
		t.Fatal("switching to an expanded tab must keep its follow state")
	}

	tui.toggleTab(1)
	if tui.tabs.Expanded[1] {
		t.Fatal("focused summary tab should collapse")
	}
	tui.toggleTab(1)
	if !tui.tabs.Expanded[1] {
		t.Fatal("collapsed summary tab should re-expand")
	}
	if !tui.scrolls[1].Follow {
		t.Fatal("re-expanding a collapsed tab must resume following")
	}
}

func TestTUICycleFocusSkipsCollapsedTabs(t *testing.T) {
	tui := newTUIForTest()
	tui.tabs.Expanded = []bool{true, false, true}
	tui.tabs.Focus = 0
	tui.cycleFocus()
	if tui.tabs.Focus != 2 {
		t.Fatalf("focus should skip the collapsed summary tab and land on logs, got %d", tui.tabs.Focus)
	}
	tui.cycleFocus()
	if tui.tabs.Focus != 0 {
		t.Fatalf("focus should wrap to the output tab, got %d", tui.tabs.Focus)
	}
	tui.tabs.Expanded = []bool{false, false, false}
	tui.cycleFocus()
	if tui.tabs.Focus != -1 {
		t.Fatalf("focus should be -1 with no expanded tabs, got %d", tui.tabs.Focus)
	}
}

func TestScrollClamp(t *testing.T) {
	if got := taiui.ClampOffset(0, 10, 3); got != 0 {
		t.Fatalf("offset 0 should be unchanged, got %d", got)
	}
	if got := taiui.ClampOffset(7, 10, 3); got != 7 {
		t.Fatalf("offset 7 (the max) should be unchanged, got %d", got)
	}
	if got := taiui.ClampOffset(8, 10, 3); got != 7 {
		t.Fatalf("offset 8 should clamp to 7, got %d", got)
	}
	if got := taiui.ClampOffset(100, 10, 3); got != 7 {
		t.Fatalf("offset 100 should clamp to 7, got %d", got)
	}
	if got := taiui.ClampOffset(1<<30, 10, 3); got != 7 {
		t.Fatalf("tail sentinel should clamp to 7, got %d", got)
	}
	if got := taiui.ClampOffset(-5, 10, 3); got != 0 {
		t.Fatalf("negative offset should clamp to 0, got %d", got)
	}
	if got := taiui.ClampOffset(0, 2, 3); got != 0 {
		t.Fatalf("fitted content should clamp to 0, got %d", got)
	}
	if got := taiui.ClampOffset(1<<30, 2, 3); got != 0 {
		t.Fatalf("tail sentinel with fitted content should clamp to 0, got %d", got)
	}
}

// panelTestScreen records presented frames for TUI panel rendering tests.
type panelTestScreen struct {
	width, height int
	frames        []taiui.Frame
}

func (s *panelTestScreen) Width() int { return s.width }

func (s *panelTestScreen) Height() int { return s.height }

func (s *panelTestScreen) Present(f taiui.Frame) {
	s.frames = append(s.frames, f)
}

func TestTUIPanelWrapsLongLines(t *testing.T) {
	src := strings.Repeat("abcdefghijklmnopqrstuvwxyz", 1)
	lines := plainLines(taiui.WrapLines([]string{src, src, src}, 11))
	element := taiui.Panel(
		taiui.Box{Top: 0, Left: 0, Bottom: 6, Right: 12},
		"Output", false, lines, 0, false, true, panelStyle,
	)

	screen := &panelTestScreen{width: 12, height: 6}
	taiui.Render(element, screen)

	if len(screen.frames) == 0 {
		t.Fatal("expected a rendered frame")
	}
	frame := screen.frames[len(screen.frames)-1]
	if cell := frame.Cells[1*frame.Width+0]; cell.Rune != 'a' {
		t.Fatalf("expected 'a' at (0,1), got %v", cell.Rune)
	}
	if cell := frame.Cells[1*frame.Width+10]; cell.Rune != 'k' {
		t.Fatalf("expected 'k' at (10,1), got %v", cell.Rune)
	}
	if cell := frame.Cells[2*frame.Width+0]; cell.Rune != 'l' {
		t.Fatalf("expected 'l' at (0,2), got %v", cell.Rune)
	}
}

func TestTUIPanelScrollbarHiddenWhenFollowing(t *testing.T) {
	var lines []string
	for i := 0; i < 40; i++ {
		lines = append(lines, fmt.Sprintf("line %02d", i))
	}

	renderPanel := func(follow bool) taiui.Frame {
		element := taiui.Panel(
			taiui.Box{Top: 0, Left: 0, Bottom: 10, Right: 80},
			"Output", false, plainLines(lines), 0, false, follow, panelStyle,
		)
		screen := &panelTestScreen{width: 80, height: 10}
		taiui.Render(element, screen)
		if len(screen.frames) == 0 {
			t.Fatal("expected a rendered frame")
		}
		return screen.frames[len(screen.frames)-1]
	}

	following := renderPanel(true)
	rightmost := following.Width - 1
	for y := 0; y < following.Height; y++ {
		if cell := following.Cells[y*following.Width+rightmost]; cell.Rune == '█' {
			t.Fatalf("expected no scrollbar thumb while following, got one at (%d,%d)", rightmost, y)
		}
	}

	scrolled := renderPanel(false)
	foundThumb := false
	for y := 0; y < scrolled.Height; y++ {
		if cell := scrolled.Cells[y*scrolled.Width+rightmost]; cell.Rune == '█' {
			foundThumb = true
			break
		}
	}
	if !foundThumb {
		t.Fatal("expected a scrollbar thumb when scrolled away from the tail")
	}
}

func TestTabPanelBoxClampMatchesScrollView(t *testing.T) {
	tabs := taiui.NewTabs(3)
	tabs.Expanded = []bool{true, true, false}
	tabs.Focus = -1
	boxes := tabs.Boxes(80, 40)
	paneHeight := max(boxes[0].Height()-1, 1)
	if paneHeight != 18 {
		t.Fatalf("expected an 18-row scroll view, got %d", paneHeight)
	}
	const displayLines = 100
	if got := taiui.ClampOffset(1<<30, displayLines, paneHeight); got != displayLines-18 {
		t.Fatalf("expected the tail offset %d, got %d", displayLines-18, got)
	}

	tabs2 := taiui.NewTabs(3)
	tabs2.Expanded = []bool{true, true, false}
	tabs2.Focus = -1
	tabs2.SplitVertical = true
	boxes = tabs2.Boxes(80, 40)
	if paneHeight := max(boxes[0].Height()-1, 1); paneHeight != 39 {
		t.Fatalf("expected a 39-row scroll view, got %d", paneHeight)
	}
}

func TestTUISticksToTail(t *testing.T) {
	tui := newTUIForTest()
	tui.tabs.Expanded = []bool{true, false, false}
	tui.tabs.HasContent = []bool{true, false, false}
	tui.tabs.Focus = 0
	tui.scrolls[0].Follow = true
	tui.screen = taiui.NewTerminalScreen(&strings.Builder{}, 80, 10)
	tui.width = 80
	tui.height = 10

	var sb strings.Builder
	for i := 0; i < 20; i++ {
		fmt.Fprintf(&sb, "line %d\n", i)
	}
	tui.write([]byte(sb.String()))
	tui.render()
	contentWidth := 79
	display := taiui.WrapLinesColored(tui.output.Lines(), contentWidth)
	want := len(display) - 7
	if want < 0 {
		want = 0
	}
	if tui.scrolls[0].Offset != want {
		t.Fatalf("expected topLeft %d, got %d", want, tui.scrolls[0].Offset)
	}
	if !tui.scrolls[0].Follow {
		t.Fatal("expected follow on the output tab")
	}

	tui.write([]byte("new line\nanother line\n"))
	tui.render()
	display = taiui.WrapLinesColored(tui.output.Lines(), contentWidth)
	want = len(display) - 7
	if want < 0 {
		want = 0
	}
	if tui.scrolls[0].Offset != want {
		t.Fatalf("expected topLeft %d after new output, got %d", want, tui.scrolls[0].Offset)
	}
	if !tui.scrolls[0].Follow {
		t.Fatal("expected follow to persist on the output tab")
	}
}

func TestTUIReopenResumesFollow(t *testing.T) {
	tui := newTUIForTest()
	tui.tabs.Expanded = []bool{true, false, false}
	tui.tabs.HasContent = []bool{true, false, false}
	tui.tabs.Focus = 0
	tui.scrolls[0].Follow = true
	tui.screen = taiui.NewTerminalScreen(&strings.Builder{}, 80, 10)
	tui.width = 80
	tui.height = 10

	var sb strings.Builder
	for i := 0; i < 10; i++ {
		fmt.Fprintf(&sb, "line %d\n", i)
	}
	tui.write([]byte(sb.String()))
	tui.render()

	tui.scroll(-1)
	if tui.scrolls[0].Follow {
		t.Fatal("expected follow false after scrolling away")
	}

	tui.toggleTab(0)
	if tui.tabs.Expanded[0] {
		t.Fatal("expected output tab collapsed")
	}
	if tui.tabs.Focus != -1 {
		t.Fatalf("expected focus -1 with no expanded tabs, got %d", tui.tabs.Focus)
	}
	tui.toggleTab(0)
	if !tui.tabs.Expanded[0] {
		t.Fatal("expected output tab re-expanded")
	}
	if tui.tabs.Focus != 0 {
		t.Fatalf("expected focus 0 after re-expand, got %d", tui.tabs.Focus)
	}
	if !tui.scrolls[0].Follow {
		t.Fatal("expected follow true after re-expand")
	}

	tui.write([]byte("line 10\nline 11\n"))
	tui.render()
	contentWidth := 79
	display := taiui.WrapLinesColored(tui.output.Lines(), contentWidth)
	want := len(display) - 7
	if want < 0 {
		want = 0
	}
	if tui.scrolls[0].Offset != want {
		t.Fatalf("expected topLeft %d after new content, got %d", want, tui.scrolls[0].Offset)
	}
	if !tui.scrolls[0].Follow {
		t.Fatal("expected follow to persist after new content")
	}
}

func TestTUIStopsFollowingWhenScrolledAway(t *testing.T) {
	tui := newTUIForTest()
	tui.tabs.Expanded = []bool{true, false, false}
	tui.tabs.HasContent = []bool{true, false, false}
	tui.tabs.Focus = 0
	tui.scrolls[0].Follow = true
	tui.screen = taiui.NewTerminalScreen(&strings.Builder{}, 80, 10)
	tui.width = 80
	tui.height = 10

	var sb strings.Builder
	for i := 0; i < 20; i++ {
		fmt.Fprintf(&sb, "line %d\n", i)
	}
	tui.write([]byte(sb.String()))
	tui.render()

	contentWidth := 79
	display := taiui.WrapLinesColored(tui.output.Lines(), contentWidth)
	initialMax := len(display) - 7
	if initialMax < 0 {
		initialMax = 0
	}

	tui.scroll(-3)
	if tui.scrolls[0].Offset != initialMax-3 {
		t.Fatalf("expected topLeft %d after scrolling up, got %d", initialMax-3, tui.scrolls[0].Offset)
	}
	if tui.scrolls[0].Follow {
		t.Fatal("expected follow cleared after scrolling away")
	}

	tui.write([]byte("more\n"))
	tui.render()
	if tui.scrolls[0].Offset != initialMax-3 {
		t.Fatalf("expected view to stay at %d while scrolled away, got %d", initialMax-3, tui.scrolls[0].Offset)
	}
	if tui.scrolls[0].Follow {
		t.Fatal("expected follow to stay cleared while scrolled away")
	}

	tui.scrollTo(1 << 30)
	if !tui.scrolls[0].Follow {
		t.Fatal("expected follow restored at the end")
	}
	tui.write([]byte("tail\n"))
	tui.render()
	display = taiui.WrapLinesColored(tui.output.Lines(), contentWidth)
	want := len(display) - 7
	if want < 0 {
		want = 0
	}
	if tui.scrolls[0].Offset != want {
		t.Fatalf("expected topLeft %d after resuming follow, got %d", want, tui.scrolls[0].Offset)
	}
	if !tui.scrolls[0].Follow {
		t.Fatal("expected follow to persist after resuming")
	}
}

func TestTUIPageScrollUsesPaneHeight(t *testing.T) {
	tui := newTUIForTest()
	tui.tabs.Expanded = []bool{true, false, false}
	tui.tabs.HasContent = []bool{true, false, false}
	tui.tabs.Focus = 0
	tui.scrolls[0].Follow = false
	tui.screen = taiui.NewTerminalScreen(&strings.Builder{}, 80, 10)
	tui.width = 80
	tui.height = 10

	var sb strings.Builder
	for i := 0; i < 100; i++ {
		fmt.Fprintf(&sb, "line %d\n", i)
	}
	tui.write([]byte(sb.String()))
	tui.render()
	tui.scrolls[0].Offset = 0
	tui.scrolls[0].Follow = false

	tui.pageScroll(1)
	if tui.scrolls[0].Offset != 6 {
		t.Fatalf("expected topLeft 6 after page down, got %d", tui.scrolls[0].Offset)
	}
	if tui.scrolls[0].Follow {
		t.Fatal("expected follow cleared after page down from the top")
	}

	tui.pageScroll(-1)
	if tui.scrolls[0].Offset != 0 {
		t.Fatalf("expected topLeft 0 after page up, got %d", tui.scrolls[0].Offset)
	}

	tui.scrolls[0].Offset = 90
	tui.scrolls[0].Follow = false
	tui.pageScroll(1)
	if tui.scrolls[0].Offset != tui.scrolls[0].MaxOffset {
		t.Fatalf("expected topLeft clamped to %d, got %d", tui.scrolls[0].MaxOffset, tui.scrolls[0].Offset)
	}
}

func TestTUIDownAtEndKeepsFollow(t *testing.T) {
	tui := newTUIForTest()
	tui.tabs.Expanded = []bool{true, false, false}
	tui.tabs.HasContent = []bool{true, false, false}
	tui.tabs.Focus = 0
	tui.scrolls[0].Follow = true
	tui.screen = taiui.NewTerminalScreen(&strings.Builder{}, 80, 10)
	tui.width = 80
	tui.height = 10

	var sb strings.Builder
	for i := 0; i < 20; i++ {
		fmt.Fprintf(&sb, "line %d\n", i)
	}
	tui.write([]byte(sb.String()))
	tui.render()

	tui.scroll(1)
	if !tui.scrolls[0].Follow {
		t.Fatal("down at the latest row must keep following")
	}
	if tui.scrolls[0].Offset != tui.scrolls[0].MaxOffset {
		t.Fatalf("expected topLeft at max offset %d, got %d", tui.scrolls[0].MaxOffset, tui.scrolls[0].Offset)
	}
}

func TestTUIQuitConfirmation(t *testing.T) {
	t.Run("FirstPressShowsConfirmationSecondPressQuits", func(t *testing.T) {
		tui := newTUIForTest()
		if tui.handleQuitKey() {
			t.Fatal("the first quit key press must not quit")
		}
		if !tui.quit.Pending() {
			t.Fatal("the first quit key press must set the confirmation state")
		}
		if !tui.handleQuitKey() {
			t.Fatal("the second quit key press must confirm the quit")
		}
	})

	t.Run("AnyOtherKeyCancels", func(t *testing.T) {
		tui := newTUIForTest()
		tui.handleQuitKey()
		if !tui.quit.Pending() {
			t.Fatal("expected the confirmation state after the first quit key")
		}
		tui.cancelConfirmQuit()
		if tui.quit.Pending() {
			t.Fatal("a non-quit key must cancel the pending quit confirmation")
		}
		if tui.handleQuitKey() {
			t.Fatal("a quit key after cancellation must not quit immediately")
		}
	})

	t.Run("ConfirmationBarRendered", func(t *testing.T) {
		var sb strings.Builder
		tui := newTUIForTest()
		tui.screen = taiui.NewTerminalScreen(&sb, 80, 10)
		tui.width = 80
		tui.height = 10
		tui.handleQuitKey()
		tui.render()
		if !strings.Contains(sb.String(), "Quit?") {
			t.Fatalf("expected the quit confirmation bar in the rendered output, got: %q", sb.String())
		}
		sb.Reset()
		tui.cancelConfirmQuit()
		tui.render()
		if strings.Contains(sb.String(), "Quit?") {
			t.Fatalf("expected no confirmation bar without a pending confirmation, got: %q", sb.String())
		}
	})
}

func TestTUIMouseWheel(t *testing.T) {
	t.Run("ScrollsTabUnderCursorWithoutChangingFocus", func(t *testing.T) {
		tui := newTUIForTest()
		tui.width, tui.height = 80, 45
		tui.tabs.Expanded = []bool{true, false, true}
		tui.tabs.HasContent = []bool{true, false, true}
		tui.tabs.Focus = 0
		tui.scrolls[2].MaxOffset = 100
		tui.scrolls[2].Offset = 50
		// Horizontal split: the unfocused Logs tab is capped to
		// logsMaxBoxHeight rows, so the output tab occupies rows 0..40,
		// the collapsed summary tab row 41, and the logs tab rows
		// 42..44. A wheel event over the logs tab scrolls its view
		// without changing the focus.
		tui.handleMouseKey("mouse-wheel-down@5,43")
		if tui.scrolls[2].Offset != 51 {
			t.Fatalf("expected offset 51, got %d", tui.scrolls[2].Offset)
		}
		if tui.tabs.Focus != 0 {
			t.Fatalf("wheel must not change the focus, got %d", tui.tabs.Focus)
		}
		tui.handleMouseKey("mouse-wheel-up@5,43")
		if tui.scrolls[2].Offset != 50 {
			t.Fatalf("expected offset 50, got %d", tui.scrolls[2].Offset)
		}
	})

	t.Run("CollapsedTabIsNoOp", func(t *testing.T) {
		tui := newTUIForTest()
		tui.width, tui.height = 80, 45
		tui.tabs.Expanded = []bool{true, false, true}
		tui.tabs.HasContent = []bool{true, false, true}
		tui.tabs.Focus = 0
		tui.scrolls[1].MaxOffset = 100
		tui.scrolls[1].Offset = 10
		// A wheel event over the collapsed summary row (41) is a no-op.
		tui.handleMouseKey("mouse-wheel-down@5,41")
		if tui.scrolls[2].Offset != 0 {
			t.Fatal("wheel must not affect another tab")
		}
		if tui.scrolls[1].Offset != 10 || tui.tabs.Focus != 0 {
			t.Fatal("wheel over a collapsed tab must be a no-op")
		}
	})
}

func TestTUIRenderBuildsViewFromState(t *testing.T) {
	// render() forks the current TUI state values into a fresh dscope view
	// scope on every call and lets the provider graph derive the panels;
	// there is no cached scope or lazy initialization to manage. A second
	// render after new output shows the updated content. See TheoryOfTUI.
	var sb strings.Builder
	tui := newTUIForTest()
	tui.screen = taiui.NewTerminalScreen(&sb, 80, 10)
	tui.width = 80
	tui.height = 10
	tui.write([]byte("hello\n"))
	tui.render()
	if !strings.Contains(sb.String(), "hello") {
		t.Fatalf("expected rendered output, got: %q", sb.String())
	}
	sb.Reset()
	tui.write([]byte("world\n"))
	tui.render()
	if !strings.Contains(sb.String(), "world") {
		t.Fatalf("expected updated rendered output, got: %q", sb.String())
	}
}

func TestTabPanelBoxWeighted(t *testing.T) {
	tabs := taiui.NewTabs(3)
	// The first set of assertions exercises the side-by-side (vertical
	// split) layout; the default is horizontal (stacked). See
	// TheoryOfTUI.
	tabs.SplitVertical = true
	tabs.Expanded = []bool{true, true, false}
	tabs.Focus = 0
	boxes := tabs.Boxes(90, 40)
	if boxes[0].Left != 0 || boxes[0].Right != 66 || boxes[0].Top != 0 || boxes[0].Bottom != 40 {
		t.Fatalf("unexpected focused panel box: %+v", boxes[0])
	}
	if boxes[1].Left != 66 || boxes[1].Right != 89 {
		t.Fatalf("unexpected non-focused panel box: %+v", boxes[1])
	}
	if boxes[2].Left != 89 || boxes[2].Right != 90 {
		t.Fatalf("unexpected collapsed panel box: %+v", boxes[2])
	}

	tabs.Focus = 1
	boxes = tabs.Boxes(90, 40)
	if boxes[0].Left != 0 || boxes[0].Right != 22 {
		t.Fatalf("unexpected non-focused panel box: %+v", boxes[0])
	}
	if boxes[1].Left != 22 || boxes[1].Right != 89 {
		t.Fatalf("unexpected focused panel box: %+v", boxes[1])
	}

	tabs.Focus = -1
	boxes = tabs.Boxes(90, 40)
	if boxes[0].Left != 0 || boxes[0].Right != 44 {
		t.Fatalf("unexpected equal-share panel box: %+v", boxes[0])
	}
	if boxes[1].Left != 44 || boxes[1].Right != 89 {
		t.Fatalf("unexpected equal-share panel box: %+v", boxes[1])
	}

	tabs2 := taiui.NewTabs(3)
	tabs2.Expanded = []bool{true, true, false}
	tabs2.Focus = 0
	boxes = tabs2.Boxes(80, 45)
	if boxes[0].Top != 0 || boxes[0].Bottom != 33 {
		t.Fatalf("unexpected focused panel box: %+v", boxes[0])
	}
	if boxes[1].Top != 33 || boxes[1].Bottom != 44 {
		t.Fatalf("unexpected non-focused panel box: %+v", boxes[1])
	}
	if boxes[2].Top != 44 || boxes[2].Bottom != 45 {
		t.Fatalf("unexpected collapsed panel box: %+v", boxes[2])
	}

	tabs3 := taiui.NewTabs(3)
	// The last set of assertions also exercises the side-by-side layout.
	tabs3.SplitVertical = true
	tabs3.Expanded = []bool{true, true, true}
	tabs3.Focus = 1
	boxes = tabs3.Boxes(90, 24)
	if boxes[0].Left != 0 || boxes[0].Right != 18 {
		t.Fatalf("unexpected first panel box: %+v", boxes[0])
	}
	if boxes[1].Left != 18 || boxes[1].Right != 72 {
		t.Fatalf("unexpected focused middle panel box: %+v", boxes[1])
	}
	if boxes[2].Left != 72 || boxes[2].Right != 90 {
		t.Fatalf("unexpected last panel box: %+v", boxes[2])
	}
}

func TestTabPanelBoxCollapsedInPlace(t *testing.T) {
	tabs := taiui.NewTabs(3)
	// Side-by-side (vertical split) layout is exercised explicitly; the
	// default is horizontal (stacked).
	tabs.SplitVertical = true
	tabs.Expanded = []bool{true, false, true}
	tabs.Focus = 0
	boxes := tabs.Boxes(90, 40)
	// The focused tab has weight 3, the other expanded tab weight 1: the
	// expanded width (89) splits as 66 and 23, and the collapsed tab
	// keeps its one-column strip in the middle.
	if boxes[0].Left != 0 || boxes[0].Right != 66 {
		t.Fatalf("unexpected output panel box: %+v", boxes[0])
	}
	if boxes[1].Left != 66 || boxes[1].Right != 67 {
		t.Fatalf("collapsed round tab must stay in the middle, got %+v", boxes[1])
	}
	if boxes[2].Left != 67 || boxes[2].Right != 90 {
		t.Fatalf("unexpected logs panel box: %+v", boxes[2])
	}

	tabs2 := taiui.NewTabs(3)
	tabs2.Expanded = []bool{true, false, true}
	tabs2.Focus = 0
	boxes = tabs2.Boxes(80, 45)
	// The stacked layout splits the expanded height (44) the same way:
	// 33 rows for the focused tab, 11 for the other expanded tab.
	if boxes[0].Top != 0 || boxes[0].Bottom != 33 {
		t.Fatalf("unexpected output panel box: %+v", boxes[0])
	}
	if boxes[1].Top != 33 || boxes[1].Bottom != 34 {
		t.Fatalf("collapsed round tab must stay in the middle, got %+v", boxes[1])
	}
	if boxes[2].Top != 34 || boxes[2].Bottom != 45 {
		t.Fatalf("unexpected logs panel box: %+v", boxes[2])
	}
}

func TestTabPanelBoxCollapsedFirstAndLast(t *testing.T) {
	tabs := taiui.NewTabs(3)
	// Side-by-side (vertical split) layout is exercised explicitly; the
	// default is horizontal (stacked).
	tabs.SplitVertical = true
	tabs.Expanded = []bool{false, true, false}
	tabs.Focus = 1
	boxes := tabs.Boxes(90, 40)
	if boxes[0].Left != 0 || boxes[0].Right != 1 {
		t.Fatalf("unexpected collapsed output panel box: %+v", boxes[0])
	}
	if boxes[1].Left != 1 || boxes[1].Right != 89 {
		t.Fatalf("unexpected expanded round panel box: %+v", boxes[1])
	}
	if boxes[2].Left != 89 || boxes[2].Right != 90 {
		t.Fatalf("unexpected collapsed logs panel box: %+v", boxes[2])
	}
}

func TestCollapsedPanelRendering(t *testing.T) {
	style := panelStyle

	t.Run("Horizontal", func(t *testing.T) {
		element := taiui.CollapsedPanel(taiui.Box{Top: 0, Left: 0, Bottom: 1, Right: 12}, "1 Output", false, style)
		screen := &panelTestScreen{width: 12, height: 1}
		taiui.Render(element, screen)
		if len(screen.frames) == 0 {
			t.Fatal("expected a rendered frame")
		}
		frame := screen.frames[len(screen.frames)-1]
		if cell := frame.Cells[2]; cell.Rune != '1' {
			t.Fatalf("expected '1' at (2,0), got %v", cell.Rune)
		}
		if cell := frame.Cells[4]; cell.Rune != 'O' {
			t.Fatalf("expected 'O' at (4,0), got %v", cell.Rune)
		}
	})

	t.Run("Vertical", func(t *testing.T) {
		element := taiui.CollapsedPanel(taiui.Box{Top: 0, Left: 0, Bottom: 8, Right: 1}, "1 Output", false, style)
		screen := &panelTestScreen{width: 1, height: 8}
		taiui.Render(element, screen)
		if len(screen.frames) == 0 {
			t.Fatal("expected a rendered frame")
		}
		frame := screen.frames[len(screen.frames)-1]
		if cell := frame.Cells[0]; cell.Rune != '1' {
			t.Fatalf("expected '1' at (0,0), got %v", cell.Rune)
		}
		if cell := frame.Cells[1*frame.Width+0]; cell.Rune != ' ' {
			t.Fatalf("expected space at (0,1), got %v", cell.Rune)
		}
		if cell := frame.Cells[2*frame.Width+0]; cell.Rune != 'O' {
			t.Fatalf("expected 'O' at (0,2), got %v", cell.Rune)
		}
	})
}

func TestComputeTabBoxesAllCollapsed(t *testing.T) {
	tabs := taiui.NewTabs(3)
	tabs.SplitVertical = true
	boxes := tabs.Boxes(80, 24)
	for i := 0; i < 3; i++ {
		if boxes[i].Width() != 1 {
			t.Fatalf("tab %d: expected 1-column box, got %+v", i, boxes[i])
		}
		if boxes[i].Top != 0 || boxes[i].Bottom != 24 {
			t.Fatalf("tab %d: expected full-height box, got %+v", i, boxes[i])
		}
	}
	if boxes[0].Left != 0 || boxes[1].Left != 1 || boxes[2].Left != 2 {
		t.Fatalf("unexpected collapsed layout: %+v", boxes)
	}

	tabs2 := taiui.NewTabs(3)
	boxes = tabs2.Boxes(80, 24)
	for i := 0; i < 3; i++ {
		if boxes[i].Height() != 1 {
			t.Fatalf("tab %d: expected 1-row box, got %+v", i, boxes[i])
		}
		if boxes[i].Left != 0 || boxes[i].Right != 80 {
			t.Fatalf("tab %d: expected full-width box, got %+v", i, boxes[i])
		}
	}
	if boxes[0].Top != 0 || boxes[1].Top != 1 || boxes[2].Top != 2 {
		t.Fatalf("unexpected collapsed layout: %+v", boxes)
	}
}

func TestTuiStateAutoExpandTabs(t *testing.T) {
	tui := newTUIForTest()
	writeModelOutput(tui, "model output\n")
	if !tui.tabs.Expanded[0] {
		t.Fatal("output tab should auto-expand on streamed output")
	}
	if tui.tabs.Focus != 0 {
		t.Fatalf("expected focus on the output tab, got %d", tui.tabs.Focus)
	}

	tui.writeLogs([]byte("msg=\"log record\"\n"))
	if !tui.tabs.Expanded[2] {
		t.Fatal("logs tab should auto-expand on log records")
	}
	if tui.tabs.Focus != 0 {
		t.Fatalf("auto-expand must not change an established focus, got %d", tui.tabs.Focus)
	}

	tui.handleEvent(pipeline.Event{Kind: pipeline.EventAttemptCompleted, Attempt: 1, Summary: "- done"})
	if !tui.tabs.Expanded[1] {
		t.Fatal("events tab should auto-expand on a rendered event")
	}
	if tui.tabs.Focus != 0 {
		t.Fatalf("auto-expand must not change an established focus, got %d", tui.tabs.Focus)
	}
	if len(tui.events) != 1 || len(tui.events[0]) != 2 {
		t.Fatalf("expected one event group with header and summary lines, got %v", tui.events)
	}
	if tui.events[0][0].Text != "✅ [Attempt 1 complete]" {
		t.Fatalf("expected the emoji header first in the event group, got %q", tui.events[0][0].Text)
	}
	if tui.events[0][1].Text != "- done" {
		t.Fatalf("expected the summary line after the header, got %q", tui.events[0][1].Text)
	}

	tui2 := newTUIForTest()
	tui2.tabs.Expanded = []bool{true, false, false}
	tui2.tabs.HasContent = []bool{true, false, false}
	tui2.tabs.Focus = 0
	tui2.handleEvent(pipeline.Event{Kind: pipeline.EventAttemptStart, Attempt: 1})
	if !tui2.tabs.Expanded[1] {
		t.Fatal("events tab should auto-expand on the attempt start event")
	}
	if tui2.tabs.Focus != 0 {
		t.Fatalf("auto-expand must not change an established focus, got %d", tui2.tabs.Focus)
	}
}

func TestTuiStateAutoExpandPreservesFocus(t *testing.T) {
	tui := newTUIForTest()
	tui.tabs.Expanded = []bool{true, false, false}
	tui.tabs.HasContent = []bool{true, false, false}
	tui.tabs.Focus = 0
	tui.writeLogs([]byte("msg=\"log record\"\n"))
	if !tui.tabs.Expanded[2] {
		t.Fatal("logs tab should auto-expand on log records")
	}
	if tui.tabs.Focus != 0 {
		t.Fatalf("focus must stay on the output tab, got %d", tui.tabs.Focus)
	}
	if !tui.scrolls[2].Follow {
		t.Fatal("auto-expanded tab should follow the tail")
	}
	tui.cycleFocus()
	if tui.tabs.Focus != 2 {
		t.Fatalf("expected focus to cycle to the logs tab, got %d", tui.tabs.Focus)
	}
}

func TestTuiStateEmptyWriteDoesNotExpandTabs(t *testing.T) {
	tui := newTUIForTest()
	tui.write(nil)
	tui.writeLogs(nil)
	for i := 0; i < 3; i++ {
		if tui.tabs.Expanded[i] {
			t.Fatalf("tab %d must not expand on empty writes", i)
		}
	}
	if tui.tabs.Focus != -1 {
		t.Fatalf("expected no focus change on empty writes, got %d", tui.tabs.Focus)
	}
}

func TestTuiStateAutoExpandOnlyFirstContent(t *testing.T) {
	tui := newTUIForTest()
	tui.write([]byte("first output\n"))
	if !tui.tabs.Expanded[0] {
		t.Fatal("output tab should auto-expand on first content")
	}
	tui.toggleTab(0)
	if tui.tabs.Expanded[0] {
		t.Fatal("output tab should be collapsed")
	}
	tui.write([]byte("more output\n"))
	if tui.tabs.Expanded[0] {
		t.Fatal("output tab must not re-expand on subsequent content")
	}
}

func TestWithTUIOutputObserver(t *testing.T) {
	tui := newTUIForTest()
	var gotOpts pipeline.RunOptions
	run := func(ctx context.Context, opts pipeline.RunOptions, result *pipeline.Result) iter.Seq2[pipeline.Event, error] {
		gotOpts = opts
		return func(yield func(pipeline.Event, error) bool) {
			// The run yields the finish reason and the attempt summary as
			// events; the wrapper's tap must forward both to the TUI.
			if !yield(pipeline.Event{Kind: pipeline.EventFinish, Detail: "stop"}, nil) {
				return
			}
			yield(pipeline.Event{Kind: pipeline.EventAttemptCompleted, Attempt: 1, Summary: "- done"}, nil)
		}
	}
	wrapped := withTUIOutputObserver(run, tui)

	var result pipeline.Result
	for _, e := range wrapped(context.Background(), pipeline.RunOptions{}, &result) {
		if e != nil {
			t.Fatal(e)
		}
	}
	if len(gotOpts.StateDecorators) != 1 {
		t.Fatalf("expected 1 state decorator, got %d", len(gotOpts.StateDecorators))
	}

	var state generators.State = generators.NewPrompts("", nil)
	state, err := gotOpts.StateDecorators[0](state).AppendContent(&generators.Content{
		Role: generators.RoleModel,
		Parts: []generators.Part{
			generators.Text("model output\n"),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	state, err = state.AppendContent(&generators.Content{
		Role: generators.RoleModel,
		Parts: []generators.Part{
			generators.Thought("deep thinking\n"),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	state, err = state.AppendContent(&generators.Content{
		Role: generators.RoleModel,
		Parts: []generators.Part{
			generators.Text("answer\n"),
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	tui.mu.Lock()
	defer tui.mu.Unlock()
	var sb strings.Builder
	for _, line := range tui.output.Lines() {
		sb.WriteString(line.Text)
		sb.WriteString("\n")
	}
	output := sb.String()
	for _, want := range []string{"model output", "deep thinking", "answer"} {
		if !strings.Contains(output, want) {
			t.Fatalf("expected %q in output, got %q", want, output)
		}
	}
	if !strings.Contains(output, "deep thinking\n\nanswer") {
		t.Fatalf("expected a blank line between the thought and the answer, got %q", output)
	}
	if tui.generating {
		t.Fatal("expected the finish event to clear the generating hint")
	}
	var events strings.Builder
	for _, group := range tui.events {
		for _, line := range group {
			events.WriteString(line.Text)
			events.WriteString("\n")
		}
	}
	rendered := events.String()
	if !strings.Contains(rendered, "[Finish: stop]") {
		t.Fatalf("expected the finish line in the events tab, got %q", rendered)
	}
	if !strings.Contains(rendered, "- done") {
		t.Fatalf("expected the attempt summary in the events tab, got %q", rendered)
	}
}

func TestTUIHandleEventRendersKinds(t *testing.T) {
	tui := newTUIForTest()
	tui.generating = true
	tui.handleEvent(pipeline.Event{Kind: pipeline.EventFinish, Detail: "stop"})
	if len(tui.events) != 1 || len(tui.events[0]) != 1 {
		t.Fatalf("expected 1 event group of 1 line, got %v", tui.events)
	}
	if tui.events[0][0].Text != "🏁 [Finish: stop]" || tui.events[0][0].Color != outputColorLogLine {
		t.Fatalf("unexpected finish line: %+v", tui.events[0][0])
	}
	if tui.generating {
		t.Fatal("expected the finish event to clear the generating hint")
	}

	usage := generators.Usage{}
	usage.Prompt.TokenCount = 100
	tui.handleEvent(pipeline.Event{Kind: pipeline.EventUsage, Attempt: 2, Detail: "error", Usage: usage})
	last := tui.events[len(tui.events)-1]
	if last[0].Text != "📊 [Usage] attempt 2 (error): prompt 100, cached 0, completion 0, thoughts 0" {
		t.Fatalf("unexpected usage line: %q", last[0].Text)
	}
	if last[0].Color != outputColorLogLine {
		t.Fatalf("expected log color for the usage line, got %v", last[0].Color)
	}

	measuredUsage := generators.Usage{}
	measuredUsage.Candidates.TokenCount = 20
	measuredUsage.TimeToFirstToken = 300 * time.Millisecond // ttft 0.3s
	measuredUsage.GenerateDuration = 200 * time.Millisecond // 20 tokens / 0.2s -> 100.0 tok/s
	tui.handleEvent(pipeline.Event{Kind: pipeline.EventUsage, Attempt: 5, Usage: measuredUsage})
	measuredLines := tui.events[len(tui.events)-1]
	wantMeasured := "📊 [Usage] attempt 5: prompt 0, cached 0, completion 20, thoughts 0, ttft 0.3s, 100.0 tok/s"
	if measuredLines[0].Text != wantMeasured {
		t.Fatalf("unexpected measured usage line: %q", measuredLines[0].Text)
	}

	tui.handleEvent(pipeline.Event{Kind: pipeline.EventThoughtSummary, Summary: "- point"})
	group := tui.events[len(tui.events)-1]
	if group[0].Text != "💭 [Thought Summary]" || group[0].Color != outputColorThoughtLine {
		t.Fatalf("unexpected thought summary header: %+v", group[0])
	}
	if body := group[1]; body.Text != "- point" || body.Color != taiui.NoColor {
		t.Fatalf("unexpected thought summary body: %+v", body)
	}

	tui.handleEvent(pipeline.Event{Kind: pipeline.EventAttemptStart, Attempt: 3})
	group = tui.events[len(tui.events)-1]
	if group[0].Text != "🚀 [Attempt 3 start]" || group[0].Color != outputColorLogLine {
		t.Fatalf("unexpected attempt start line: %+v", group[0])
	}

	tui.handleEvent(pipeline.Event{Kind: pipeline.EventAttemptCompleted, Attempt: 3})
	group = tui.events[len(tui.events)-1]
	if group[0].Text != "✅ [Attempt 3 complete]" || group[0].Color != outputColorLogLine {
		t.Fatalf("unexpected empty-summary completion line: %+v", group[0])
	}

	// The truncated display pairs the session-wide attempt number
	// with the in-generation position over the retry budget.
	// See TheoryOfLoopEvents.
	tui.handleEvent(pipeline.Event{Kind: pipeline.EventTruncated, Attempt: 4, AttemptInGeneration: 1, MaxAttempts: 3, Detail: "missing completion"})
	group = tui.events[len(tui.events)-1]
	if group[0].Text != "✂️ [Attempt 4 truncated (attempt 1/3): missing completion]" {
		t.Fatalf("unexpected truncated line: %+v", group[0])
	}

	tui.handleEvent(pipeline.Event{Kind: pipeline.EventKind("custom-kind"), Detail: "note"})
	group = tui.events[len(tui.events)-1]
	if group[0].Text != "❓ [Event custom-kind] note" || group[0].Color != outputColorLogLine {
		t.Fatalf("unexpected generic event line: %+v", group[0])
	}
}

// TestTUIEventLinesRenderGoalLoopPrefix verifies the loop attribution of
// the per-attempt events: a goal run's start, completion, and usage
// lines carry the "loop L attempt N" prefix, while a non-goal event
// keeps the bare attempt label, so non-goal display bytes stay
// unchanged. See TheoryOfTUI and pipeline.TheoryOfLoopEvents.
func TestTUIEventLinesRenderGoalLoopPrefix(t *testing.T) {
	usage := generators.Usage{}
	usage.Prompt.TokenCount = 100

	startLines := eventLines(pipeline.Event{Kind: pipeline.EventAttemptStart, Loop: 3, Attempt: 2})
	if startLines[0].Text != "🚀 [loop 3 attempt 2 start]" {
		t.Fatalf("unexpected goal-loop attempt start line: %q", startLines[0].Text)
	}

	completeLines := eventLines(pipeline.Event{Kind: pipeline.EventAttemptCompleted, Loop: 3, Attempt: 2})
	if completeLines[0].Text != "✅ [loop 3 attempt 2 complete]" {
		t.Fatalf("unexpected goal-loop complete line: %q", completeLines[0].Text)
	}

	usageLines := eventLines(pipeline.Event{Kind: pipeline.EventUsage, Loop: 3, Attempt: 2, Usage: usage})
	if usageLines[0].Text != "📊 [Usage] loop 3 attempt 2: prompt 100, cached 0, completion 0, thoughts 0" {
		t.Fatalf("unexpected goal-loop usage line: %q", usageLines[0].Text)
	}

	plain := eventLines(pipeline.Event{Kind: pipeline.EventAttemptStart, Attempt: 2})
	if plain[0].Text != "🚀 [Attempt 2 start]" {
		t.Fatalf("unexpected non-goal attempt start line: %q", plain[0].Text)
	}
}

func TestTUICaptureContentNotifies(t *testing.T) {
	tui := newTUIForTest()
	tui.updateCh = make(chan struct{}, 1)
	state := generators.NewPrompts("", nil)
	s := tuiOutputState{upstream: state, tui: tui}
	if _, err := s.AppendContent(&generators.Content{
		Role:  generators.RoleModel,
		Parts: []generators.Part{generators.Text("model output\n")},
	}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-tui.updateCh:
	default:
		t.Fatal("expected a notification when model output is captured")
	}
}

func TestTuiStateFlushTerminatesPartialOutputLine(t *testing.T) {
	tui := newTUIForTest()
	state := generators.NewPrompts("", nil)
	s := tuiOutputState{upstream: state, tui: tui}

	// Stream model output that ends without a trailing newline.
	if _, err := s.AppendContent(&generators.Content{
		Role:  generators.RoleModel,
		Parts: []generators.Part{generators.Text("model output")},
	}); err != nil {
		t.Fatal(err)
	}
	tui.mu.Lock()
	partial := tui.output.HasPartial()
	tui.mu.Unlock()
	if !partial {
		t.Fatal("expected a partial line before flush")
	}

	// Flush terminates the partial line.
	if _, err := s.Flush(); err != nil {
		t.Fatal(err)
	}
	tui.mu.Lock()
	hasPartial := tui.output.HasPartial()
	tui.mu.Unlock()
	if hasPartial {
		t.Fatal("flush must terminate a partial output line")
	}

	// A subsequent write — e.g., command output via the Output writer —
	// must start on a fresh line instead of being merged into the model's
	// final line.
	tui.write([]byte("command output\n"))
	tui.mu.Lock()
	defer tui.mu.Unlock()
	lines := tui.output.Lines()
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d: %v", len(lines), lines)
	}
	if lines[0].Text != "model output" || lines[1].Text != "command output" {
		t.Fatalf("unexpected lines: %v", lines)
	}
}

func TestTuiStateFlushKeepsCompleteLine(t *testing.T) {
	tui := newTUIForTest()
	state := generators.NewPrompts("", nil)
	s := tuiOutputState{upstream: state, tui: tui}

	// Stream model output that already ends with a newline.
	if _, err := s.AppendContent(&generators.Content{
		Role:  generators.RoleModel,
		Parts: []generators.Part{generators.Text("model output\n")},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Flush(); err != nil {
		t.Fatal(err)
	}
	tui.mu.Lock()
	defer tui.mu.Unlock()
	lines := tui.output.Lines()
	if len(lines) != 1 || lines[0].Text != "model output" {
		t.Fatalf("unexpected lines: %v", lines)
	}
	if tui.output.HasPartial() {
		t.Fatal("flush must not leave a partial line when output already ends with a newline")
	}
}

func TestRoleColor(t *testing.T) {
	cases := []struct {
		role generators.Role
		want taiui.Color
	}{
		{generators.RoleUser, outputColorUserLine},
		{generators.RoleTool, outputColorToolLine},
		{generators.RoleSystem, outputColorSystemLine},
		{generators.RoleLog, outputColorLogLine},
		{generators.RoleModel, taiui.NoColor},
		{generators.RoleAssistant, taiui.NoColor},
	}
	for _, c := range cases {
		if got := roleColor(c.role); got != c.want {
			t.Fatalf("roleColor(%s) = %#x, want %#x", c.role, got, c.want)
		}
	}
}

func TestTUICaptureContentRoleColors(t *testing.T) {
	tui := newTUIForTest()
	state := generators.NewPrompts("", nil)
	s := tuiOutputState{upstream: state, tui: tui}
	for _, c := range []struct {
		role generators.Role
		text string
	}{
		{generators.RoleUser, "user\n"},
		{generators.RoleModel, "model\n"},
		{generators.RoleTool, "tool\n"},
		{generators.RoleSystem, "system\n"},
		{generators.RoleLog, "log\n"},
	} {
		if _, err := s.AppendContent(&generators.Content{
			Role:  c.role,
			Parts: []generators.Part{generators.Text(c.text)},
		}); err != nil {
			t.Fatal(err)
		}
	}
	tui.mu.Lock()
	defer tui.mu.Unlock()
	lines := tui.output.Lines()
	want := []struct {
		text  string
		color taiui.Color
	}{
		{"user", outputColorUserLine},
		{"", taiui.NoColor},
		{"model", taiui.NoColor},
		{"", taiui.NoColor},
		{"tool", outputColorToolLine},
		{"", taiui.NoColor},
		{"system", outputColorSystemLine},
		{"", taiui.NoColor},
		{"log", outputColorLogLine},
	}
	if len(lines) != len(want) {
		t.Fatalf("expected %d lines, got %d: %v", len(want), len(lines), lines)
	}
	for i, w := range want {
		if lines[i].Text != w.text || lines[i].Color != w.color {
			t.Fatalf("line %d: got %+v, want text %q color %#x", i, lines[i], w.text, w.color)
		}
	}
}

func TestTUICaptureContentThoughtColor(t *testing.T) {
	tui := newTUIForTest()
	state := generators.NewPrompts("", nil)
	s := tuiOutputState{upstream: state, tui: tui}
	if _, err := s.AppendContent(&generators.Content{
		Role:  generators.RoleModel,
		Parts: []generators.Part{generators.Thought("thinking\n")},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AppendContent(&generators.Content{
		Role:  generators.RoleModel,
		Parts: []generators.Part{generators.Text("answer\n")},
	}); err != nil {
		t.Fatal(err)
	}
	tui.mu.Lock()
	defer tui.mu.Unlock()
	lines := tui.output.Lines()
	want := []struct {
		text  string
		color taiui.Color
	}{
		{"thinking", outputColorThoughtLine},
		{"", taiui.NoColor},
		{"answer", taiui.NoColor},
	}
	if len(lines) != len(want) {
		t.Fatalf("expected %d lines, got %d: %v", len(want), len(lines), lines)
	}
	for i, w := range want {
		if lines[i].Text != w.text || lines[i].Color != w.color {
			t.Fatalf("line %d: got %+v, want text %q color %#x", i, lines[i], w.text, w.color)
		}
	}
}

func TestWrapTabLinesCarriesColors(t *testing.T) {
	lines := []taiui.Line{
		{Text: "aaa bbb", Color: outputColorUserLine},
		{Text: "ccc", Color: taiui.NoColor},
	}
	wrapped := taiui.WrapLinesColored(lines, 5)
	want := []taiui.Line{
		{Text: "aaa", Color: outputColorUserLine},
		{Text: "bbb", Color: outputColorUserLine},
		{Text: "ccc", Color: taiui.NoColor},
	}
	if len(wrapped) != len(want) {
		t.Fatalf("expected %d lines, got %d: %v", len(want), len(wrapped), wrapped)
	}
	for i := range want {
		if wrapped[i] != want[i] {
			t.Fatalf("line %d: got %+v, want %+v", i, wrapped[i], want[i])
		}
	}
}

func TestTUIPanelColorsContent(t *testing.T) {
	lines := []taiui.Line{
		{Text: "red", Color: outputColorLogLine},
		{Text: "plain", Color: taiui.NoColor},
	}
	element := taiui.Panel(
		taiui.Box{Top: 0, Left: 0, Bottom: 3, Right: 10},
		"Output", false, lines, 0, false, true, panelStyle,
	)
	screen := &panelTestScreen{width: 10, height: 3}
	taiui.Render(element, screen)
	if len(screen.frames) == 0 {
		t.Fatal("expected a rendered frame")
	}
	frame := screen.frames[len(screen.frames)-1]
	cell := frame.Cells[1*frame.Width+0]
	if cell.Rune != 'r' {
		t.Fatalf("expected 'r' at (0,1), got %v", cell.Rune)
	}
	r, g, b := cell.Style.Fg().RGB()
	if !(r == 0xff && g == 0 && b == 0) {
		t.Fatalf("expected red foreground, got %#x %#x %#x", r, g, b)
	}
	cell = frame.Cells[2*frame.Width+0]
	if cell.Rune != 'p' {
		t.Fatalf("expected 'p' at (0,2), got %v", cell.Rune)
	}
	if r, g, b := cell.Style.Fg().RGB(); r >= 0 || g >= 0 || b >= 0 {
		t.Fatal("expected default foreground for plain line")
	}
}

func TestTUIPanelColorsUseAnsi16Palette(t *testing.T) {
	lines := []taiui.Line{
		{Text: "u", Color: outputColorUserLine},
		{Text: "t", Color: outputColorToolLine},
		{Text: "s", Color: outputColorSystemLine},
		{Text: "l", Color: outputColorLogLine},
		{Text: "m", Color: outputColorThoughtLine},
	}
	element := taiui.LinesElement(lines, taiui.Box{Top: 0, Left: 0, Bottom: 5, Right: 40})
	screen := &panelTestScreen{width: 40, height: 5}
	taiui.Render(element, screen)
	if len(screen.frames) == 0 {
		t.Fatal("expected a rendered frame")
	}
	frame := screen.frames[len(screen.frames)-1]
	want := []taiui.Color{outputColorUserLine, outputColorToolLine, outputColorSystemLine, outputColorLogLine, outputColorThoughtLine}
	for i, c := range want {
		cell := frame.Cells[i*frame.Width]
		if !cell.Set {
			t.Fatalf("expected row %d to be painted", i)
		}
		fg := cell.Style.Fg()
		if fg&color.IsRGB != 0 {
			t.Fatalf("color %d must be a palette color, not true-color RGB", i)
		}
		// The palette index is the low byte of the color value; the high
		// bits carry the IsValid marker, so the comparison masks them off.
		if got := int(fg & 0xff); got != int(c&0xff) {
			t.Fatalf("color %d: expected ANSI 16 palette index %d, got %d", i, int(c&0xff), got)
		}
	}
}

func TestTUIJumpToTransition(t *testing.T) {
	// The [ ] shortcuts must jump the Output tab's view through the
	// section transitions, letting the user quickly browse the whole
	// output. The Output tab colors each section by its role and
	// thinking state, so a transition is a color change between
	// consecutive display lines. Each transition yields two stops — the
	// previous section's end anchored at the pane bottom and the new
	// section's start anchored at the pane top — so the walk covers both
	// sides of every change. See TheoryOfTUI.

	// setupLong builds an output long enough to scroll, with sections
	// (plain, thought, plain, tool, plain) whose transitions sit at
	// display indices 20, 21, 41, and 42: the thought and tool sections
	// each contribute an entry boundary and an exit boundary. With the
	// full-height output pane (box height 8, pane height 7 after the
	// one-row label strip), each boundary contributes an exit stop at
	// boundary-7 and an entry stop at the boundary, so the forward walk
	// visits offsets 13, 14, 20, 21, 34, 35, 41, 42: at each change the
	// view first shows the previous section's end at the pane bottom,
	// then the new section's start at the pane top.
	setupLong := func(t *testing.T) *TUI {
		t.Helper()
		tui := newTUIForTest()
		tui.tabs.Expanded = []bool{true, false, false}
		tui.tabs.HasContent = []bool{true, false, false}
		tui.tabs.Focus = 0
		tui.screen = taiui.NewTerminalScreen(&strings.Builder{}, 80, 10)
		tui.width = 80
		tui.height = 10
		var b strings.Builder
		for i := 0; i < 20; i++ {
			fmt.Fprintf(&b, "plain %02d\n", i)
		}
		tui.writeColored(taiui.NoColor, []byte(b.String()))
		tui.writeColored(outputColorThoughtLine, []byte("a thought\n"))
		b.Reset()
		for i := 0; i < 20; i++ {
			fmt.Fprintf(&b, "middle %02d\n", i)
		}
		tui.writeColored(taiui.NoColor, []byte(b.String()))
		tui.writeColored(outputColorToolLine, []byte("a tool call\n"))
		b.Reset()
		for i := 0; i < 20; i++ {
			fmt.Fprintf(&b, "tail %02d\n", i)
		}
		tui.writeColored(taiui.NoColor, []byte(b.String()))
		tui.scrolls[0].Follow = false
		return tui
	}

	t.Run("Next", func(t *testing.T) {
		tui := setupLong(t)
		tui.jumpToTransition(1)
		if tui.scrolls[0].Offset != 13 {
			t.Fatalf("expected offset 13 at the plain section's end stop, got %d", tui.scrolls[0].Offset)
		}
		if tui.tabs.Focus != 0 {
			t.Fatalf("expected the output tab focused after the jump, got %d", tui.tabs.Focus)
		}
		tui.jumpToTransition(1)
		if tui.scrolls[0].Offset != 14 {
			t.Fatalf("expected offset 14 at the thought section's end stop, got %d", tui.scrolls[0].Offset)
		}
		tui.jumpToTransition(1)
		if tui.scrolls[0].Offset != 20 {
			t.Fatalf("expected offset 20 at the thought entry stop, got %d", tui.scrolls[0].Offset)
		}
		tui.jumpToTransition(1)
		if tui.scrolls[0].Offset != 21 {
			t.Fatalf("expected offset 21 at the middle entry stop, got %d", tui.scrolls[0].Offset)
		}
		tui.jumpToTransition(1)
		if tui.scrolls[0].Offset != 34 {
			t.Fatalf("expected offset 34 at the middle section's end stop, got %d", tui.scrolls[0].Offset)
		}
		tui.jumpToTransition(1)
		if tui.scrolls[0].Offset != 35 {
			t.Fatalf("expected offset 35 at the tool section's end stop, got %d", tui.scrolls[0].Offset)
		}
		tui.jumpToTransition(1)
		if tui.scrolls[0].Offset != 41 {
			t.Fatalf("expected offset 41 at the tool entry stop, got %d", tui.scrolls[0].Offset)
		}
		tui.jumpToTransition(1)
		if tui.scrolls[0].Offset != 42 {
			t.Fatalf("expected offset 42 at the tail entry stop, got %d", tui.scrolls[0].Offset)
		}
		// Past the last stop no transition lies ahead: the forward key
		// falls back to the live tail — the symmetric endpoint of the
		// backward fallback to the content start — instead of silently
		// doing nothing. The display holds 62 lines and the pane shows
		// 7, so the tail offset is 55.
		tui.jumpToTransition(1)
		if tui.scrolls[0].Offset != 55 {
			t.Fatalf("expected offset 55 at the live tail past the last stop, got %d", tui.scrolls[0].Offset)
		}
		tui.jumpToTransition(1)
		if tui.scrolls[0].Offset != 55 {
			t.Fatalf("expected the view to stay at the tail, got %d", tui.scrolls[0].Offset)
		}
	})

	t.Run("Previous", func(t *testing.T) {
		tui := setupLong(t)
		tui.scrolls[0].Offset = 42
		tui.jumpToTransition(-1)
		if tui.scrolls[0].Offset != 41 {
			t.Fatalf("expected offset 41 after the prev-jump, got %d", tui.scrolls[0].Offset)
		}
		tui.jumpToTransition(-1)
		if tui.scrolls[0].Offset != 35 {
			t.Fatalf("expected offset 35 after the second prev-jump, got %d", tui.scrolls[0].Offset)
		}
		tui.jumpToTransition(-1)
		if tui.scrolls[0].Offset != 34 {
			t.Fatalf("expected offset 34 after the third prev-jump, got %d", tui.scrolls[0].Offset)
		}
		tui.jumpToTransition(-1)
		if tui.scrolls[0].Offset != 21 {
			t.Fatalf("expected offset 21 after the fourth prev-jump, got %d", tui.scrolls[0].Offset)
		}
		tui.jumpToTransition(-1)
		if tui.scrolls[0].Offset != 20 {
			t.Fatalf("expected offset 20 after the fifth prev-jump, got %d", tui.scrolls[0].Offset)
		}
		tui.jumpToTransition(-1)
		if tui.scrolls[0].Offset != 14 {
			t.Fatalf("expected offset 14 after the sixth prev-jump, got %d", tui.scrolls[0].Offset)
		}
		tui.jumpToTransition(-1)
		if tui.scrolls[0].Offset != 13 {
			t.Fatalf("expected offset 13 after the seventh prev-jump, got %d", tui.scrolls[0].Offset)
		}
		// Past the first transition's exit stop the [ key must reach the
		// very beginning of the content: the first display line is never
		// a stop, so without the fallback the start of the first
		// section would be unreachable.
		tui.jumpToTransition(-1)
		if tui.scrolls[0].Offset != 0 {
			t.Fatalf("expected the view to reach the very beginning, got %d", tui.scrolls[0].Offset)
		}
		tui.jumpToTransition(-1)
		if tui.scrolls[0].Offset != 0 {
			t.Fatalf("expected the view to stay at 0 at the very beginning, got %d", tui.scrolls[0].Offset)
		}
	})

	t.Run("FromTailSentinel", func(t *testing.T) {
		// Before the first render the scroll offset is the tail sentinel;
		// the jump clamps it against the fresh display so it anchors at
		// the content end, and the previous jump lands on the last
		// stop.
		tui := setupLong(t)
		tui.scrolls[0].Offset = 1 << 30
		tui.jumpToTransition(-1)
		if tui.scrolls[0].Offset != 42 {
			t.Fatalf("expected the last stop when jumping back from the tail, got %d", tui.scrolls[0].Offset)
		}
	})

	t.Run("ExpandsCollapsedTab", func(t *testing.T) {
		tui := setupLong(t)
		tui.tabs.Toggle(0) // collapse the focused output tab
		if tui.tabs.Expanded[0] {
			t.Fatal("expected the output tab collapsed")
		}
		tui.jumpToTransition(1)
		if !tui.tabs.Expanded[0] {
			t.Fatal("the jump must expand a collapsed output tab")
		}
		if tui.tabs.Focus != 0 {
			t.Fatalf("expected the output tab focused after the jump, got %d", tui.tabs.Focus)
		}
		if tui.scrolls[0].Offset != 13 {
			t.Fatalf("expected offset 13 after the jump, got %d", tui.scrolls[0].Offset)
		}
	})

	t.Run("TakesFocusFromAnotherTab", func(t *testing.T) {
		tui := setupLong(t)
		tui.tabs.Expanded = []bool{true, true, false}
		tui.tabs.HasContent = []bool{true, true, false}
		tui.tabs.Focus = 1
		tui.jumpToTransition(1)
		if tui.tabs.Focus != 0 {
			t.Fatalf("the jump must take the focus on the output tab, got %d", tui.tabs.Focus)
		}
		// The output pane is smaller here (box height 6, pane height 5),
		// so the first stop sits at boundary 20 minus 5.
		if tui.scrolls[0].Offset != 15 {
			t.Fatalf("expected offset 15 after the jump, got %d", tui.scrolls[0].Offset)
		}
	})

	t.Run("EmptyContent", func(t *testing.T) {
		tui := newTUIForTest()
		tui.tabs.Expanded = []bool{true, false, false}
		tui.tabs.HasContent = []bool{true, false, false}
		tui.tabs.Focus = 0
		tui.scrolls[0].Offset = 0
		tui.screen = taiui.NewTerminalScreen(&strings.Builder{}, 80, 10)
		tui.width = 80
		tui.height = 10
		tui.jumpToTransition(1)
		if tui.scrolls[0].Offset != 0 {
			t.Fatalf("expected the view unchanged without content, got %d", tui.scrolls[0].Offset)
		}
	})

	t.Run("NoTransitions", func(t *testing.T) {
		tui := newTUIForTest()
		tui.tabs.Expanded = []bool{true, false, false}
		tui.tabs.HasContent = []bool{true, false, false}
		tui.tabs.Focus = 0
		tui.screen = taiui.NewTerminalScreen(&strings.Builder{}, 80, 10)
		tui.width = 80
		tui.height = 10
		// All lines share the default color: no transitions to jump to.
		// The forward key must still move the view — the whole output is
		// one section, so it falls back to the live tail — just as the
		// backward key falls back to the very beginning. With 20 lines
		// in a 7-row pane the tail offset is 13.
		for i := 0; i < 20; i++ {
			tui.write([]byte(fmt.Sprintf("line %02d\n", i)))
		}
		tui.scrolls[0].Offset = 0
		tui.jumpToTransition(1)
		if tui.scrolls[0].Offset != 13 {
			t.Fatalf("expected the ] key to reach the live tail without transitions, got %d", tui.scrolls[0].Offset)
		}
	})

	t.Run("PreviousWithoutTransitions", func(t *testing.T) {
		// With no section transitions in the output, the [ key must
		// still reach the very beginning of the content: the whole
		// output is one section, so its start is the previous-navigation
		// target.
		tui := newTUIForTest()
		tui.tabs.Expanded = []bool{true, false, false}
		tui.tabs.HasContent = []bool{true, false, false}
		tui.tabs.Focus = 0
		tui.screen = taiui.NewTerminalScreen(&strings.Builder{}, 80, 10)
		tui.width = 80
		tui.height = 10
		// All lines share the default color: no transitions to jump to.
		for i := 0; i < 20; i++ {
			tui.write([]byte(fmt.Sprintf("line %02d\n", i)))
		}
		tui.scrolls[0].Offset = 5
		tui.jumpToTransition(-1)
		if tui.scrolls[0].Offset != 0 {
			t.Fatalf("expected the [ key to reach the very beginning without transitions, got %d", tui.scrolls[0].Offset)
		}
	})
}

func TestReadTUIKeysHelp(t *testing.T) {
	ch := make(chan string, 10)
	go taiui.ReadKeys(strings.NewReader("?\x1b[A"), ch)
	var got []string
	for len(got) < 2 {
		select {
		case k := <-ch:
			got = append(got, mapTUIKey(k))
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for keys")
		}
	}
	want := []string{"help", "up"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("key %d: expected %q, got %q", i, want[i], got[i])
		}
	}
}

func TestMapTUIKey(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"q", "quit"},
		{"Q", "quit"},
		{"ctrl-c", "quit"},
		{"s", "split"},
		{"S", "split"},
		{"?", "help"},
		{"[", "prev-transition"},
		{"]", "next-transition"},
		// Unmapped keys pass through unchanged.
		{"up", "up"},
		{"down", "down"},
		{"1", "1"},
		{"tab", "tab"},
		{"mouse-left@5,5", "mouse-left@5,5"},
	}
	for _, c := range cases {
		if got := mapTUIKey(c.in); got != c.want {
			t.Fatalf("mapTUIKey(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestMapTUIKeyMouse(t *testing.T) {
	for _, key := range []string{"m", "M"} {
		if got := mapTUIKey(key); got != "mouse" {
			t.Fatalf("mapTUIKey(%q) = %q, want %q", key, got, "mouse")
		}
	}
}

func TestTUIHelpToggle(t *testing.T) {
	tui := newTUIForTest()
	if tui.showHelp {
		t.Fatal("help must start hidden")
	}
	tui.toggleHelp()
	if !tui.showHelp {
		t.Fatal("toggleHelp must show the help overlay")
	}
	tui.toggleHelp()
	if tui.showHelp {
		t.Fatal("toggleHelp must hide the help overlay on the second press")
	}
}

func TestTUIToggleMouse(t *testing.T) {
	// toggleMouse records a status line in the Logs tab, so the test
	// constructs the buffers a running session would have. The session
	// itself stays nil — only the recorded state flips, without touching
	// a terminal; the taiui side owns the terminal-sequence test
	// (TestSessionSetMouse).
	tui := &TUI{
		output:         taiui.NewLineBuffer(0),
		logs:           taiui.NewStringBuffer(0),
		tabs:           taiui.NewTabs(3),
		updateCh:       make(chan struct{}, 8),
		mouseReporting: true,
	}
	tui.toggleMouse()
	if tui.mouseReporting {
		t.Fatal("expected mouse reporting disabled after the first toggle")
	}
	logRecords := tui.logs.Lines()
	lastLog := ""
	if len(logRecords) > 0 {
		lastLog = logRecords[len(logRecords)-1]
	}
	if !strings.Contains(lastLog, "mouse reporting off") {
		t.Fatalf("expected a mouse-reporting line in the logs tab, got %v", logRecords)
	}
	if out := tui.output.Lines(); len(out) > 0 {
		t.Fatalf("the output tab must not carry the notice, got %v", out)
	}
	tui.toggleMouse()
	if !tui.mouseReporting {
		t.Fatal("expected mouse reporting re-enabled after the second toggle")
	}
}

func TestTUIHelpOverlay(t *testing.T) {
	var sb strings.Builder
	tui := newTUIForTest()
	tui.screen = taiui.NewTerminalScreen(&sb, 80, 10)
	tui.width = 80
	tui.height = 10
	tui.showHelp = true
	tui.render()
	output := sb.String()
	if !strings.Contains(output, "Help") {
		t.Fatalf("expected the help title in the rendered output, got: %q", output)
	}
	if !strings.Contains(output, "1 / 2 / 3") {
		t.Fatalf("expected the key bindings in the rendered output, got: %q", output)
	}
}

func TestReadTUIKeysSS3AndVT220(t *testing.T) {
	ch := make(chan string, 10)
	go taiui.ReadKeys(strings.NewReader("\x1bOA\x1bOB\x1bOH\x1bOF\x1b[1~\x1b[4~"), ch)
	var got []string
	for len(got) < 6 {
		select {
		case k := <-ch:
			got = append(got, mapTUIKey(k))
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for keys")
		}
	}
	want := []string{"up", "down", "home", "end", "home", "end"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("key %d: expected %q, got %q", i, want[i], got[i])
		}
	}
}

func TestTUIWrappedDisplayCache(t *testing.T) {
	tui := newTUIForTest()
	box := taiui.Box{Top: 0, Left: 0, Bottom: 10, Right: 40}

	tui.write([]byte("hello\n"))
	display1 := wrappedDisplay(tui, 0, box)
	if len(display1) != 1 || display1[0].Text != "hello" {
		t.Fatalf("unexpected display1: %v", display1)
	}

	tui.write([]byte("partial text"))
	display2 := wrappedDisplay(tui, 0, box)
	if len(display2) != 2 || display2[1].Text != "partial text" {
		t.Fatalf("unexpected display2: %v", display2)
	}

	tui.write([]byte("\nworld\n"))
	display3 := wrappedDisplay(tui, 0, box)
	if len(display3) != 3 || display3[1].Text != "partial text" || display3[2].Text != "world" {
		t.Fatalf("unexpected display3: %v", display3)
	}

	// Width resize re-wraps the whole content; the incremental cache
	// internals are covered by taiui's own tests.
	boxWider := taiui.Box{Top: 0, Left: 0, Bottom: 10, Right: 80}
	displayWider := wrappedDisplay(tui, 0, boxWider)
	if len(displayWider) != 3 {
		t.Fatalf("expected 3 lines after resize, got %d", len(displayWider))
	}
}

func TestTUIWrappedDisplayLargeOutputPerformance(t *testing.T) {
	tui := newTUIForTest()
	box := taiui.Box{Top: 0, Left: 0, Bottom: 25, Right: 80}

	for i := 0; i < 50000; i++ {
		tui.output.Append(taiui.NoColor, fmt.Sprintf("line %d\n", i))
	}

	display := wrappedDisplay(tui, 0, box)
	if len(display) != 50000 {
		t.Fatalf("expected 50000 display lines, got %d", len(display))
	}

	// Appending one new line wraps incrementally.
	tui.output.Append(taiui.NoColor, "new final line\n")
	display2 := wrappedDisplay(tui, 0, box)
	if len(display2) != 50001 {
		t.Fatalf("expected 50001 display lines, got %d", len(display2))
	}
}
