package main

import (
	"fmt"
	"strings"
	"testing"
	"time"

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

func TestTuiStateWriteLines(t *testing.T) {
	st := &tuiState{}
	st.write([]byte("hello\nworld\n"))
	if len(st.lines) != 2 || st.lines[0] != "hello" || st.lines[1] != "world" {
		t.Fatalf("unexpected lines: %v", st.lines)
	}
}

func TestTuiStateWriteLogs(t *testing.T) {
	st := &tuiState{}
	st.writeLogs([]byte("hello\nworld\n"))
	if len(st.logs) != 2 || st.logs[0] != "hello" || st.logs[1] != "world" {
		t.Fatalf("unexpected logs: %v", st.logs)
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

func TestTuiStatePartialLines(t *testing.T) {
	st := &tuiState{}
	st.write([]byte("foo"))
	st.write([]byte("bar\n"))
	if len(st.lines) != 1 || st.lines[0] != "foobar" {
		t.Fatalf("unexpected lines: %v", st.lines)
	}
	if st.partial != "" {
		t.Fatalf("unexpected partial: %q", st.partial)
	}
	st.write([]byte("baz"))
	if st.partial != "baz" {
		t.Fatalf("unexpected partial: %q", st.partial)
	}
	lines := st.outputLinesForRender()
	if len(lines) != 2 || lines[1] != "baz" {
		t.Fatalf("unexpected rendered lines: %v", lines)
	}
}

func TestTuiStateParsesSummaries(t *testing.T) {
	st := &tuiState{}
	st.write([]byte("<<徕珑龘 <summary>\n- one\n- two\n徕珑龘\n"))
	if len(st.summaries) != 3 {
		t.Fatalf("expected 3 summary lines, got %v", st.summaries)
	}
	if st.summaries[0] != "- one" || st.summaries[1] != "- two" || st.summaries[2] != "" {
		t.Fatalf("unexpected summaries: %v", st.summaries)
	}
}

func TestTuiStateParsesSummariesAcrossChunks(t *testing.T) {
	st := &tuiState{}
	st.write([]byte("<<徕珑龘 <summary>\n- one\n- tw"))
	st.write([]byte("o\n徕珑龘\n"))
	if len(st.summaries) != 3 {
		t.Fatalf("expected 3 summary lines, got %v", st.summaries)
	}
	if st.summaries[0] != "- one" || st.summaries[1] != "- two" {
		t.Fatalf("unexpected summaries: %v", st.summaries)
	}
}

func TestTuiStateIgnoresOtherBlocks(t *testing.T) {
	st := &tuiState{}
	text := "<<龘靐齉 <change op=\"MODIFY\" target=\"Foo\" file-path=\"x.go\">\nfunc Foo() {}\n龘靐齉\n" +
		"<<徕珑龘 <summary>\n- s\n徕珑龘\n"
	st.write([]byte(text))
	if len(st.summaries) != 2 || st.summaries[0] != "- s" || st.summaries[1] != "" {
		t.Fatalf("unexpected summaries: %v", st.summaries)
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
	if len(st.summaries) != 2 {
		t.Fatalf("expected 2 summary lines, got %v", st.summaries)
	}
	if st.summaries[0] != "- done" || st.summaries[1] != "" {
		t.Fatalf("unexpected summaries: %v", st.summaries)
	}
}

func TestTuiStateParsesSummariesWaitsForStreamingBlock(t *testing.T) {
	// A fragment whose closing line is still streaming must be kept, not
	// skipped: no complete block exists beyond it, so the parser waits
	// for the fragment's own closing line. See TheoryOfTUI.
	st := &tuiState{}
	st.write([]byte("<<黿鼍爩 <summary>\n- not yet complete"))
	if len(st.summaries) != 0 {
		t.Fatalf("expected no summaries while the block is incomplete, got %v", st.summaries)
	}
	st.write([]byte("\n黿鼍爩\n"))
	if len(st.summaries) != 2 || st.summaries[0] != "- not yet complete" {
		t.Fatalf("unexpected summaries: %v", st.summaries)
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
		if len(st.summaries) != 2 || st.summaries[0] != "- done" {
			t.Fatalf("unexpected summaries: %v", st.summaries)
		}
	})
	t.Run("SingleLeftChevron", func(t *testing.T) {
		st := &tuiState{}
		st.write([]byte("prose\n<"))
		st.write([]byte("<黿鼍爩 <summary>\n- done\n黿鼍爩\n"))
		if len(st.summaries) != 2 || st.summaries[0] != "- done" {
			t.Fatalf("unexpected summaries: %v", st.summaries)
		}
	})
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
	display := taiui.WrapLines(src, 9)
	last := display[len(display)-1]
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
		element := tui.panel(0, taiui.Box{Top: 0, Left: 0, Bottom: 4, Right: 12}, []string{"content"}, 0, focus)
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

func TestTUIToggleTabs(t *testing.T) {
	tui := &TUI{}
	tui.open = [3]bool{true, true, true}
	tui.focus = 0

	// Closing the focused tab moves the focus to the next open tab.
	tui.toggleTab(0)
	if tui.open[0] {
		t.Fatal("output tab should be closed")
	}
	if tui.focus != 1 {
		t.Fatalf("focus should move to the summary tab, got %d", tui.focus)
	}

	// Closing a non-focused tab leaves the focus unchanged.
	tui.toggleTab(2)
	if tui.open[2] {
		t.Fatal("logs tab should be closed")
	}
	if tui.focus != 1 {
		t.Fatalf("focus should stay on the summary tab, got %d", tui.focus)
	}

	// Closing the last open tab clears the focus.
	tui.toggleTab(1)
	if tui.open[1] {
		t.Fatal("summary tab should be closed")
	}
	if tui.focus != -1 {
		t.Fatalf("focus should be -1 when no tab is open, got %d", tui.focus)
	}

	// Reopening a tab restores the focus to it.
	tui.toggleTab(2)
	if !tui.open[2] {
		t.Fatal("logs tab should be reopened")
	}
	if tui.focus != 2 {
		t.Fatalf("focus should be on the reopened logs tab, got %d", tui.focus)
	}
}

func TestTUICycleFocusSkipsClosedTabs(t *testing.T) {
	tui := &TUI{}
	tui.open = [3]bool{true, false, true}
	tui.focus = 0
	tui.cycleFocus()
	if tui.focus != 2 {
		t.Fatalf("focus should skip the closed summary tab and land on logs, got %d", tui.focus)
	}
	tui.cycleFocus()
	if tui.focus != 0 {
		t.Fatalf("focus should wrap to the output tab, got %d", tui.focus)
	}
	tui.open = [3]bool{false, false, false}
	tui.cycleFocus()
	if tui.focus != -1 {
		t.Fatalf("focus should be -1 with no open tabs, got %d", tui.focus)
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
	lines := taiui.WrapLines([]string{src, src, src}, 11)
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

func TestTabPanelBoxClampMatchesScrollView(t *testing.T) {
	// The scroll offset clamp must match the actual scroll view height.
	// In horizontal split (stacked), each panel is height/N tall, so the
	// scroll view is the panel height minus the one-row label strip. The
	// old clamp used the full-height pane (height - height/10), which
	// allowed the offset to reach only len(displays) - 0.9*height —
	// stopping the scroll before the content tail became visible.
	// See TheoryOfTUI.
	//
	// Stacked layout, 2 tabs on a 40-row screen: each panel is 20 rows,
	// the label strip is 1 row, so the scroll view is 19 rows and the
	// tail offset is len(displays) - 19.
	box := tabPanelBox(false, 0, 2, 80, 40)
	paneHeight := max(box.Height()-1, 1)
	if paneHeight != 19 {
		t.Fatalf("expected a 19-row scroll view, got %d", paneHeight)
	}
	const displayLines = 100
	if got := scrollClamp(1<<30, displayLines, paneHeight); got != displayLines-19 {
		t.Fatalf("expected the tail offset %d, got %d", displayLines-19, got)
	}

	// Side-by-side layout: the panel spans the full height, so the
	// scroll view is height - 1.
	box = tabPanelBox(true, 0, 2, 80, 40)
	if paneHeight := max(box.Height()-1, 1); paneHeight != 39 {
		t.Fatalf("expected a 39-row scroll view, got %d", paneHeight)
	}
}

func TestTUISticksToTail(t *testing.T) {
	tui := &TUI{}
	tui.open = [3]bool{true, false, false}
	tui.focus = 0
	tui.follow = [3]bool{true, false, false}
	tui.screen = taiui.NewTerminalScreen(&strings.Builder{}, 80, 10)
	tui.width = 80
	tui.height = 10

	var sb strings.Builder
	for i := 0; i < 20; i++ {
		fmt.Fprintf(&sb, "line %d\n", i)
	}
	tui.write([]byte(sb.String()))
	tui.render()

	contentWidth := 79 // 80 minus the scrollbar column
	display := taiui.WrapLines(tui.outputLinesForRender(), contentWidth)
	want := len(display) - 9 // 10 rows minus the one-row label strip
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
	display = taiui.WrapLines(tui.outputLinesForRender(), contentWidth)
	want = len(display) - 9
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
	tui.open = [3]bool{true, false, false}
	tui.focus = 0
	tui.follow = [3]bool{true, false, false}
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

	// Close and reopen the output tab.
	tui.toggleTab(0)
	if tui.open[0] {
		t.Fatal("expected output tab closed")
	}
	if tui.focus != -1 {
		t.Fatalf("expected focus -1 with no open tabs, got %d", tui.focus)
	}
	tui.toggleTab(0)
	if !tui.open[0] {
		t.Fatal("expected output tab reopened")
	}
	if tui.focus != 0 {
		t.Fatalf("expected focus 0 after reopen, got %d", tui.focus)
	}
	// Reopening must resume following the live tail.
	if !tui.follow[0] {
		t.Fatal("expected follow true after reopen")
	}

	// New content arriving after the reopen must move the view to the
	// new tail, so the pane shows the latest lines.
	tui.write([]byte("line 10\nline 11\n"))
	tui.render()
	contentWidth := 79 // 80 columns minus the scrollbar column
	display := taiui.WrapLines(tui.outputLinesForRender(), contentWidth)
	want := len(display) - 9 // 10 rows minus the one-row label strip
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
	tui.open = [3]bool{true, false, false}
	tui.focus = 0
	tui.follow = [3]bool{true, false, false}
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
	display := taiui.WrapLines(tui.outputLinesForRender(), contentWidth)
	initialMax := len(display) - 9
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
	display = taiui.WrapLines(tui.outputLinesForRender(), contentWidth)
	want := len(display) - 9
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
	tui.open = [3]bool{true, false, false}
	tui.focus = 0
	tui.follow = [3]bool{true, false, false}
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
