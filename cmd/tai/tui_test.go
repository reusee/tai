package main

import (
	"context"
	"fmt"
	"iter"
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v3/color"
	"github.com/reusee/tai/flags"
	"github.com/reusee/tai/generators"
	"github.com/reusee/tai/loops"
	"github.com/reusee/tai/taiui"
)

// newTUIForTest constructs a TUI with the display state initialized for
// tests: small line buffers, no terminal attached, and default tabs and
// scrolls. Tests that render set t.screen, t.width, and t.height before
// calling render(); rendering itself forks the current state values into a
// fresh dscope view scope. See TheoryOfTUI.
func newTUIForTest() *TUI {
	return &TUI{
		output:  taiui.NewLineBuffer(0),
		logs:    taiui.NewStringBuffer(0),
		tabs:    taiui.NewTabs(3),
		scrolls: [3]taiui.ScrollState{},
	}
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

func TestTuiStateWriteLines(t *testing.T) {
	tui := newTUIForTest()
	tui.write([]byte("hello\nworld\n"))
	lines := tui.output.Lines()
	if len(lines) != 2 || lines[0].Text != "hello" || lines[1].Text != "world" {
		t.Fatalf("unexpected lines: %v", lines)
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
	if label, highlight := outputTabLabel(tui.finished, tui.generating); label != "Output" || highlight {
		t.Fatalf("expected plain Output label before any activity, got label %q highlight %v", label, highlight)
	}
	tui.writeLogs([]byte("level=INFO msg=generating name=model\n"))
	if !tui.generating {
		t.Fatal("expected generating after the generating log")
	}
	if label, highlight := outputTabLabel(tui.finished, tui.generating); label != "Output (generating...)" || !highlight {
		t.Fatalf("expected generating hint with highlight, got label %q highlight %v", label, highlight)
	}
	tui.finishReason("stop")
	if tui.generating {
		t.Fatal("expected not generating after the finish reason")
	}
	if label, highlight := outputTabLabel(tui.finished, tui.generating); label != "Output" || highlight {
		t.Fatalf("expected plain Output label after the finish reason, got label %q highlight %v", label, highlight)
	}
	tui.finished = true
	if label, highlight := outputTabLabel(tui.finished, tui.generating); label != "Output (done)" || highlight {
		t.Fatalf("expected done hint without highlight, got label %q highlight %v", label, highlight)
	}
}

func TestTuiStateRequestingLogsWrite(t *testing.T) {
	tui := newTUIForTest()
	tui.writeLogs([]byte("msg=\"generating\"\n"))
	if !tui.generating {
		t.Fatal("expected generating after log write")
	}
	if label, highlight := outputTabLabel(tui.finished, tui.generating); label != "Output (generating...)" || !highlight {
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
	tui.finishReason("stop")
	if tui.generating {
		t.Fatal("expected not generating after the finish reason")
	}
	if label, _ := outputTabLabel(tui.finished, tui.generating); label != "Output" {
		t.Fatalf("expected plain Output label after the finish reason, got %q", label)
	}
}

func TestTUIOutputTabLabel(t *testing.T) {
	tui := newTUIForTest()
	if label, highlight := outputTabLabel(tui.finished, tui.generating); label != "Output" || highlight {
		t.Fatalf("expected plain Output label, got label %q highlight %v", label, highlight)
	}
	tui.writeLogs([]byte("level=INFO msg=generating name=model\n"))
	if label, highlight := outputTabLabel(tui.finished, tui.generating); label != "Output (generating...)" || !highlight {
		t.Fatalf("expected generating hint with highlight, got label %q highlight %v", label, highlight)
	}
	tui.finished = true
	if label, highlight := outputTabLabel(tui.finished, tui.generating); label != "Output (done)" || highlight {
		t.Fatalf("expected done hint without highlight, got label %q highlight %v", label, highlight)
	}
}

func TestTUIPanelTitleHighlightedDuringRequest(t *testing.T) {
	renderTitle := func(tui *TUI, focus bool) taiui.Frame {
		label, highlight := outputTabLabel(tui.finished, tui.generating)
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

func TestTuiStateParsesSummaries(t *testing.T) {
	tui := newTUIForTest()
	tui.write([]byte("<<徕珑龘 <summary>\n- one\n- two\n徕珑龘\n"))
	if len(tui.signals) != 3 {
		t.Fatalf("expected 3 signal lines, got %v", tui.signals)
	}
	if tui.signals[0].Text != "- one" || tui.signals[1].Text != "- two" || tui.signals[2].Text != "" {
		t.Fatalf("unexpected signals: %v", tui.signals)
	}
}

func TestTuiStateParsesSummariesAcrossChunks(t *testing.T) {
	tui := newTUIForTest()
	tui.write([]byte("<<徕珑龘 <summary>\n- one\n- tw"))
	tui.write([]byte("o\n徕珑龘\n"))
	if len(tui.signals) != 3 {
		t.Fatalf("expected 3 signal lines, got %v", tui.signals)
	}
	if tui.signals[0].Text != "- one" || tui.signals[1].Text != "- two" {
		t.Fatalf("unexpected signals: %v", tui.signals)
	}
}

func TestTuiStateIgnoresOtherBlocks(t *testing.T) {
	tui := newTUIForTest()
	text := "<<龘靐齉 <change op=\"MODIFY\" target=\"Foo\" file-path=\"x.go\">\nfunc Foo() {}\n龘靐齉\n" +
		"<<徕珑龘 <summary>\n- s\n徕珑龘\n"
	tui.write([]byte(text))
	if len(tui.signals) != 2 || tui.signals[0].Text != "- s" || tui.signals[1].Text != "" {
		t.Fatalf("unexpected signals: %v", tui.signals)
	}
}

func TestTuiStateParsesSummariesSkipsTruncatedFragment(t *testing.T) {
	tui := newTUIForTest()
	tui.write([]byte("<<徕珑龘 <change op=\"MODIFY\" target=\"Foo\" file-path=\"/x.go\">\nfunc Foo() {\n"))
	tui.write([]byte("round 2 output\n<<黿鼍爩 <summary>\n- done\n黿鼍爩\n"))
	if len(tui.signals) != 2 {
		t.Fatalf("expected 2 signal lines, got %v", tui.signals)
	}
	if tui.signals[0].Text != "- done" || tui.signals[1].Text != "" {
		t.Fatalf("unexpected signals: %v", tui.signals)
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

func TestTuiStateParsesSummariesWaitsForStreamingBlock(t *testing.T) {
	tui := newTUIForTest()
	tui.write([]byte("<<黿鼍爩 <summary>\n- not yet complete"))
	if len(tui.signals) != 0 {
		t.Fatalf("expected no signals while the block is incomplete, got %v", tui.signals)
	}
	tui.write([]byte("\n黿鼍爩\n"))
	if len(tui.signals) != 2 || tui.signals[0].Text != "- not yet complete" {
		t.Fatalf("unexpected signals: %v", tui.signals)
	}
}

func TestTuiStateParsesSummariesKeepsPartialMarker(t *testing.T) {
	t.Run("PartialDoubleLeftChevrons", func(t *testing.T) {
		tui := newTUIForTest()
		tui.write([]byte("prose\n<<"))
		tui.write([]byte("黿鼍爩 <summary>\n- done\n黿鼍爩\n"))
		if len(tui.signals) != 2 || tui.signals[0].Text != "- done" {
			t.Fatalf("unexpected signals: %v", tui.signals)
		}
	})
	t.Run("SingleLeftChevron", func(t *testing.T) {
		tui := newTUIForTest()
		tui.write([]byte("prose\n<"))
		tui.write([]byte("<黿鼍爩 <summary>\n- done\n黿鼍爩\n"))
		if len(tui.signals) != 2 || tui.signals[0].Text != "- done" {
			t.Fatalf("unexpected signals: %v", tui.signals)
		}
	})
}

func TestTuiStateCollectsFinishSignals(t *testing.T) {
	tui := newTUIForTest()
	tui.finishReason("stop")
	if len(tui.signals) != 1 {
		t.Fatalf("expected 1 finish signal, got %v", tui.signals)
	}
	if tui.signals[0].Text != "[Finish: stop]" {
		t.Fatalf("unexpected signal: %q", tui.signals[0].Text)
	}
}

func TestTuiStateSignalsCombineSummaryAndFinish(t *testing.T) {
	tui := newTUIForTest()
	tui.write([]byte("<<徕珑龘 <summary>\n- done\n徕珑龘\n"))
	tui.finishReason("stop")
	if len(tui.signals) != 3 {
		t.Fatalf("expected 3 signal lines, got %v", tui.signals)
	}
	if tui.signals[0].Text != "- done" || tui.signals[1].Text != "" || tui.signals[2].Text != "[Finish: stop]" {
		t.Fatalf("unexpected signals: %v", tui.signals)
	}
}

func TestTuiStateFinishSignalExpandsSummaryTab(t *testing.T) {
	tui := newTUIForTest()
	tui.tabs.Expanded = []bool{true, false, false}
	tui.tabs.HasContent = []bool{true, false, false}
	tui.tabs.Focus = 0
	tui.finishReason("stop")
	if !tui.tabs.Expanded[1] {
		t.Fatal("summary tab should auto-expand on a finish reason")
	}
	if tui.tabs.Focus != 0 {
		t.Fatalf("auto-expand must not change an established focus, got %d", tui.tabs.Focus)
	}
	if len(tui.signals) != 1 {
		t.Fatalf("expected the finish signal, got %v", tui.signals)
	}
}

func TestTuiStateSummaryTabTitle(t *testing.T) {
	if tabNames[1] != "Summary" {
		t.Fatalf("expected the summary tab title, got %q", tabNames[1])
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
	go taiui.ReadKeys(strings.NewReader("123sS"), ch)
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
		if !tui.confirmQuit {
			t.Fatal("the first quit key press must set the confirmation state")
		}
		if !tui.handleQuitKey() {
			t.Fatal("the second quit key press must confirm the quit")
		}
	})

	t.Run("AnyOtherKeyCancels", func(t *testing.T) {
		tui := newTUIForTest()
		tui.handleQuitKey()
		if !tui.confirmQuit {
			t.Fatal("expected the confirmation state after the first quit key")
		}
		tui.cancelConfirmQuit()
		if tui.confirmQuit {
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
		tui.confirmQuit = true
		tui.render()
		if !strings.Contains(sb.String(), "Quit?") {
			t.Fatalf("expected the quit confirmation bar in the rendered output, got: %q", sb.String())
		}
		sb.Reset()
		tui.confirmQuit = false
		tui.render()
		if strings.Contains(sb.String(), "Quit?") {
			t.Fatalf("expected no confirmation bar without a pending confirmation, got: %q", sb.String())
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
	tui.write([]byte("model output\n"))
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

	tui.write([]byte("<<徕珑龘 <summary>\n- done\n徕珑龘\n"))
	if !tui.tabs.Expanded[1] {
		t.Fatal("summary tab should auto-expand on a summary block")
	}
	if tui.tabs.Focus != 0 {
		t.Fatalf("auto-expand must not change an established focus, got %d", tui.tabs.Focus)
	}
	if len(tui.signals) != 2 {
		t.Fatalf("expected the summary signals, got %v", tui.signals)
	}

	tui2 := newTUIForTest()
	tui2.tabs.Expanded = []bool{true, false, false}
	tui2.tabs.HasContent = []bool{true, false, false}
	tui2.tabs.Focus = 0
	tui2.write([]byte("<<龘靐齉 <change op=\"MODIFY\" target=\"Foo\" file-path=\"x.go\">\nfunc Foo() {}\n龘靐齉\n"))
	if tui2.tabs.Expanded[1] {
		t.Fatal("summary tab must not expand without a summary block or finish line")
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
	var gotOpts loops.RunOptions
	run := func(ctx context.Context, opts loops.RunOptions, result *loops.Result) iter.Seq[error] {
		gotOpts = opts
		return func(yield func(error) bool) {}
	}
	wrapped := withTUIOutputObserver(run, tui)

	var result loops.Result
	for e := range wrapped(context.Background(), loops.RunOptions{}, &result) {
		t.Fatal(e)
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
	if len(tui.signals) != 1 || tui.signals[0].Text != "[Finish: stop]" {
		t.Fatalf("expected finish reason in round tab, got %v", tui.signals)
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

func TestTuiStateFinishReasonColor(t *testing.T) {
	tui := newTUIForTest()
	tui.finishReason("stop")
	if len(tui.signals) != 1 {
		t.Fatalf("expected 1 finish signal, got %v", tui.signals)
	}
	if tui.signals[0].Text != "[Finish: stop]" || tui.signals[0].Color != outputColorLogLine {
		t.Fatalf("unexpected signal: %+v", tui.signals[0])
	}
}

func TestTuiStateSummaryLinesPlain(t *testing.T) {
	tui := newTUIForTest()
	tui.write([]byte("<<徕珑龘 <summary>\n- done\n徕珑龘\n"))
	if len(tui.signals) != 2 {
		t.Fatalf("expected 2 signal lines, got %v", tui.signals)
	}
	if tui.signals[0].Text != "- done" || tui.signals[0].Color != taiui.NoColor {
		t.Fatalf("unexpected signal: %+v", tui.signals[0])
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
