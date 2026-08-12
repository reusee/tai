package main

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v3/color"
	"github.com/reusee/tai/flags"
	"github.com/reusee/tai/generators"
	"github.com/reusee/tai/loops"
	"github.com/reusee/tai/taiui"
)

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

func plainDisplay(lines []string) []displayLine {
	out := make([]displayLine, 0, len(lines))
	for _, line := range lines {
		out = append(out, displayLine{text: line})
	}
	return out
}

func TestTuiStateWriteLines(t *testing.T) {
	st := &tuiState{}
	st.write([]byte("hello\nworld\n"))
	if len(st.lines) != 2 || st.lines[0].text != "hello" || st.lines[1].text != "world" {
		t.Fatalf("unexpected lines: %v", st.lines)
	}
}

func TestDisplayChatInput(t *testing.T) {
	tui := &TUI{}
	displayChatInput(tui, flags.Chats{"hello", "world"})
	tui.mu.Lock()
	defer tui.mu.Unlock()
	if len(tui.lines) != 2 {
		t.Fatalf("expected 2 lines, got %d: %v", len(tui.lines), tui.lines)
	}
	if tui.lines[0].text != "hello" || tui.lines[0].color != outputColorUser {
		t.Fatalf("unexpected first line: %+v", tui.lines[0])
	}
	if tui.lines[1].text != "world" || tui.lines[1].color != outputColorUser {
		t.Fatalf("unexpected second line: %+v", tui.lines[1])
	}
	// The Output tab expands and takes the focus for the chat input.
	if !tui.expanded[0] {
		t.Fatal("output tab should auto-expand on chat input")
	}
	if tui.focus != 0 {
		t.Fatalf("expected focus on the output tab, got %d", tui.focus)
	}
	if !tui.follow[0] {
		t.Fatal("output tab should follow the tail")
	}
}

func TestDisplayChatInputEmpty(t *testing.T) {
	tui := &TUI{}
	displayChatInput(tui, nil)
	tui.mu.Lock()
	defer tui.mu.Unlock()
	if len(tui.lines) != 0 {
		t.Fatalf("expected no lines for empty chats, got %v", tui.lines)
	}
	if tui.expanded[0] {
		t.Fatal("output tab must not expand for empty chats")
	}
}

func TestTuiStateWriteLogs(t *testing.T) {
	st := &tuiState{}
	st.writeLogs([]byte("hello\nworld\n"))
	if len(st.logs) != 2 || st.logs[0] != "hello" || st.logs[1] != "world" {
		t.Fatalf("unexpected logs: %v", st.logs)
	}
}

func TestTuiStateRequesting(t *testing.T) {
	// The Output tab's "generating" hint appears while a generation
	// request is in flight and clears when the request returns: a fresh
	// state with no activity and a finished session are never
	// "requesting", the request-start log sets the hint, and the finish
	// reason clears it. Finish reasons are read from the generation
	// state, not scanned from rendered text. See TheoryOfTUI.
	st := &tuiState{}
	if st.requesting() {
		t.Fatal("expected not requesting with no activity")
	}
	st.writeLogs([]byte("level=INFO msg=generating name=model\n"))
	if !st.requesting() {
		t.Fatal("expected requesting after the generating log")
	}
	// The finish reason marks the request as returned: the hint clears
	// even though the session has not ended (e.g., waiting for the next
	// round or user input).
	st.finishReason("stop")
	if st.requesting() {
		t.Fatal("expected not requesting after the finish reason")
	}
	st.finished = true
	if st.requesting() {
		t.Fatal("expected not requesting when finished")
	}
}

func TestTuiStateRequestingLogsWrite(t *testing.T) {
	// The generator logs a record at the start of each request (e.g., the
	// "generating" log in the generators package), so a log write also
	// marks the model as actively generating — this covers the wait for
	// the first output byte, before any content has streamed.
	// See TheoryOfTUI.
	st := &tuiState{}
	st.writeLogs([]byte("msg=\"generating\"\n"))
	if !st.requesting() {
		t.Fatal("expected requesting after log write")
	}
}

func TestIsGeneratingLog(t *testing.T) {
	// The request-start log appears as "msg=generating" (bare message)
	// or `msg="generating"` (quoted message). The detection requires the
	// value to be followed by a space or the line end, so a log about
	// "generating" that is not the request-start record (e.g.,
	// "generating failed") is not mistaken for one. See TheoryOfTUI.
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
	// The generating hint reflects the request lifecycle, not a time
	// window: it persists for the whole request (including silent
	// thinking phases) and clears when the request returns with a finish
	// reason. Before the fix, the hint was a recency timeout, so it
	// disappeared after a silence and only reappeared on the next output.
	// See TheoryOfTUI.
	st := &tuiState{}
	st.writeLogs([]byte("level=INFO msg=generating name=model\n"))
	if !st.requesting() {
		t.Fatal("expected requesting after the generating log")
	}
	st.finishReason("stop")
	if st.requesting() {
		t.Fatal("expected not requesting after the finish reason")
	}
}

func TestTUIOutputTabLabel(t *testing.T) {
	// The Output tab title carries the session-state hint: "generating..."
	// while a generation request is in flight, "(done)" after the session
	// ends, and the plain title otherwise. The generating hint also
	// requests the active-request highlight. The request is marked in
	// flight by the generator's "generating" log. See TheoryOfTUI.
	st := &tuiState{}
	if label, highlight := st.outputTabLabel(); label != "Output" || highlight {
		t.Fatalf("expected plain Output label, got label %q highlight %v", label, highlight)
	}
	st.writeLogs([]byte("level=INFO msg=generating name=model\n"))
	if label, highlight := st.outputTabLabel(); label != "Output (generating...)" || !highlight {
		t.Fatalf("expected generating hint with highlight, got label %q highlight %v", label, highlight)
	}
	st.finished = true
	if label, highlight := st.outputTabLabel(); label != "Output (done)" || highlight {
		t.Fatalf("expected done hint without highlight, got label %q highlight %v", label, highlight)
	}
}

func TestTUIPanelTitleHighlightedDuringRequest(t *testing.T) {
	// While a generation request is in flight, the Output tab's title is
	// drawn in tabActiveLabelFg so the in-flight request is visible at a
	// glance. An idle session keeps the ordinary title color. The
	// request is marked in flight by the generator's "generating" log.
	// See TheoryOfTUI.
	renderTitle := func(tui *TUI, focus bool) taiui.Frame {
		element := tui.panel(0, taiui.Box{Top: 0, Left: 0, Bottom: 2, Right: 12}, plainDisplay([]string{"content"}), 0, focus)
		screen := &panelTestScreen{width: 12, height: 2}
		taiui.Render(taiui.NewBaseScope(func() taiui.Root {
			return taiui.Root{Element: element}
		}), screen)
		if len(screen.frames) == 0 {
			t.Fatal("expected a rendered frame")
		}
		return screen.frames[len(screen.frames)-1]
	}

	tui := &TUI{}
	tui.writeLogs([]byte("level=INFO msg=generating name=model\n"))
	frame := renderTitle(tui, false)
	cell := frame.Cells[2] // 'O' of the title at (2,0)
	if cell.Rune != 'O' {
		t.Fatalf("expected title 'O' at (2,0), got %v", cell.Rune)
	}
	wantR, wantG, wantB := color.PaletteColor(int(tabActiveLabelFg)).RGB()
	if r, g, b := cell.Style.Fg().RGB(); r != wantR || g != wantG || b != wantB {
		t.Fatalf("expected highlighted title foreground %#x %#x %#x, got %#x %#x %#x", wantR, wantG, wantB, r, g, b)
	}

	// An idle session shows the plain title without the highlight.
	idle := &TUI{}
	idleFrame := renderTitle(idle, false)
	idleCell := idleFrame.Cells[2]
	if idleCell.Rune != 'O' {
		t.Fatalf("expected title 'O' at (2,0), got %v", idleCell.Rune)
	}
	if r, g, b := idleCell.Style.Fg().RGB(); r == wantR && g == wantG && b == wantB {
		t.Fatal("expected the idle title to keep the ordinary foreground color")
	}
}

