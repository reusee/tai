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
	// TUI panels wrap long lines within their visible width so content
	// is never hidden behind a pane edge or the scrollbar. The panel
	// text is rendered by VerticalScroll, which renders its child at the
	// visible width (the window width minus the scrollbar column) when
	// the scrollbar is shown.
	tui := &TUI{}
	lines := []string{
		"abcdefghijklmnopqrstuvwxyz",
		"abcdefghijklmnopqrstuvwxyz",
		"abcdefghijklmnopqrstuvwxyz",
	}
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
	// visible content area is 9 columns. Each source line wraps at the
	// visible width: the second visible line starts with 'j'. Without
	// visible-width wrapping, the line would wrap at the full 10-column
	// content width and the second visible line would start with 'k',
	// hiding a column behind the scrollbar.
	if cell := frame.Cells[1*frame.Width+1]; cell.Rune != 'a' {
		t.Fatalf("expected 'a' at (1,1), got %v", cell.Rune)
	}
	if cell := frame.Cells[2*frame.Width+1]; cell.Rune != 'j' {
		t.Fatalf("expected 'j' at (1,2), got %v", cell.Rune)
	}
}
