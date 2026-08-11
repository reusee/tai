package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gdamore/tcell/v3/tty"
	"github.com/reusee/dscope"
	"github.com/reusee/tai/blocks"
	"github.com/reusee/tai/flags"
	"github.com/reusee/tai/logs"
	"github.com/reusee/tai/taiui"
)

const (
	maxTUILines     = 10000
	maxTUISummaries = 2000
)

const TheoryOfTUI = `
The TUI interface replaces stdout with a three-pane terminal UI: the left
pane streams the model output, the middle pane collects the bodies of summary
blocks as they appear in the output stream, and the right pane collects log
records. Tab cycles the focus among the panes; up/down and page up/down
scroll the focused pane; home/end jump to the start/end. When the generation
finishes, the TUI stays open so the output can be browsed, and q quits the
TUI.

Standard output is replaced by a pipe whose reader copies into the TUI's
output buffer, so all existing code paths that write to os.Stdout — including
file markers, round statistics, and goal loop banners — appear in the TUI as
a single stream. The pipe is intentionally not a terminal, so library-level
terminal detection (e.g. colors in generators.NewOutput) sees a non-terminal
and suppresses raw ANSI escapes, keeping the TUI buffer free of control
sequences. The TUI owns its own tty for rendering and input.

Log records are routed to the logs pane by forking the logs.Writer provider
to a writer that appends to the TUI's logs buffer, so logs are not written
to stderr in TUI mode. Direct stderr writes from other code paths remain
captured by the output pipe, matching the stdout stream.

Summary extraction reuses the block parser: the TUI writer keeps a parse
buffer and extracts each complete summary block's body as it streams, so the
middle pane is updated incrementally without requiring the generation loop
to know about the TUI.
`

// Tui enables the terminal UI mode.
type Tui bool

func (Module) Tui() Tui { return false }

var _ flags.Flag = Tui(false)

func (t Tui) Handle(key string, args []string) (newDef any, remainArgs []string, err error) {
	ret := Tui(true)
	return &ret, args, nil
}

func (t Tui) Keys() map[string]string {
	return map[string]string{"-tui": "Use the TUI interface"}
}

// tuiState holds the data behind the TUI: the output line buffer, the
// parsed summary lines, the log line buffer, and the view offsets. It is
// testable without a terminal.
type tuiState struct {
	mu          sync.Mutex
	lines       []string
	partial     string // incomplete trailing output line
	summaries   []string
	logs        []string
	logsPartial string // incomplete trailing log line
	parseBuf    []byte

	// view state
	topLeft  int
	topRight int
	topLogs  int
	focus    int // 0 = output, 1 = summary, 2 = logs
	finished bool
}

// write appends output and extracts summary blocks.
func (s *tuiState) write(p []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.appendOutput(p)
	s.parseSummaries(p)
}

// writeLogs appends log output to the logs buffer, splitting it into lines
// and retaining the incomplete trailing line for the next chunk.
func (s *tuiState) writeLogs(p []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.logsPartial += string(p)
	for {
		idx := strings.IndexByte(s.logsPartial, '\n')
		if idx < 0 {
			break
		}
		s.logs = append(s.logs, s.logsPartial[:idx])
		s.logsPartial = s.logsPartial[idx+1:]
		if len(s.logs) > maxTUILines {
			s.logs = append([]string(nil), s.logs[len(s.logs)-maxTUILines:]...)
		}
	}
}

func (s *tuiState) appendOutput(p []byte) {
	s.partial += string(p)
	for {
		idx := strings.IndexByte(s.partial, '\n')
		if idx < 0 {
			break
		}
		s.lines = append(s.lines, s.partial[:idx])
		s.partial = s.partial[idx+1:]
		if len(s.lines) > maxTUILines {
			s.lines = append([]string(nil), s.lines[len(s.lines)-maxTUILines:]...)
		}
	}
}