func TestTuiStateLogsPartialLines(t *testing.T) {
	st := &tuiState{}
	st.writeLogs([]byte("foo"))
	st.writeLogs([]byte("bar\n"))
	if len(st.logs) != 1 || st.logs[0] != "foobar" {
		t.Fatalf("unexpected logs: %v", st.logs)
	}
	if st.logsPartial != "" {
		t.Fatalf("unexpected partial: %q", st.logsPartial)
	}
	st.writeLogs([]byte("baz"))
	if st.logsPartial != "baz" {
		t.Fatalf("unexpected partial: %q", st.logsPartial)
	}
	lines := st.logsLinesForRender()
	if len(lines) != 2 || lines[1] != "baz" {
		t.Fatalf("unexpected rendered log lines: %v", lines)
	}
}

func TestTuiLogsWriterWritesToLogs(t *testing.T) {
	tui := &TUI{}
	writer := logsWriter{t: tui}
	if _, err := writer.Write([]byte("hello\n")); err != nil {
		t.Fatal(err)
	}
	tui.mu.Lock()
	defer tui.mu.Unlock()
	if len(tui.logs) != 1 || tui.logs[0] != "hello" {
		t.Fatalf("unexpected logs: %v", tui.logs)
	}
}

func TestPlainOutputLinesAlternatesBackgrounds(t *testing.T) {
	lines := plainOutputLines([]string{"a", "b", "c"}, tabUnfocusBG)
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d", len(lines))
	}
	for i, line := range lines {
		want := int32(tabUnfocusBG)
		if i%2 == 1 {
			want = logAltBG(tabUnfocusBG)
		}
		if line.bgColor != want {
			t.Fatalf("line %d: expected background %#x, got %#x", i, want, line.bgColor)
		}
		if line.color != 0 {
			t.Fatalf("line %d: expected no foreground color, got %#x", i, line.color)
		}
	}
	if logAltBG(tabUnfocusBG) == tabUnfocusBG {
		t.Fatal("alternate background must differ from the base")
	}
}

func TestLogAltBG(t *testing.T) {
	// The alternate shade shifts each channel of the base toward the
	// mid-gray, so it works on both the focused (gray) and unfocused
	// (dark blue) tab backgrounds.
	for _, base := range []int32{tabUnfocusBG, tabFocusBG} {
		r1, g1, b1 := color.NewHexColor(base).RGB()
		r2, g2, b2 := color.NewHexColor(logAltBG(base)).RGB()
		if !(r2 > r1 && g2 > g1 && b2 > b1) {
			t.Fatalf("expected alternate lighter than base %#x, got %#x %#x %#x -> %#x %#x %#x",
				base, r1, g1, b1, r2, g2, b2)
		}
	}
}

func TestColoredTextAlternatingBackgrounds(t *testing.T) {
	alt := logAltBG(tabUnfocusBG)
	lines := []displayLine{
		{text: "first", bgColor: tabUnfocusBG},
		{text: "second", bgColor: alt},
	}
	element := coloredText(lines, taiui.Box{Top: 0, Left: 0, Bottom: 2, Right: 10})
	screen := &panelTestScreen{width: 10, height: 2}
	taiui.Render(taiui.NewBaseScope(func() taiui.Root {
		return taiui.Root{Element: element}
	}), screen)
	if len(screen.frames) == 0 {
		t.Fatal("expected a rendered frame")
	}
	frame := screen.frames[len(screen.frames)-1]

	// The first row carries the base background across its full width.
	wantR, wantG, wantB := color.NewHexColor(tabUnfocusBG).RGB()
	cell := frame.Cells[9] // row 0, rightmost column: a fill cell
	if !cell.Set {
		t.Fatal("expected the first row painted with its background")
	}
	if r, g, b := cell.Style.Bg().RGB(); r != wantR || g != wantG || b != wantB {
		t.Fatalf("expected base background %#x, got %#x %#x %#x", tabUnfocusBG, r, g, b)
	}

	// The second row carries the alternate background.
	wantR, wantG, wantB = color.NewHexColor(alt).RGB()
	cell = frame.Cells[19] // row 1, rightmost column: a fill cell
	if !cell.Set {
		t.Fatal("expected the second row painted with its background")
	}
	if r, g, b := cell.Style.Bg().RGB(); r != wantR || g != wantG || b != wantB {
		t.Fatalf("expected alternate background %#x, got %#x %#x %#x", alt, r, g, b)
	}
}

func TestTuiStatePartialLines(t *testing.T) {
	st := &tuiState{}
	st.write([]byte("foo"))
	st.write([]byte("bar\n"))
	if len(st.lines) != 1 || st.lines[0].text != "foobar" {
		t.Fatalf("unexpected lines: %v", st.lines)
	}
	if st.partial.text != "" {
		t.Fatalf("unexpected partial: %q", st.partial.text)
	}
	st.write([]byte("baz"))
	if st.partial.text != "baz" {
		t.Fatalf("unexpected partial: %q", st.partial.text)
	}
	lines := st.outputLinesForRender()
	if len(lines) != 2 || lines[1].text != "baz" {
		t.Fatalf("unexpected rendered lines: %v", lines)
	}
}

func TestTuiStateParsesSummaries(t *testing.T) {
	st := &tuiState{}
	st.write([]byte("<<徕珑龘 <summary>\n- one\n- two\n徕珑龘\n"))
	if len(st.signals) != 3 {
		t.Fatalf("expected 3 signal lines, got %v", st.signals)
	}
	if st.signals[0].text != "- one" || st.signals[1].text != "- two" || st.signals[2].text != "" {
		t.Fatalf("unexpected signals: %v", st.signals)
	}
}

func TestTuiStateParsesSummariesAcrossChunks(t *testing.T) {
	st := &tuiState{}
	st.write([]byte("<<徕珑龘 <summary>\n- one\n- tw"))
	st.write([]byte("o\n徕珑龘\n"))
	if len(st.signals) != 3 {
		t.Fatalf("expected 3 signal lines, got %v", st.signals)
	}
	if st.signals[0].text != "- one" || st.signals[1].text != "- two" {
		t.Fatalf("unexpected signals: %v", st.signals)
	}
}

func TestTuiStateIgnoresOtherBlocks(t *testing.T) {
	st := &tuiState{}
	text := "<<龘靐齉 <change op=\"MODIFY\" target=\"Foo\" file-path=\"x.go\">\nfunc Foo() {}\n龘靐齉\n" +
		"<<徕珑龘 <summary>\n- s\n徕珑龘\n"
	st.write([]byte(text))
	if len(st.signals) != 2 || st.signals[0].text != "- s" || st.signals[1].text != "" {
		t.Fatalf("unexpected signals: %v", st.signals)
	}
}

