package main

import (
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

func TestTUIPanelShowsTailOfWrappedContent(t *testing.T) {
	// The panel must reach the tail of wrapped content: the scroll
	// offset is clamped against the wrapped display-line count, so at
	// the maximum offset the last display line lands on the pane's last
	// row. Under raw-line clamping the tail was unreachable once
	// wrapping multiplied the display rows. See TheoryOfTUI.
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
	// height 10 gives paneHeight 8; the tail offset is
	// len(display) - 8 + 1 = len(display) - 7.
	element := tui.panel("Title", display, len(display)-7, 10, false)

	screen := &panelTestScreen{width: 12, height: 10}
	taiui.Render(taiui.NewBaseScope(func() taiui.Root {
		return taiui.Root{Element: element}
	}), screen)

	if len(screen.frames) == 0 {
		t.Fatal("expected a rendered frame")
	}
	frame := screen.frames[len(screen.frames)-1]
	// At the maximum offset the last display line lands on the pane's
	// bottom row: screen row 8, column 1 (inside the top border).
	if cell := frame.Cells[8*frame.Width+1]; cell.Rune != 'T' {
		t.Fatalf("expected THE-END at the pane's bottom row (8,1), got %v", cell.Rune)
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

func TestScrollClamp(t *testing.T) {
	// The maximum offset is displayLines - paneHeight + 1: the pane's
	// scroll box starts one row inside the panel border, so at the
	// maximum offset the last display line lands on the pane's last row.
	// An offset beyond the content clamps to the maximum, a negative
	// offset clamps to 0, and the tail sentinel (1<<30) clamps to the
	// last row. See TheoryOfTUI.
	if got := scrollClamp(0, 10, 3); got != 0 {
		t.Fatalf("offset 0 should be unchanged, got %d", got)
	}
	if got := scrollClamp(8, 10, 3); got != 8 {
		t.Fatalf("offset 8 (the max) should be unchanged, got %d", got)
	}
	if got := scrollClamp(9, 10, 3); got != 8 {
		t.Fatalf("offset 9 should clamp to 8, got %d", got)
	}
	if got := scrollClamp(100, 10, 3); got != 8 {
		t.Fatalf("offset 100 should clamp to 8, got %d", got)
	}
	if got := scrollClamp(1<<30, 10, 3); got != 8 {
		t.Fatalf("tail sentinel should clamp to 8, got %d", got)
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
	// width (the window width minus the border and scrollbar columns), so
	// a long source line occupies several display rows. The panel's Text
	// renders the pre-wrapped lines; the first display rows are visible.
	tui := &TUI{}
	src := strings.Repeat("abcdefghijklmnopqrstuvwxyz", 1)
	lines := taiui.WrapLines([]string{src, src, src}, 9)
	element := tui.panel("Title", lines, 0, 4, false)

	screen := &panelTestScreen{width: 12, height: 6}
	taiui.Render(taiui.NewBaseScope(func() taiui.Root {
		return taiui.Root{Element: element}
	}), screen)

	if len(screen.frames) == 0 {
		t.Fatal("expected a rendered frame")
	}
	frame := screen.frames[len(screen.frames)-1]
	// The pane is 12 columns wide with a border and a scrollbar; the
	// visible content area is 9 columns. Each source line wraps into
	// 9-column display rows: the first display row is "abcdefghi" and
	// the second is "jklmnopqr".
	if cell := frame.Cells[1*frame.Width+1]; cell.Rune != 'a' {
		t.Fatalf("expected 'a' at (1,1), got %v", cell.Rune)
	}
	if cell := frame.Cells[2*frame.Width+1]; cell.Rune != 'j' {
		t.Fatalf("expected 'j' at (1,2), got %v", cell.Rune)
	}
}
