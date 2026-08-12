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
The TUI interface replaces stdout with a three-tab terminal UI: the Output
tab streams the model output, the Summary tab collects the bodies of summary
blocks as they appear in the output stream, and the Logs tab collects log
records. The keys 1, 2, and 3 toggle each tab on and off (Output, Summary,
Logs respectively); the available space is divided equally among the open
tabs. The s key switches between vertical splitting (tabs side by side, a
vertical split line) and horizontal splitting (tabs stacked, a horizontal
split line). Tab cycles the focus among the open tabs, skipping closed
ones; up/down and page up/down scroll the focused pane; home/end jump to
the start/end. When the generation finishes, the TUI stays open so the
output can be browsed, and q quits the TUI. When all tabs are closed, a
hint panel is shown instead of a blank screen.

Tabs are distinguished by alternating gray background shades instead of
borders: each tab has a base gray (darker for Output, lighter for Logs)
that lightens when the tab is focused, so the focus is visible without a
border. A one-row label strip across each tab's top names it; the focused
label is bright and bold, the unfocused label gray.

Each tab's view follows the latest content while it is at the latest row,
matching the terminal session convention. A follow flag per tab starts
true; any manual scroll that leaves the latest row clears it, and a scroll
that reaches the latest row again sets it. While a tab follows, every
render sticks its offset to the newest content as output arrives; while it
does not, the view stays where the user placed it even as content grows.

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