func (s *tuiState) parseSummaries(p []byte) {
	s.parseBuf = append(s.parseBuf, p...)
	for {
		block, _, end, ok, err := blocks.ParseFirstBlock(s.parseBuf)
		if err != nil {
			// incomplete block; wait for more output
			return
		}
		if !ok {
			// no block marker in the buffer
			s.parseBuf = nil
			return
		}
		if block.Kind == "summary" {
			body := strings.TrimSpace(block.Body)
			if body != "" {
				for _, line := range strings.Split(body, "\n") {
					s.summaries = append(s.summaries, line)
				}
				s.summaries = append(s.summaries, "")
				if len(s.summaries) > maxTUISummaries {
					s.summaries = append([]string(nil), s.summaries[len(s.summaries)-maxTUISummaries:]...)
				}
			}
		}
		s.parseBuf = s.parseBuf[end:]
		if len(s.parseBuf) == 0 {
			return
		}
	}
}

// outputLinesForRender returns the complete output lines, including the
// partial trailing line.
func (s *tuiState) outputLinesForRender() []string {
	if s.partial == "" {
		return s.lines
	}
	ret := make([]string, 0, len(s.lines)+1)
	ret = append(ret, s.lines...)
	ret = append(ret, s.partial)
	return ret
}

// logsLinesForRender returns the complete log lines, including the
// partial trailing line.
func (s *tuiState) logsLinesForRender() []string {
	if s.logsPartial == "" {
		return s.logs
	}
	ret := make([]string, 0, len(s.logs)+1)
	ret = append(ret, s.logs...)
	ret = append(ret, s.logsPartial)
	return ret
}

type tuiWriter struct{ t *TUI }

type logsWriter struct{ t *TUI }

func (w tuiWriter) Write(p []byte) (int, error) {
	w.t.write(p)
	w.t.notify()
	return len(p), nil
}

func (w logsWriter) Write(p []byte) (int, error) {
	w.t.writeLogs(p)
	w.t.notify()
	return len(p), nil
}

type TUI struct {
	tuiState
	tty      tty.Tty
	screen   *taiui.TerminalScreen
	updateCh chan struct{}
	width    int
	height   int
	runErr   error
}

func newTUI() (*TUI, error) {
	t, err := tty.NewStdIoTty()
	if err != nil {
		t, err = tty.NewDevTty()
		if err != nil {
			return nil, err
		}
	}
	if err := t.Start(); err != nil {
		return nil, err
	}
	width, height := 80, 25
	if ws, err := t.WindowSize(); err == nil && ws.Width > 0 && ws.Height > 0 {
		width, height = ws.Width, ws.Height
	}
	return &TUI{
		tuiState: tuiState{
			focus:    0,
			topLeft:  1 << 30,
			topRight: 1 << 30,
			topLogs:  1 << 30,
		},
		tty:      t,
		screen:   taiui.NewTerminalScreen(t, width, height),
		updateCh: make(chan struct{}, 1),
		width:    width,
		height:   height,
	}, nil
}

// Writer returns the writer that appends to the TUI output buffer.
func (t *TUI) Writer() io.Writer {
	return tuiWriter{t}
}

// LogsWriter returns the writer that appends log output to the TUI's logs
// pane. runWithTUI forks the logs.Writer provider to this writer so logs
// are not written to stderr in TUI mode.
func (t *TUI) LogsWriter() io.Writer {
	return logsWriter{t}
}

func (t *TUI) notify() {
	select {
	case t.updateCh <- struct{}{}:
	default:
	}
}

func (t *TUI) Stop() error {
	return t.tty.Stop()
}