func TestTuiStateParsesSummariesSkipsTruncatedFragment(t *testing.T) {
	// An unclosed fragment from a truncated round must not wedge the
	// parse buffer: when a complete block exists beyond the fragment's
	// opening line, the fragment is skipped and the later summary is
	// still extracted. See TheoryOfTUI.
	st := &tuiState{}
	st.write([]byte("<<徕珑龘 <change op=\"MODIFY\" target=\"Foo\" file-path=\"/x.go\">\nfunc Foo() {\n"))
	st.write([]byte("round 2 output\n<<黿鼍爩 <summary>\n- done\n黿鼍爩\n"))
	if len(st.signals) != 2 {
		t.Fatalf("expected 2 signal lines, got %v", st.signals)
	}
	if st.signals[0].text != "- done" || st.signals[1].text != "" {
		t.Fatalf("unexpected signals: %v", st.signals)
	}
}

func TestTuiOutputPreservesIndentation(t *testing.T) {
	// The TUI wraps streamed output with taiui.WrapLines before display.
	// An indented code line that fits the tab width must keep its
	// indentation; without the wrap fast path, the slow path dropped
	// leading spaces and code displayed in the TUI lost all indentation.
	st := &tuiState{}
	st.write([]byte("    func main() {\n        fmt.Println(1)\n    }\n"))
	lines := st.outputLinesForRender()
	wrapped := wrapTabLines(lines, 80)
	want := []string{
		"    func main() {",
		"        fmt.Println(1)",
		"    }",
	}
	if len(wrapped) != len(want) {
		t.Fatalf("expected %d lines, got %d: %q", len(want), len(wrapped), wrapped)
	}
	for i := range want {
		if wrapped[i].text != want[i] {
			t.Fatalf("line %d: got %q, want %q", i, wrapped[i].text, want[i])
		}
	}
}

func TestTuiStateParsesSummariesWaitsForStreamingBlock(t *testing.T) {
	// A fragment whose closing line is still streaming must be kept, not
	// skipped: no complete block exists beyond it, so the parser waits
	// for the fragment's own closing line. See TheoryOfTUI.
	st := &tuiState{}
	st.write([]byte("<<黿鼍爩 <summary>\n- not yet complete"))
	if len(st.signals) != 0 {
		t.Fatalf("expected no signals while the block is incomplete, got %v", st.signals)
	}
	st.write([]byte("\n黿鼍爩\n"))
	if len(st.signals) != 2 || st.signals[0].text != "- not yet complete" {
		t.Fatalf("unexpected signals: %v", st.signals)
	}
}

func TestTuiStateParsesSummariesKeepsPartialMarker(t *testing.T) {
	// A block opener split across chunk boundaries at any byte position
	// must be retained until the next chunk completes it. Without
	// retention, a "<<" or a lone "<" at the end of a chunk is discarded
	// and the summary it opens is lost. See TheoryOfTUI.
	t.Run("PartialDoubleLeftChevrons", func(t *testing.T) {
		st := &tuiState{}
		st.write([]byte("prose\n<<"))
		st.write([]byte("黿鼍爩 <summary>\n- done\n黿鼍爩\n"))
		if len(st.signals) != 2 || st.signals[0].text != "- done" {
			t.Fatalf("unexpected signals: %v", st.signals)
		}
	})
	t.Run("SingleLeftChevron", func(t *testing.T) {
		st := &tuiState{}
		st.write([]byte("prose\n<"))
		st.write([]byte("<黿鼍爩 <summary>\n- done\n黿鼍爩\n"))
		if len(st.signals) != 2 || st.signals[0].text != "- done" {
			t.Fatalf("unexpected signals: %v", st.signals)
		}
	})
}

func TestTuiStateCollectsFinishSignals(t *testing.T) {
	st := &tuiState{}
	st.finishReason("stop")
	if len(st.signals) != 1 {
		t.Fatalf("expected 1 finish signal, got %v", st.signals)
	}
	if st.signals[0].text != "[Finish: stop]" {
		t.Fatalf("unexpected signal: %q", st.signals[0].text)
	}
}

func TestTuiStateSignalsCombineSummaryAndFinish(t *testing.T) {
	// The Round tab shows both round completion signals: the summary
	// block body and the finish reason, in order. Finish reasons are
	// read from the generation state. See TheoryOfTUI.
	st := &tuiState{}
	st.write([]byte("<<徕珑龘 <summary>\n- done\n徕珑龘\n"))
	st.finishReason("stop")
	if len(st.signals) != 3 {
		t.Fatalf("expected 3 signal lines, got %v", st.signals)
	}
	if st.signals[0].text != "- done" || st.signals[1].text != "" || st.signals[2].text != "[Finish: stop]" {
		t.Fatalf("unexpected signals: %v", st.signals)
	}
}

func TestTuiStateFinishSignalExpandsRoundTab(t *testing.T) {
	tui := &TUI{}
	tui.expanded = [3]bool{true, false, false}
	tui.hasContent = [3]bool{true, false, false}
	tui.focus = 0
	tui.finishReason("stop")
	if !tui.expanded[1] {
		t.Fatal("round tab should auto-expand on a finish reason")
	}
	if tui.focus != 0 {
		t.Fatalf("auto-expand must not change an established focus, got %d", tui.focus)
	}
	if len(tui.signals) != 1 {
		t.Fatalf("expected the finish signal, got %v", tui.signals)
	}
}

func TestTuiStateRoundTabTitle(t *testing.T) {
	// The tab shows round completion signals, not only summaries, so its
	// title is "Round". See TheoryOfTUI.
	if tabNames[1] != "Round" {
		t.Fatalf("expected the round tab title, got %q", tabNames[1])
	}
}

func TestTUIPanelShowsTailOfWrappedContent(t *testing.T) {
	// The panel must reach the tail of wrapped content: the scroll
	// offset is clamped against the wrapped display-line count, so at
	// the maximum offset the last display line lands on the scroll
	// view's last row. Under raw-line clamping the tail was unreachable
	// once wrapping multiplied the display rows. See TheoryOfTUI.
	var src []string
	for i := 0; i < 20; i++ {
		src = append(src, strings.Repeat("x", 20))
	}
	src = append(src, "THE-END")
	display := plainDisplay(taiui.WrapLines(src, 9))
	last := display[len(display)-1].text
	if last != "THE-END" {
		t.Fatalf("expected the last display line to be THE-END, got %q", last)
	}

	tui := &TUI{}
	// The panel is 10 rows tall: the one-row label strip leaves 9 rows
	// for the scroll view; the tail offset is len(display) - 9.
	paneHeight := 9
	element := tui.panel(0, taiui.Box{Top: 0, Left: 0, Bottom: 10, Right: 12}, display, scrollClamp(1<<30, len(display), paneHeight), false)

	screen := &panelTestScreen{width: 12, height: 10}
	taiui.Render(taiui.NewBaseScope(func() taiui.Root {
		return taiui.Root{Element: element}
	}), screen)

	if len(screen.frames) == 0 {
		t.Fatal("expected a rendered frame")
	}
	frame := screen.frames[len(screen.frames)-1]
	// At the maximum offset the last display line lands on the scroll
	// view's last row: screen row 9, column 0 (the label strip occupies
	// row 0, and the scroll view spans rows 1..9).
	if cell := frame.Cells[9*frame.Width+0]; cell.Rune != 'T' {
		t.Fatalf("expected THE-END at the pane's bottom row (9,0), got %v", cell.Rune)
	}
}