Scroll extents are derived from the wrapped display lines, not the raw
source lines: each pane pre-wraps its content with taiui.WrapLines at the
visible width (the tab's width minus the scrollbar column), exactly as the
pane's Text wraps it, so the tail of wrapped content is reachable and the
scrollbar thumb maps the visible window onto the full content. Each tab
carries a one-row label strip across its top; the scroll view spans the
remaining rows. The panel boxes are computed from the split geometry
before rendering — full screen height in vertical split (side by side),
height/N in horizontal split (stacked), with the last tab absorbing the
rounding remainder — so the label strip and the scroll offset clamp are
derived from the exact panel dimensions. The pane-local offset is clamped
by scrollClamp to [0, displayLines - paneHeight], where paneHeight is the
ACTUAL scroll view height: the panel height minus the label row. A
full-height pane height would match the scroll view in vertical split but
would stop the scroll short in horizontal split — each panel is height/N
tall, so its scroll view is correspondingly shorter — leaving the content
tail unreachable. At the maximum offset the last display line lands on the
scroll view's last row. The tail sentinel (1<<30) used by the end key
clamps to that maximum.
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

type tuiState struct {
	mu          sync.Mutex
	lines       []string
	partial     string // incomplete trailing output line
	summaries   []string
	logs        []string
	logsPartial string // incomplete trailing log line
	parseBuf    []byte

	// open reports whether each tab is enabled: index 0 is the output
	// tab, 1 the summary tab, 2 the logs tab. The keys 1, 2, and 3
	// toggle them. splitVertical reports whether the tabs are laid out
	// side by side (vertical split line) or stacked (horizontal split
	// line); the s key toggles it. See TheoryOfTUI.
	open          [3]bool
	splitVertical bool

	// view state
	topLeft  int
	topRight int
	topLogs  int
	focus    int // 0 = output, 1 = summary, 2 = logs, -1 = none open
	finished bool

	// follow reports whether each tab's view sticks to the latest content.
	// It starts true; manual scrolling away clears it, and reaching the
	// latest row sets it again. See TheoryOfTUI.
	follow [3]bool

	// maxOffsets holds the maximum scroll offset of each tab computed at
	// the last render. scroll and scrollTo use it to decide whether the
	// user's view is at the latest row.
	maxOffsets [3]int
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
			open:          [3]bool{true, true, true},
			splitVertical: true,
			focus:         0,
			topLeft:       1 << 30,
			topRight:      1 << 30,
			topLogs:       1 << 30,
			follow:        [3]bool{true, true, true},
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

// scrollClamp clamps a pane scroll offset against the pane's wrapped
// display lines. The maximum offset is displayLines - paneHeight: at the
// maximum offset the last display line lands on the pane's last row.
// Offsets beyond the content clamp to the maximum; negative offsets clamp
// to 0. A large sentinel offset (1<<30) therefore sticks the view to the
// tail. See TheoryOfTUI.
func scrollClamp(offset, displayLines, paneHeight int) int {
	if displayLines <= paneHeight {
		return 0
	}
	maxOffset := displayLines - paneHeight
	if offset > maxOffset {
		offset = maxOffset
	}
	if offset < 0 {
		offset = 0
	}
	return offset
}

// toggleTab toggles the open state of the tab at the given index
// (0 = output, 1 = summary, 2 = logs). Closing the focused tab moves the
// focus to another open tab; reopening a tab after all tabs were closed
// focuses it. See TheoryOfTUI.
func (t *TUI) toggleTab(idx int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.open[idx] = !t.open[idx]
	if t.focus == idx && !t.open[idx] {
		// The focused tab was closed: move the focus to another open tab.
		t.cycleFocus()
		return
	}
	if t.focus == -1 && t.open[idx] {
		// A tab was reopened after all tabs were closed: focus it.
		t.focus = idx
	}
}

// cycleFocus advances the focus to the next open tab after the current
// one, wrapping around. Closed tabs are skipped. When no tab is open, the
// focus becomes -1. See TheoryOfTUI.
func (t *TUI) cycleFocus() {
	if t.focus >= 0 {
		for i := 1; i <= 3; i++ {
			f := (t.focus + i) % 3
			if t.open[f] {
				t.focus = f
				return
			}
		}
	}
	for i := 0; i < 3; i++ {
		if t.open[i] {
			t.focus = i
			return
		}
	}
	t.focus = -1
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
				t.cycleFocus()
				t.mu.Unlock()
			case "1":
				t.toggleTab(0)
			case "2":
				t.toggleTab(1)
			case "3":
				t.toggleTab(2)
			case "split":
				t.mu.Lock()
				t.splitVertical = !t.splitVertical
				t.mu.Unlock()
			case "up":
				t.scroll(-1)
			case "down":
				t.scroll(1)
			case "pageup":
				t.scroll(-max(t.height-1, 1))
			case "pagedown":
				t.scroll(max(t.height-1, 1))
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
	var top *int
	idx := -1
	switch t.focus {
	case 0:
		top = &t.topLeft
		idx = 0
	case 1:
		top = &t.topRight
		idx = 1
	case 2:
		top = &t.topLogs
		idx = 2
	default:
		return
	}
	newTop := *top + delta
	if newTop < 0 {
		newTop = 0
	}
	if newTop > t.maxOffsets[idx] {
		newTop = t.maxOffsets[idx]
	}
	*top = newTop
	// Reaching the latest row resumes following; scrolling away stops it.
	// See TheoryOfTUI.
	t.follow[idx] = newTop >= t.maxOffsets[idx]
}

func (t *TUI) scrollTo(top int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	idx := -1
	switch t.focus {
	case 0:
		t.topLeft = top
		idx = 0
	case 1:
		t.topRight = top
		idx = 1
	case 2:
		t.topLogs = top
		idx = 2
	default:
		return
	}
	// Only a jump to the latest row (the end key reaches it via the sentinel)
	// resumes following; any other jump stops it. See TheoryOfTUI.
	t.follow[idx] = top >= t.maxOffsets[idx]
}

// tabPanelBox computes the panel box of the pos-th open tab when
// open tabs are laid out on a width x height screen. In vertical split
// (side by side) each tab spans the full screen height and a share of
// the width; in horizontal split (stacked) it spans the full width and
// a share of the height. The last tab absorbs the rounding remainder.
// The box is computed here — not left to the layout container — so the
// panel's one-row label strip, the scroll view below it, and the scroll
// offset clamp in render() are all derived from the exact panel
// dimensions. See TheoryOfTUI.
func tabPanelBox(splitVertical bool, pos, open, width, height int) taiui.Box {
	if splitVertical {
		tabWidth := width / open
		right := (pos + 1) * tabWidth
		if pos == open-1 {
			right = width
		}
		return taiui.Box{Top: 0, Left: pos * tabWidth, Bottom: height, Right: right}
	}
	tabHeight := height / open
	bottom := (pos + 1) * tabHeight
	if pos == open-1 {
		bottom = height
	}
	return taiui.Box{Top: pos * tabHeight, Left: 0, Bottom: bottom, Right: width}
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
	width := t.width
	if width < 1 {
		width = 1
	}

	// Open tabs share the available space equally. See TheoryOfTUI.
	var open []int
	for i := 0; i < 3; i++ {
		if t.open[i] {
			open = append(open, i)
		}
	}

	// With no tab open, show a hint instead of a blank screen.
	if len(open) == 0 {
		root := taiui.Root{Element: taiui.Rect(
			taiui.Fill(true),
			taiui.BGColor(taiui.HexColor(0x101010)),
			taiui.Text("Press 1/2/3 to open tabs", taiui.AlignCenter, taiui.VAlignMiddle),
		)}
		taiui.Render(taiui.NewBaseScope(func() taiui.Root { return root }), t.screen)
		return
	}

	// Compute the panel box of each open tab. The boxes are computed
	// here — not left to the layout container — so the panel's one-row
	// label strip, the scroll view below it, and the scroll offset
	// clamp below are all derived from the exact panel dimensions.
	// See tabPanelBox and TheoryOfTUI.
	panelBoxes := [3]taiui.Box{}
	for pos, idx := range open {
		panelBoxes[idx] = tabPanelBox(t.splitVertical, pos, len(open), width, height)
	}

	// The content width reserves one column for the scrollbar, matching
	// the scroll's visible-width rendering. All open tabs share the same
	// width except the last, which absorbs the rounding remainder.
	contentWidth := max(panelBoxes[open[0]].Width()-1, 1)

	displays := [3][]string{
		taiui.WrapLines(outputLines, contentWidth),
		taiui.WrapLines(t.summaries, contentWidth),
		taiui.WrapLines(logsLines, contentWidth),
	}

	// The scroll offset is clamped against the ACTUAL scroll view height
	// — the panel height minus the one-row label strip. In horizontal
	// split (stacked) each panel is height/N tall, so its scroll view is
	// far shorter than a full-height pane; clamping against a full-height
	// pane would stop the scroll before the content tail became visible.
	// While a tab follows the latest content, its offset is stuck to the
	// newest row before clamping; a view manually placed at the latest
	// row resumes following. See TheoryOfTUI.
	var maxOffsets [3]int
	tops := [3]int{t.topLeft, t.topRight, t.topLogs}
	for _, idx := range open {
		paneHeight := max(panelBoxes[idx].Height()-1, 1)
		maxOffsets[idx] = max(len(displays[idx])-paneHeight, 0)
		if t.follow[idx] {
			tops[idx] = maxOffsets[idx]
		}
		tops[idx] = scrollClamp(tops[idx], len(displays[idx]), paneHeight)
		if tops[idx] == maxOffsets[idx] {
			t.follow[idx] = true
		}
	}
	t.topLeft, t.topRight, t.topLogs = tops[0], tops[1], tops[2]
	t.maxOffsets = maxOffsets

	var panels []taiui.Element
	for _, idx := range open {
		panels = append(panels, t.panel(idx, panelBoxes[idx], displays[idx], tops[idx], t.focus == idx))
	}

	// The panels are overlaid on the root; each panel positions its own
	// label strip and scroll view at the exact box computed above.
	// Overlay takes ...any; []taiui.Element is not assignable to []any,
	// so the slice is converted element-wise.
	// See TheoryOfTUI.
	overlaySpecs := make([]any, len(panels))
	for i, p := range panels {
		overlaySpecs[i] = p
	}
	root := taiui.Root{Element: taiui.Overlay(overlaySpecs...)}
	taiui.Render(taiui.NewBaseScope(func() taiui.Root { return root }), t.screen)
}

// tabNames are the names of the three tabs, drawn in their label strips.
// tabGray is the base background shade of each tab: darker for the output
// tab, lighter for the logs tab, so tabs are distinguished by alternating
// gray shades instead of borders. A focused tab lightens its shade; the
// label strip is lighter still. See TheoryOfTUI.
var (
	tabNames = [...]string{"Output", "Summary", "Logs"}
	tabGray  = [3]int32{0x181818, 0x2c2c2c, 0x404040}
)

func (t *TUI) panel(idx int, box taiui.Box, lines []string, top int, focus bool) taiui.Element {
	base := tabGray[idx]
	if focus {
		base += 0x0e
	}
	label := tabNames[idx]
	if idx == 0 && t.finished {
		label = "Output (done)"
	}
	labelFg := int32(0x9a9a9a)
	if focus {
		labelFg = 0xffffff
	}
	// The label strip is pinned to the panel's top row and the scroll
	// view spans the remaining rows. The label is exactly one row, so
	// no blank rows appear below the title, and the scroll offset clamp
	// in render() can bound the view against the exact scroll height.
	// See TheoryOfTUI.
	headerBox := box
	headerBox.Bottom = box.Top + 1
	scrollBox := box
	scrollBox.Top = box.Top + 1
	return taiui.Overlay(
		taiui.Text(
			"  "+label+"  ",
			taiui.Box(headerBox),
			taiui.Fill(true),
			taiui.BGColor(taiui.HexColor(base+0x10)),
			taiui.Bold(focus),
			taiui.FGColor(taiui.HexColor(labelFg)),
		),
		taiui.VerticalScroll(
			taiui.Text(lines, taiui.Wrap(true)),
			top,
			taiui.Box(scrollBox),
			taiui.Scrollbar(true),
			taiui.Fill(true),
			taiui.BGColor(taiui.HexColor(base)),
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
			case '1':
				ch <- "1"
			case '2':
				ch <- "2"
			case '3':
				ch <- "3"
			case 's', 'S':
				ch <- "split"
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