func (t *TUI) Run(gen func()) error {
	io.WriteString(t.tty, "\x1b[?25l")
	defer t.Stop()

	resizeCh := make(chan bool, 4)
	t.tty.NotifyResize(resizeCh)
	keyCh := make(chan string, 16)
	go readTUIKeys(t.tty, keyCh)

	go func() {
		defer func() {
			if r := recover(); r != nil {
				t.mu.Lock()
				t.runErr = fmt.Errorf("panic: %v", r)
				t.mu.Unlock()
				t.write([]byte(fmt.Sprintf("[panic] %v\n", r)))
			}
			t.mu.Lock()
			t.finished = true
			t.mu.Unlock()
			t.notify()
		}()
		gen()
	}()

	for {
		t.render()
		select {
		case key := <-keyCh:
			switch key {
			case "tab":
				t.mu.Lock()
				t.focus = (t.focus + 1) % 3
				t.mu.Unlock()
			case "up":
				t.scroll(-1)
			case "down":
				t.scroll(1)
			case "pageup":
				t.scroll(-max(t.height-2, 1))
			case "pagedown":
				t.scroll(max(t.height-2, 1))
			case "home":
				t.scrollTo(0)
			case "end":
				t.scrollTo(1 << 30)
			case "quit":
				io.WriteString(t.tty, "\x1b[0m")
				fmt.Fprintf(t.tty, "\x1b[%d;1H", t.height)
				io.WriteString(t.tty, "\x1b[?25h")
				t.mu.Lock()
				err := t.runErr
				t.mu.Unlock()
				return err
			}
		case <-t.updateCh:
		case <-resizeCh:
			if ws, err := t.tty.WindowSize(); err == nil && ws.Width > 0 && ws.Height > 0 {
				t.mu.Lock()
				t.width, t.height = ws.Width, ws.Height
				t.mu.Unlock()
				t.screen.Resize(ws.Width, ws.Height)
			}
		}
	}
}

func (t *TUI) scroll(delta int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	switch t.focus {
	case 0:
		t.topLeft += delta
		if t.topLeft < 0 {
			t.topLeft = 0
		}
	case 1:
		t.topRight += delta
		if t.topRight < 0 {
			t.topRight = 0
		}
	case 2:
		t.topLogs += delta
		if t.topLogs < 0 {
			t.topLogs = 0
		}
	}
}

func (t *TUI) scrollTo(top int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	switch t.focus {
	case 0:
		t.topLeft = top
	case 1:
		t.topRight = top
	case 2:
		t.topLogs = top
	}
}

func (t *TUI) render() {
	t.mu.Lock()
	defer t.mu.Unlock()

	outputLines := t.outputLinesForRender()
	logsLines := t.logsLinesForRender()
	height := t.height
	if height < 1 {
		height = 1
	}

	maxLeft := 0
	if len(outputLines) > height {
		maxLeft = len(outputLines) - height
	}
	if t.topLeft > maxLeft {
		t.topLeft = maxLeft
	}
	if t.topLeft < 0 {
		t.topLeft = 0
	}

	maxRight := 0
	if len(t.summaries) > height {
		maxRight = len(t.summaries) - height
	}
	if t.topRight > maxRight {
		t.topRight = maxRight
	}
	if t.topRight < 0 {
		t.topRight = 0
	}

	maxLogs := 0
	if len(logsLines) > height {
		maxLogs = len(logsLines) - height
	}
	if t.topLogs > maxLogs {
		t.topLogs = maxLogs
	}
	if t.topLogs < 0 {
		t.topLogs = 0
	}

	outputTitle := "Output"
	if t.finished {
		outputTitle = "Output (done)"
	}

	left := t.panel(outputTitle, outputLines, t.topLeft, height, t.focus == 0)
	middle := t.panel("Summary", t.summaries, t.topRight, height, t.focus == 1)
	right := t.panel("Logs", logsLines, t.topLogs, height, t.focus == 2)

	root := taiui.Root{Element: taiui.Row(
		taiui.Weighted(1, left),
		taiui.Weighted(1, middle),
		taiui.Weighted(1, right),
	)}
	taiui.Render(taiui.NewBaseScope(func() taiui.Root { return root }), t.screen)
}