func TestTUIPanelBackgroundColors(t *testing.T) {
	// The TUI uses exactly two background colors: dark blue for the
	// unfocused tab and dark gray for the focused tab. The label strip
	// shares the tab's background, so the focus state is the only
	// differentiator. See TheoryOfTUI.
	renderPanel := func(focus bool) taiui.Frame {
		tui := &TUI{}
		element := tui.panel(0, taiui.Box{Top: 0, Left: 0, Bottom: 4, Right: 12}, plainDisplay([]string{"content"}), 0, focus)
		screen := &panelTestScreen{width: 12, height: 4}
		taiui.Render(taiui.NewBaseScope(func() taiui.Root {
			return taiui.Root{Element: element}
		}), screen)
		if len(screen.frames) == 0 {
			t.Fatal("expected a rendered frame")
		}
		return screen.frames[len(screen.frames)-1]
	}

	cases := []struct {
		focus bool
		want  [3]int32
	}{
		{false, [3]int32{0x0a, 0x14, 0x28}}, // dark blue
		{true, [3]int32{0x2e, 0x2e, 0x2e}},  // dark gray
	}
	for _, tc := range cases {
		frame := renderPanel(tc.focus)
		// Row 0 is the label strip and row 1 is the scroll content; both
		// share the tab's background color.
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
	go readTUIKeys(strings.NewReader("\x1b[Aq\x1b[5~\x1b[6~"), ch)
	var got []string
	for len(got) < 4 {
		select {
		case k := <-ch:
			got = append(got, k)
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

func TestReadTUIKeysTabAndSplit(t *testing.T) {
	ch := make(chan string, 10)
	go readTUIKeys(strings.NewReader("123sS"), ch)
	var got []string
	for len(got) < 5 {
		select {
		case k := <-ch:
			got = append(got, k)
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

func TestTUINumberKeySemantics(t *testing.T) {
	tui := &TUI{}
	tui.expanded = [3]bool{true, true, false}
	tui.hasContent = [3]bool{true, true, false}
	tui.focus = 0
	tui.lastFocus = [3]int{0, -1, -1}
	tui.focusOrder = 1

	// Focused tab: pressing its key collapses it and moves the focus to
	// the expanded tab that was last focused. Tab 1 was never focused
	// (lastFocus -1), so it is the only expanded tab and takes the focus.
	tui.toggleTab(0)
	if tui.expanded[0] {
		t.Fatal("focused output tab should be collapsed")
	}
	if tui.focus != 1 {
		t.Fatalf("focus should move to the last-focused expanded tab, got %d", tui.focus)
	}

	// Collapsed tab: pressing its key expands it and switches the focus to it.
	tui.toggleTab(2)
	if !tui.expanded[2] {
		t.Fatal("collapsed logs tab should expand")
	}
	if tui.focus != 2 {
		t.Fatalf("focus should switch to the logs tab, got %d", tui.focus)
	}

	// Expanded non-focused tab: pressing its key switches the focus to it
	// without collapsing it.
	tui.toggleTab(1)
	if !tui.expanded[1] {
		t.Fatal("expanded round tab must stay expanded")
	}
	if tui.focus != 1 {
		t.Fatalf("focus should switch to the round tab, got %d", tui.focus)
	}

	// Focused tab again: pressing its key collapses it, leaving the other
	// expanded tab focused. Tab 2 was focused most recently (lastFocus 1),
	// so the focus returns to it.
	tui.toggleTab(1)
	if tui.expanded[1] {
		t.Fatal("focused round tab should collapse")
	}
	if tui.focus != 2 {
		t.Fatalf("focus should move to the last-focused expanded tab, got %d", tui.focus)
	}

	// Collapsing the last expanded tab clears the focus.
	tui.toggleTab(2)
	if tui.expanded[2] {
		t.Fatal("focused logs tab should collapse")
	}
	if tui.focus != -1 {
		t.Fatalf("focus should be -1 when no tab is expanded, got %d", tui.focus)
	}
}

func TestTuiStateCollapseFocusLastExpanded(t *testing.T) {
	// When a focused tab collapses, the focus moves to the expanded tab
	// that was last focused. Tabs that were never focused tie-break by
	// index order. See TheoryOfTUI.
	tui := &TUI{}
	tui.expanded = [3]bool{true, true, true}
	tui.hasContent = [3]bool{true, true, true}
	tui.focus = 0
	tui.lastFocus = [3]int{0, -1, -1}
	tui.focusOrder = 1

	// Focus tab 2, then tab 1: tab 1 is the most recently focused.
	tui.toggleTab(2)
	tui.toggleTab(1)
	if tui.focus != 1 {
		t.Fatalf("expected focus on tab 1, got %d", tui.focus)
	}

	// Collapse tab 1: focus returns to tab 2, the last-focused expanded
	// tab.
	tui.toggleTab(1)
	if tui.expanded[1] {
		t.Fatal("tab 1 should be collapsed")
	}
	if tui.focus != 2 {
		t.Fatalf("expected focus to return to tab 2, got %d", tui.focus)
	}
}

func TestTUINumberKeySwitchKeepsFollowState(t *testing.T) {
	tui := &TUI{}
	tui.expanded = [3]bool{true, true, false}
	tui.hasContent = [3]bool{true, true, false}
	tui.focus = 0
	tui.follow = [3]bool{false, true, false}

	// Switching to an already-expanded non-focused tab keeps its view: the
	// tab's follow state is untouched, so a scrolled position survives.
	tui.toggleTab(1)
	if tui.focus != 1 {
		t.Fatalf("focus should switch to the round tab, got %d", tui.focus)
	}
	if !tui.follow[1] {
		t.Fatal("switching to an expanded tab must keep its follow state")
	}

	// Collapsing and re-expanding the focused tab resumes following the
	// live tail.
	tui.toggleTab(1)
	if tui.expanded[1] {
		t.Fatal("focused round tab should collapse")
	}
	tui.toggleTab(1)
	if !tui.expanded[1] {
		t.Fatal("collapsed round tab should re-expand")
	}
	if !tui.follow[1] {
		t.Fatal("re-expanding a collapsed tab must resume following")
	}
}

func TestTUICycleFocusSkipsCollapsedTabs(t *testing.T) {
	tui := &TUI{}
	tui.expanded = [3]bool{true, false, true}
	tui.focus = 0
	tui.cycleFocus()
	if tui.focus != 2 {
		t.Fatalf("focus should skip the collapsed round tab and land on logs, got %d", tui.focus)
	}
	tui.cycleFocus()
	if tui.focus != 0 {
		t.Fatalf("focus should wrap to the output tab, got %d", tui.focus)
	}
	tui.expanded = [3]bool{false, false, false}
	tui.cycleFocus()
	if tui.focus != -1 {
		t.Fatalf("focus should be -1 with no expanded tabs, got %d", tui.focus)
	}
}

func TestScrollClamp(t *testing.T) {
	// The maximum offset is displayLines - paneHeight: at the maximum
	// offset the last display line lands on the pane's last row. An
	// offset beyond the content clamps to the maximum, a negative
	// offset clamps to 0, and the tail sentinel (1<<30) clamps to the
	// last row. See TheoryOfTUI.
	if got := scrollClamp(0, 10, 3); got != 0 {
		t.Fatalf("offset 0 should be unchanged, got %d", got)
	}
	if got := scrollClamp(7, 10, 3); got != 7 {
		t.Fatalf("offset 7 (the max) should be unchanged, got %d", got)
	}
	if got := scrollClamp(8, 10, 3); got != 7 {
		t.Fatalf("offset 8 should clamp to 7, got %d", got)
	}
	if got := scrollClamp(100, 10, 3); got != 7 {
		t.Fatalf("offset 100 should clamp to 7, got %d", got)
	}
	if got := scrollClamp(1<<30, 10, 3); got != 7 {
		t.Fatalf("tail sentinel should clamp to 7, got %d", got)
	}
	if got := scrollClamp(-5, 10, 3); got != 0 {
		t.Fatalf("negative offset should clamp to 0, got %d", got)
	}
	if got := scrollClamp(0, 2, 3); got != 0 {
		t.Fatalf("fitted content should clamp to 0, got %d", got)
	}
	if got := scrollClamp(1<<30, 2, 3); got != 0 {
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
	// TUI panels receive display lines pre-wrapped at the pane's visible
	// width (the tab's width minus the scrollbar column), so a long
	// source line occupies several display rows. The panel's Text
	// renders the pre-wrapped lines below the one-row label strip; the
	// first display rows are visible.
	tui := &TUI{}
	src := strings.Repeat("abcdefghijklmnopqrstuvwxyz", 1)
	lines := plainDisplay(taiui.WrapLines([]string{src, src, src}, 11))
	element := tui.panel(0, taiui.Box{Top: 0, Left: 0, Bottom: 6, Right: 12}, lines, 0, false)

	screen := &panelTestScreen{width: 12, height: 6}
	taiui.Render(taiui.NewBaseScope(func() taiui.Root {
		return taiui.Root{Element: element}
	}), screen)

	if len(screen.frames) == 0 {
		t.Fatal("expected a rendered frame")
	}
	frame := screen.frames[len(screen.frames)-1]
	// The pane is 12 columns wide with a scrollbar; the visible content
	// area is 11 columns, so each 26-character source line wraps into
	// three display rows. The first display row holds the first 11
	// characters, the second row the next 11. The label strip occupies
	// row 0, so the content starts at row 1.
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
	// The scrollbar is hidden while the pane follows the tail: at the
	// latest position there is nothing left to scroll toward, so the
	// thumb would only add visual noise and waste a column. Scrolling
	// away from the tail brings the scrollbar back. See TheoryOfTUI.
	var lines []string
	for i := 0; i < 40; i++ {
		lines = append(lines, fmt.Sprintf("line %02d", i))
	}

	renderPanel := func(follow bool) taiui.Frame {
		tui := &TUI{}
		tui.follow = [3]bool{follow, false, false}
		element := tui.panel(0, taiui.Box{Top: 0, Left: 0, Bottom: 10, Right: 80}, plainDisplay(lines), 0, false)
		screen := &panelTestScreen{width: 80, height: 10}
		taiui.Render(taiui.NewBaseScope(func() taiui.Root {
			return taiui.Root{Element: element}
		}), screen)
		if len(screen.frames) == 0 {
			t.Fatal("expected a rendered frame")
		}
		return screen.frames[len(screen.frames)-1]
	}

	// While following, no scrollbar thumb appears at the right edge.
	following := renderPanel(true)
	rightmost := following.Width - 1
	for y := 0; y < following.Height; y++ {
		if cell := following.Cells[y*following.Width+rightmost]; cell.Rune == '█' {
			t.Fatalf("expected no scrollbar thumb while following, got one at (%d,%d)", rightmost, y)
		}
	}

	// When scrolled away from the tail, the scrollbar thumb appears.
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
	// Stacked layout, 2 expanded tabs and 1 collapsed tab on a 40-row
	// screen: the collapsed tab takes one row at the bottom, and the
	// expanded tabs share the remaining 39 rows. The scroll offset clamp
	// must match the actual scroll view height (the panel height minus
	// the one-row label strip). See TheoryOfTUI.
	boxes := computeTabBoxes(false, [3]bool{true, true, false}, -1, 80, 40)
	paneHeight := max(boxes[0].Height()-1, 1)
	if paneHeight != 18 {
		t.Fatalf("expected an 18-row scroll view, got %d", paneHeight)
	}
	const displayLines = 100
	if got := scrollClamp(1<<30, displayLines, paneHeight); got != displayLines-18 {
		t.Fatalf("expected the tail offset %d, got %d", displayLines-18, got)
	}

	// Side-by-side layout: the panel spans the full height, so the
	// scroll view is height - 1.
	boxes = computeTabBoxes(true, [3]bool{true, true, false}, -1, 80, 40)
	if paneHeight := max(boxes[0].Height()-1, 1); paneHeight != 39 {
		t.Fatalf("expected a 39-row scroll view, got %d", paneHeight)
	}
}

func TestTUISticksToTail(t *testing.T) {
	tui := &TUI{}
	tui.expanded = [3]bool{true, false, false}
	tui.hasContent = [3]bool{true, false, false}
	tui.focus = 0
	tui.follow = [3]bool{true, false, false}
	tui.splitVertical = false
	tui.screen = taiui.NewTerminalScreen(&strings.Builder{}, 80, 10)
	tui.width = 80
	tui.height = 10

	var sb strings.Builder
	for i := 0; i < 20; i++ {
		fmt.Fprintf(&sb, "line %d\n", i)
	}
	tui.write([]byte(sb.String()))
	tui.render()
	// The output tab spans the full width (80 columns) minus the
	// scrollbar column = 79 content columns, and 8 rows (10 minus the two
	// collapsed tabs' rows). The scroll view is 7 rows (8 minus the
	// one-row label strip).
	contentWidth := 79
	display := wrapTabLines(tui.outputLinesForRender(), contentWidth)
	want := len(display) - 7
	if want < 0 {
		want = 0
	}
	if tui.topLeft != want {
		t.Fatalf("expected topLeft %d, got %d", want, tui.topLeft)
	}
	if !tui.follow[0] {
		t.Fatal("expected follow on the output tab")
	}

	tui.write([]byte("new line\nanother line\n"))
	tui.render()
	display = wrapTabLines(tui.outputLinesForRender(), contentWidth)
	want = len(display) - 7
	if want < 0 {
		want = 0
	}
	if tui.topLeft != want {
		t.Fatalf("expected topLeft %d after new output, got %d", want, tui.topLeft)
	}
	if !tui.follow[0] {
		t.Fatal("expected follow to persist on the output tab")
	}
}

func TestTUIReopenResumesFollow(t *testing.T) {
	tui := &TUI{}
	tui.expanded = [3]bool{true, false, false}
	tui.hasContent = [3]bool{true, false, false}
	tui.focus = 0
	tui.follow = [3]bool{true, false, false}
	tui.splitVertical = false
	tui.screen = taiui.NewTerminalScreen(&strings.Builder{}, 80, 10)
	tui.width = 80
	tui.height = 10

	var sb strings.Builder
	for i := 0; i < 10; i++ {
		fmt.Fprintf(&sb, "line %d\n", i)
	}
	tui.write([]byte(sb.String()))
	tui.render()

	// Simulate the user scrolling away from the tail: follow must clear
	// and the view must leave the latest row.
	tui.scroll(-1)
	if tui.follow[0] {
		t.Fatal("expected follow false after scrolling away")
	}

	// Collapse and re-expand the output tab.
	tui.toggleTab(0)
	if tui.expanded[0] {
		t.Fatal("expected output tab collapsed")
	}
	if tui.focus != -1 {
		t.Fatalf("expected focus -1 with no expanded tabs, got %d", tui.focus)
	}
	tui.toggleTab(0)
	if !tui.expanded[0] {
		t.Fatal("expected output tab re-expanded")
	}
	if tui.focus != 0 {
		t.Fatalf("expected focus 0 after re-expand, got %d", tui.focus)
	}
	// Re-expanding must resume following the live tail.
	if !tui.follow[0] {
		t.Fatal("expected follow true after re-expand")
	}

	// New content arriving after the re-expand must move the view to the
	// new tail, so the pane shows the latest lines.
	tui.write([]byte("line 10\nline 11\n"))
	tui.render()
	contentWidth := 79 // 80 columns minus the scrollbar column
	display := wrapTabLines(tui.outputLinesForRender(), contentWidth)
	want := len(display) - 7 // 8 rows minus the one-row label strip
	if want < 0 {
		want = 0
	}
	if tui.topLeft != want {
		t.Fatalf("expected topLeft %d after new content, got %d", want, tui.topLeft)
	}
	if !tui.follow[0] {
		t.Fatal("expected follow to persist after new content")
	}
}

func TestTUIStopsFollowingWhenScrolledAway(t *testing.T) {
	tui := &TUI{}
	tui.expanded = [3]bool{true, false, false}
	tui.hasContent = [3]bool{true, false, false}
	tui.focus = 0
	tui.follow = [3]bool{true, false, false}
	tui.splitVertical = false
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
	display := wrapTabLines(tui.outputLinesForRender(), contentWidth)
	initialMax := len(display) - 7
	if initialMax < 0 {
		initialMax = 0
	}

	tui.scroll(-3)
	if tui.topLeft != initialMax-3 {
		t.Fatalf("expected topLeft %d after scrolling up, got %d", initialMax-3, tui.topLeft)
	}
	if tui.follow[0] {
		t.Fatal("expected follow cleared after scrolling away")
	}

	tui.write([]byte("more\n"))
	tui.render()
	if tui.topLeft != initialMax-3 {
		t.Fatalf("expected view to stay at %d while scrolled away, got %d", initialMax-3, tui.topLeft)
	}
	if tui.follow[0] {
		t.Fatal("expected follow to stay cleared while scrolled away")
	}

	tui.scrollTo(1 << 30)
	if !tui.follow[0] {
		t.Fatal("expected follow restored at the end")
	}
	tui.write([]byte("tail\n"))
	tui.render()
	display = wrapTabLines(tui.outputLinesForRender(), contentWidth)
	want := len(display) - 7
	if want < 0 {
		want = 0
	}
	if tui.topLeft != want {
		t.Fatalf("expected topLeft %d after resuming follow, got %d", want, tui.topLeft)
	}
	if !tui.follow[0] {
		t.Fatal("expected follow to persist after resuming")
	}
}

func TestTUIDownAtEndKeepsFollow(t *testing.T) {
	tui := &TUI{}
	tui.expanded = [3]bool{true, false, false}
	tui.hasContent = [3]bool{true, false, false}
	tui.focus = 0
	tui.follow = [3]bool{true, false, false}
	tui.splitVertical = false
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
	if !tui.follow[0] {
		t.Fatal("down at the latest row must keep following")
	}
	if tui.topLeft != tui.maxOffsets[0] {
		t.Fatalf("expected topLeft at max offset %d, got %d", tui.maxOffsets[0], tui.topLeft)
	}
}

func TestTabPanelBoxWeighted(t *testing.T) {
	// Two expanded tabs and one collapsed tab on a 90-column screen: the
	// collapsed tab takes one column at the right edge, and the expanded
	// tabs share the remaining 89 columns. The focused tab has weight 2
	// and the other weight 1, so the total weight is 3: the focused tab
	// gets 2/3 of 89 = 59 columns (integer division), the other gets the
	// remaining 30.
	boxes := computeTabBoxes(true, [3]bool{true, true, false}, 0, 90, 40)
	if boxes[0].Left != 0 || boxes[0].Right != 59 || boxes[0].Top != 0 || boxes[0].Bottom != 40 {
		t.Fatalf("unexpected focused panel box: %+v", boxes[0])
	}
	if boxes[1].Left != 59 || boxes[1].Right != 89 {
		t.Fatalf("unexpected non-focused panel box: %+v", boxes[1])
	}
	if boxes[2].Left != 89 || boxes[2].Right != 90 {
		t.Fatalf("unexpected collapsed panel box: %+v", boxes[2])
	}

	// Focusing the second tab swaps the proportions.
	boxes = computeTabBoxes(true, [3]bool{true, true, false}, 1, 90, 40)
	if boxes[0].Left != 0 || boxes[0].Right != 29 {
		t.Fatalf("unexpected non-focused panel box: %+v", boxes[0])
	}
	if boxes[1].Left != 29 || boxes[1].Right != 89 {
		t.Fatalf("unexpected focused panel box: %+v", boxes[1])
	}

	// With no focused tab, every expanded tab has weight 1 and the space
	// is shared equally: each of two expanded tabs on 89 columns gets 44
	// (integer division), the last absorbs the remainder.
	boxes = computeTabBoxes(true, [3]bool{true, true, false}, -1, 90, 40)
	if boxes[0].Left != 0 || boxes[0].Right != 44 {
		t.Fatalf("unexpected equal-share panel box: %+v", boxes[0])
	}
	if boxes[1].Left != 44 || boxes[1].Right != 89 {
		t.Fatalf("unexpected equal-share panel box: %+v", boxes[1])
	}

	// Horizontal split applies the same weights along the height.
	boxes = computeTabBoxes(false, [3]bool{true, true, false}, 0, 80, 45)
	if boxes[0].Top != 0 || boxes[0].Bottom != 29 {
		t.Fatalf("unexpected focused panel box: %+v", boxes[0])
	}
	if boxes[1].Top != 29 || boxes[1].Bottom != 44 {
		t.Fatalf("unexpected non-focused panel box: %+v", boxes[1])
	}
	if boxes[2].Top != 44 || boxes[2].Bottom != 45 {
		t.Fatalf("unexpected collapsed panel box: %+v", boxes[2])
	}

	// Three expanded tabs with the middle focused: weights [1,2,1] over
	// total 4; the last tab absorbs the rounding remainder.
	boxes = computeTabBoxes(true, [3]bool{true, true, true}, 1, 90, 24)
	if boxes[0].Left != 0 || boxes[0].Right != 22 {
		t.Fatalf("unexpected first panel box: %+v", boxes[0])
	}
	if boxes[1].Left != 22 || boxes[1].Right != 67 {
		t.Fatalf("unexpected focused middle panel box: %+v", boxes[1])
	}
	if boxes[2].Left != 67 || boxes[2].Right != 90 {
		t.Fatalf("unexpected last panel box: %+v", boxes[2])
	}
}

func TestTabPanelBoxCollapsedInPlace(t *testing.T) {
	// A collapsed tab must stay in its original position, not be pushed
	// to the edge. In vertical split, tab 1 (Round) collapsed between
	// expanded tabs 0 and 2 keeps its middle position as a one-column
	// strip. See TheoryOfTUI.
	boxes := computeTabBoxes(true, [3]bool{true, false, true}, 0, 90, 40)
	if boxes[0].Left != 0 || boxes[0].Right != 59 {
		t.Fatalf("unexpected output panel box: %+v", boxes[0])
	}
	if boxes[1].Left != 59 || boxes[1].Right != 60 {
		t.Fatalf("collapsed round tab must stay in the middle, got %+v", boxes[1])
	}
	if boxes[2].Left != 60 || boxes[2].Right != 90 {
		t.Fatalf("unexpected logs panel box: %+v", boxes[2])
	}

	// Horizontal split: the collapsed tab stays in its original row
	// position.
	boxes = computeTabBoxes(false, [3]bool{true, false, true}, 0, 80, 45)
	if boxes[0].Top != 0 || boxes[0].Bottom != 29 {
		t.Fatalf("unexpected output panel box: %+v", boxes[0])
	}
	if boxes[1].Top != 29 || boxes[1].Bottom != 30 {
		t.Fatalf("collapsed round tab must stay in the middle, got %+v", boxes[1])
	}
	if boxes[2].Top != 30 || boxes[2].Bottom != 45 {
		t.Fatalf("unexpected logs panel box: %+v", boxes[2])
	}
}

func TestTabPanelBoxCollapsedFirstAndLast(t *testing.T) {
	// Collapsed tabs at the edges stay at the edges, and the expanded
	// middle tab absorbs the remaining space.
	boxes := computeTabBoxes(true, [3]bool{false, true, false}, 1, 90, 40)
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
	// A collapsed tab renders as a thin strip showing the tab's key and
	// title. In horizontal split the strip is one row tall and the label
	// is written horizontally; in vertical split the strip is one column
	// wide and the label is written vertically. See TheoryOfTUI.
	t.Run("Horizontal", func(t *testing.T) {
		tui := &TUI{}
		element := tui.collapsedPanel(0, taiui.Box{Top: 0, Left: 0, Bottom: 1, Right: 12}, false)
		screen := &panelTestScreen{width: 12, height: 1}
		taiui.Render(taiui.NewBaseScope(func() taiui.Root {
			return taiui.Root{Element: element}
		}), screen)
		if len(screen.frames) == 0 {
			t.Fatal("expected a rendered frame")
		}
		frame := screen.frames[len(screen.frames)-1]
		// The label "1 Output" is rendered with padding: "  1 Output  ".
		if cell := frame.Cells[2]; cell.Rune != '1' {
			t.Fatalf("expected '1' at (2,0), got %v", cell.Rune)
		}
		if cell := frame.Cells[4]; cell.Rune != 'O' {
			t.Fatalf("expected 'O' at (4,0), got %v", cell.Rune)
		}
	})

	t.Run("Vertical", func(t *testing.T) {
		tui := &TUI{}
		element := tui.collapsedPanel(0, taiui.Box{Top: 0, Left: 0, Bottom: 8, Right: 1}, false)
		screen := &panelTestScreen{width: 1, height: 8}
		taiui.Render(taiui.NewBaseScope(func() taiui.Root {
			return taiui.Root{Element: element}
		}), screen)
		if len(screen.frames) == 0 {
			t.Fatal("expected a rendered frame")
		}
		frame := screen.frames[len(screen.frames)-1]
		// The label "1 Output" is written vertically: '1' at row 0, the
		// space at row 1, 'O' at row 2.
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
	// When all tabs are collapsed, each takes one column (vertical
	// split) or one row (horizontal split). See TheoryOfTUI.
	boxes := computeTabBoxes(true, [3]bool{false, false, false}, -1, 80, 24)
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

	boxes = computeTabBoxes(false, [3]bool{false, false, false}, -1, 80, 24)
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
	tui := &TUI{}
	tui.focus = -1
	tui.write([]byte("model output\n"))
	if !tui.expanded[0] {
		t.Fatal("output tab should auto-expand on streamed output")
	}
	if tui.focus != 0 {
		t.Fatalf("expected focus on the output tab, got %d", tui.focus)
	}

	// A log record expands the Logs tab without changing the focus.
	tui.writeLogs([]byte("msg=\"log record\"\n"))
	if !tui.expanded[2] {
		t.Fatal("logs tab should auto-expand on log records")
	}
	if tui.focus != 0 {
		t.Fatalf("auto-expand must not change an established focus, got %d", tui.focus)
	}

	// A summary block expands the Round tab without changing the focus.
	tui.write([]byte("<<徕珑龘 <summary>\n- done\n徕珑龘\n"))
	if !tui.expanded[1] {
		t.Fatal("round tab should auto-expand on a summary block")
	}
	if tui.focus != 0 {
		t.Fatalf("auto-expand must not change an established focus, got %d", tui.focus)
	}
	if len(tui.signals) != 2 {
		t.Fatalf("expected the summary signals, got %v", tui.signals)
	}

	// A non-summary block does not expand the Round tab on its own.
	tui2 := &TUI{}
	tui2.expanded = [3]bool{true, false, false}
	tui2.hasContent = [3]bool{true, false, false}
	tui2.focus = 0
	tui2.write([]byte("<<龘靐齉 <change op=\"MODIFY\" target=\"Foo\" file-path=\"x.go\">\nfunc Foo() {}\n龘靐齉\n"))
	if tui2.expanded[1] {
		t.Fatal("round tab must not expand without a summary block or finish line")
	}
}

func TestTuiStateAutoExpandPreservesFocus(t *testing.T) {
	tui := &TUI{}
	tui.expanded = [3]bool{true, false, false}
	tui.hasContent = [3]bool{true, false, false}
	tui.focus = 0
	tui.writeLogs([]byte("msg=\"log record\"\n"))
	if !tui.expanded[2] {
		t.Fatal("logs tab should auto-expand on log records")
	}
	if tui.focus != 0 {
		t.Fatalf("focus must stay on the output tab, got %d", tui.focus)
	}
	if !tui.follow[2] {
		t.Fatal("auto-expanded tab should follow the tail")
	}
	// The newly auto-expanded tab is immediately navigable: tab cycles
	// to it from the current focus.
	tui.cycleFocus()
	if tui.focus != 2 {
		t.Fatalf("expected focus to cycle to the logs tab, got %d", tui.focus)
	}
}

func TestTuiStateEmptyWriteDoesNotExpandTabs(t *testing.T) {
	st := &tuiState{}
	st.focus = -1
	st.write(nil)
	st.writeLogs(nil)
	for i := 0; i < 3; i++ {
		if st.expanded[i] {
			t.Fatalf("tab %d must not expand on empty writes", i)
		}
	}
	if st.focus != -1 {
		t.Fatalf("expected no focus change on empty writes, got %d", st.focus)
	}
}

func TestTuiStateAutoExpandOnlyFirstContent(t *testing.T) {
	// A collapsed tab expands automatically the first time content for it
	// arrives, but not on subsequent arrivals after the user collapses
	// it. See TheoryOfTUI.
	tui := &TUI{}
	tui.focus = -1
	tui.write([]byte("first output\n"))
	if !tui.expanded[0] {
		t.Fatal("output tab should auto-expand on first content")
	}
	// Collapse the output tab.
	tui.toggleTab(0)
	if tui.expanded[0] {
		t.Fatal("output tab should be collapsed")
	}
	// New content must not re-expand it.
	tui.write([]byte("more output\n"))
	if tui.expanded[0] {
		t.Fatal("output tab must not re-expand on subsequent content")
	}
}

func TestWithTUIOutputObserver(t *testing.T) {
	// The TUI's output observer must be passed through
	// RunOptions.StateDecorators so the loop applies it to the
	// generation state. Without the wrapper, the model output and
	// finish reasons never reach the TUI. See TheoryOfTUI.
	tui := &TUI{}
	var gotOpts loops.RunOptions
	run := func(ctx context.Context, opts loops.RunOptions) (loops.Result, error) {
		gotOpts = opts
		return loops.Result{}, nil
	}
	wrapped := withTUIOutputObserver(run, tui)

	_, err := wrapped(context.Background(), loops.RunOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(gotOpts.StateDecorators) != 1 {
		t.Fatalf("expected 1 state decorator, got %d", len(gotOpts.StateDecorators))
	}

	// Apply the decorator to a state and append content: text streams
	// to the Output tab, thoughts are separated by blank lines and
	// colored distinctly, and finish reasons reach the Round tab.
	var state generators.State = generators.NewPrompts("", nil)
	state, err = gotOpts.StateDecorators[0](state).AppendContent(&generators.Content{
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
	state, err = state.AppendContent(&generators.Content{
		Role: generators.RoleLog,
		Parts: []generators.Part{
			generators.FinishReason("stop"),
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	tui.mu.Lock()
	defer tui.mu.Unlock()
	var sb strings.Builder
	for _, line := range tui.lines {
		sb.WriteString(line.text)
		sb.WriteString("\n")
	}
	output := sb.String()
	// The thought is separated from the surrounding text by blank lines,
	// with no "thinking"/"response" labels.
	for _, want := range []string{"model output", "deep thinking", "answer"} {
		if !strings.Contains(output, want) {
			t.Fatalf("expected %q in output, got %q", want, output)
		}
	}
	if !strings.Contains(output, "deep thinking\n\nanswer") {
		t.Fatalf("expected a blank line between the thought and the answer, got %q", output)
	}
	if strings.Contains(output, "\n thinking\n") || strings.Contains(output, "\n response\n") {
		t.Fatalf("expected no thinking/response labels in output, got %q", output)
	}
	if len(tui.signals) != 1 || tui.signals[0].text != "[Finish: stop]" {
		t.Fatalf("expected finish reason in round tab, got %v", tui.signals)
	}
}

func TestTUICaptureContentNotifies(t *testing.T) {
	// Model output captured via the state decorator must notify the
	// render loop. The decorator appends content directly to the
	// display buffers, bypassing the tuiWriter path that notifies;
	// without a notification the loop stays blocked on the update
	// channel and the output pane appears frozen until an input key
	// forces a re-render. See TheoryOfTUI.
	tui := &TUI{updateCh: make(chan struct{}, 1)}
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

func TestRoleColor(t *testing.T) {
	cases := []struct {
		role generators.Role
		want int32
	}{
		{generators.RoleUser, outputColorUser},
		{generators.RoleTool, outputColorTool},
		{generators.RoleSystem, outputColorSystem},
		{generators.RoleLog, outputColorLog},
		{generators.RoleModel, 0},
		{generators.RoleAssistant, 0},
	}
	for _, c := range cases {
		if got := roleColor(c.role); got != c.want {
			t.Fatalf("roleColor(%s) = %#x, want %#x", c.role, got, c.want)
		}
	}
}

func TestTUICaptureContentRoleColors(t *testing.T) {
	tui := &TUI{}
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
	// Each role switch inserts a blank line separator between the
	// colored sections.
	want := []struct {
		text  string
		color int32
	}{
		{"user", outputColorUser},
		{"", 0},
		{"model", 0},
		{"", 0},
		{"tool", outputColorTool},
		{"", 0},
		{"system", outputColorSystem},
		{"", 0},
		{"log", outputColorLog},
	}
	if len(tui.lines) != len(want) {
		t.Fatalf("expected %d lines, got %d: %v", len(want), len(tui.lines), tui.lines)
	}
	for i, w := range want {
		if tui.lines[i].text != w.text || tui.lines[i].color != w.color {
			t.Fatalf("line %d: got %+v, want text %q color %#x", i, tui.lines[i], w.text, w.color)
		}
	}
}

func TestTUICaptureContentThoughtColor(t *testing.T) {
	tui := &TUI{}
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
	// The thought is colored distinctly, and a blank line separates it
	// from the following answer text. No "thinking"/"response" labels
	// are emitted.
	want := []struct {
		text  string
		color int32
	}{
		{"thinking", outputColorThought},
		{"", 0},
		{"answer", 0},
	}
	if len(tui.lines) != len(want) {
		t.Fatalf("expected %d lines, got %d: %v", len(want), len(tui.lines), tui.lines)
	}
	for i, w := range want {
		if tui.lines[i].text != w.text || tui.lines[i].color != w.color {
			t.Fatalf("line %d: got %+v, want text %q color %#x", i, tui.lines[i], w.text, w.color)
		}
	}
}

func TestTuiStateFinishReasonColor(t *testing.T) {
	st := &tuiState{}
	st.finishReason("stop")
	if len(st.signals) != 1 {
		t.Fatalf("expected 1 finish signal, got %v", st.signals)
	}
	if st.signals[0].text != "[Finish: stop]" || st.signals[0].color != outputColorLog {
		t.Fatalf("unexpected signal: %+v", st.signals[0])
	}
}

func TestTuiStateSummaryLinesPlain(t *testing.T) {
	st := &tuiState{}
	st.write([]byte("<<徕珑龘 <summary>\n- done\n徕珑龘\n"))
	if len(st.signals) != 2 {
		t.Fatalf("expected 2 signal lines, got %v", st.signals)
	}
	if st.signals[0].text != "- done" || st.signals[0].color != 0 {
		t.Fatalf("unexpected signal: %+v", st.signals[0])
	}
}

func TestWrapTabLinesCarriesColors(t *testing.T) {
	lines := []outputLine{
		{text: "aaa bbb", color: outputColorUser},
		{text: "ccc", color: 0},
	}
	wrapped := wrapTabLines(lines, 5)
	want := []displayLine{
		{text: "aaa", color: outputColorUser},
		{text: "bbb", color: outputColorUser},
		{text: "ccc", color: 0},
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
	tui := &TUI{}
	lines := []displayLine{
		{text: "red", color: outputColorLog},
		{text: "plain", color: 0},
	}
	element := tui.panel(0, taiui.Box{Top: 0, Left: 0, Bottom: 3, Right: 10}, lines, 0, false)
	screen := &panelTestScreen{width: 10, height: 3}
	taiui.Render(taiui.NewBaseScope(func() taiui.Root {
		return taiui.Root{Element: element}
	}), screen)
	if len(screen.frames) == 0 {
		t.Fatal("expected a rendered frame")
	}
	frame := screen.frames[len(screen.frames)-1]
	// Row 0 is the label strip; content starts at row 1.
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
	// Text colors must use the ANSI 16 palette, never true-color RGB:
	// the rendered foreground is a PaletteColor, not an RGB color.
	// Backgrounds are exempt and keep true-color hex values.
	// See TheoryOfTUI.
	lines := []displayLine{
		{text: "u", color: outputColorUser},
		{text: "t", color: outputColorTool},
		{text: "s", color: outputColorSystem},
		{text: "l", color: outputColorLog},
		{text: "m", color: outputColorThought},
	}
	element := coloredText(lines, taiui.Box{Top: 0, Left: 0, Bottom: 5, Right: 40})
	screen := &panelTestScreen{width: 40, height: 5}
	taiui.Render(taiui.NewBaseScope(func() taiui.Root {
		return taiui.Root{Element: element}
	}), screen)
	if len(screen.frames) == 0 {
		t.Fatal("expected a rendered frame")
	}
	frame := screen.frames[len(screen.frames)-1]
	want := []int32{outputColorUser, outputColorTool, outputColorSystem, outputColorLog, outputColorThought}
	for i, c := range want {
		cell := frame.Cells[i*frame.Width]
		if !cell.Set {
			t.Fatalf("expected row %d to be painted", i)
		}
		fg := cell.Style.Fg()
		if fg&color.IsRGB != 0 {
			t.Fatalf("color %d must be a palette color, not true-color RGB", i)
		}
		if got := int(fg & 0xff); got != int(c) {
			t.Fatalf("color %d: expected ANSI 16 palette index %d, got %d", i, int(c), got)
		}
	}
}