func (t *TUI) panel(title string, lines []string, top, height int, focus bool) taiui.Element {
	borderColor := taiui.HexColor(0x888888)
	if focus {
		borderColor = taiui.HexColor(0xffffff)
	}
	return taiui.Rect(
		taiui.Border(true),
		taiui.BorderStyle(taiui.SameStyle.SetFG(borderColor)),
		taiui.Title(title),
		taiui.VerticalScroll(
			// Long lines wrap at the visible width so content is never
			// hidden behind a pane edge or the scrollbar.
			taiui.Text(lines, taiui.Wrap(true)),
			top+height/2,
			taiui.Scrollbar(true),
			taiui.Fill(true),
		),
	)
}

func readTUIKeys(r io.Reader, ch chan<- string) {
	var buf [64]byte
	var pending []byte
	for {
		n, err := r.Read(buf[:])
		if err != nil {
			return
		}
		if n == 0 {
			// The tty is in non-blocking raw mode; avoid a busy loop.
			// An incomplete ESC sequence that never grew is discarded.
			if len(pending) > 0 && pending[0] == 0x1b && len(pending) < 3 {
				pending = pending[:0]
			}
			time.Sleep(2 * time.Millisecond)
			continue
		}
		pending = append(pending, buf[:n]...)
		for len(pending) > 0 {
			if pending[0] == 0x1b {
				if len(pending) == 1 {
					break
				}
				if pending[1] != '[' {
					// ESC followed by a non-sequence byte: the ESC
					// is not part of an escape sequence.
					pending = pending[1:]
					continue
				}
				if len(pending) < 3 {
					break
				}
				switch pending[2] {
				case 'A':
					ch <- "up"
					pending = pending[3:]
					continue
				case 'B':
					ch <- "down"
					pending = pending[3:]
					continue
				case 'H':
					ch <- "home"
					pending = pending[3:]
					continue
				case 'F':
					ch <- "end"
					pending = pending[3:]
					continue
				default:
					if pending[2] >= '0' && pending[2] <= '9' {
						// tilde sequence: ESC [ n ~
						idx := bytes.IndexByte(pending, '~')
						if idx < 0 {
							break
						}
						seq := string(pending[:idx+1])
						switch seq {
						case "\x1b[5~":
							ch <- "pageup"
						case "\x1b[6~":
							ch <- "pagedown"
						case "\x1b[7~":
							ch <- "home"
						case "\x1b[8~":
							ch <- "end"
						}
						pending = pending[idx+1:]
						continue
					}
					// unknown escape sequence: discard one byte
					pending = pending[3:]
					continue
				}
			}
			switch pending[0] {
			case 'q', 'Q', 0x03:
				ch <- "quit"
			case '\t':
				ch <- "tab"
			}
			pending = pending[1:]
		}
	}
}

func runWithTUI(command Command, scope dscope.Scope) {
	tui, err := newTUI()
	if err != nil {
		fmt.Fprintf(os.Stderr, "cannot start TUI: %v; continuing without TUI\n", err)
		scope.Fork(command.Defs...).Call(command.Main)
		return
	}
	pr, pw, err := os.Pipe()
	if err != nil {
		_ = tui.Stop()
		fmt.Fprintf(os.Stderr, "cannot create output pipe: %v; continuing without TUI\n", err)
		scope.Fork(command.Defs...).Call(command.Main)
		return
	}
	oldOut, oldErr := os.Stdout, os.Stderr
	os.Stdout = pw
	os.Stderr = pw
	copyDone := make(chan struct{})
	go func() {
		defer close(copyDone)
		_, _ = io.Copy(tui.Writer(), pr)
	}()
	// In TUI mode, route logs to the logs pane instead of stderr. The
	// logs.Writer provider is forked to the TUI's logs writer, so every
	// log record lands in the logs pane and never in the output pipe.
	scope = scope.Fork(func() logs.Writer { return logs.Writer(tui.LogsWriter()) })
	runErr := tui.Run(func() {
		scope.Fork(command.Defs...).Call(command.Main)
	})
	_ = pw.Close()
	<-copyDone
	_ = pr.Close()
	os.Stdout, os.Stderr = oldOut, oldErr
	if runErr != nil {
		fmt.Fprintf(oldErr, "%v\n", runErr)
		os.Exit(1)
	}
}
