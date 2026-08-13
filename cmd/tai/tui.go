package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"iter"
	"os"
	"strings"
	"sync"

	"github.com/gdamore/tcell/v3/color"
	"github.com/gdamore/tcell/v3/tty"
	"github.com/reusee/dscope"
	"github.com/reusee/tai/blocks"
	"github.com/reusee/tai/flags"
	"github.com/reusee/tai/generators"
	"github.com/reusee/tai/logs"
	"github.com/reusee/tai/loops"
	"github.com/reusee/tai/taiui"
)

const (
	maxTUILines   = 10000
	maxTUISignals = 2000

	outputColorUser    int32 = 12
	outputColorTool    int32 = 11
	outputColorSystem  int32 = 14
	outputColorLog     int32 = 9
	outputColorThought int32 = 13
)

const TheoryOfTUI = `
The TUI's content lines, tab state machine, and scroll state are provided
by the taiui library (see taiui.TheoryOfLines, taiui.TheoryOfTabs, and
taiui.TheoryOfScrollState): colored line buffers, wrapped colored lines,
alternating log backgrounds, grouped colored text, tab auto-expansion and
focus order, weighted panel layout, collapsed strips, and follow-tail
scroll offsets. This command wires them with tai-specific capture:
generators.Content is converted to taiui.Line by captureContent, summary
blocks are parsed into Round-tab lines by parseSummaries, and the request
lifecycle is tracked by isGeneratingLog and outputTabLabel.

The TUI interface replaces stdout with a three-tab terminal UI: the Output
tab streams the model output, the Round tab collects the round completion
signals — the bodies of summary blocks and the finish reasons ("[Finish: ...]")
— and the Logs tab collects log records. The Logs tab renders consecutive
lines with alternating background shades so entries are visually distinct;
the two shades derive from the tab's focused or unfocused background, so the
alternation stays subtle in either state. Model output is captured from the
generation state by the tuiOutputState decorator, passed through
RunOptions.StateDecorators by runWithTUI: text parts stream to the Output
tab, thoughts are colored distinctly and separated from non-thought content
by a blank line, tool calls render as markers, and finish reasons are
read directly from the state's FinishReason parts. Only content appended
after the decorator wraps the state is displayed; initial contents are not
re-parsed or re-displayed, because unstructured text must not be
imperfectly parsed. The one exception is the user's chat input: runWithTUI
writes the flags.Chats content to the Output tab in the user role color
before the command starts, so the user sees what the model was asked even
though the chat lives in the initial state. Summary bodies are
parsed from the streamed text parts, so the TUI never scans rendered text
for "[Finish: ...]" markers and never captures model output through a
stdout pipe; stdout is discarded in TUI mode, while stderr stays visible
in the Output tab. Content is colored by role, matching the non-TUI output
colors (see generators/colors.go): user input is blue, tool calls and
results yellow, system messages cyan, log records red, and thoughts bright
magenta; model output keeps the default foreground. Role colors are ANSI 16
palette colors, so text uses only the standard 16-color SGR codes; only
backgrounds use true-color hex values. Colors are carried per output line
through wrapping, so a wrapped line keeps its role color. The keys
1, 2, and 3 select the corresponding tab (Output, Round, Logs respectively):
pressing a focused tab's key collapses it to a thin strip showing the tab's
key and title, and moves the focus to the expanded tab that was last focused
(see the focus-order paragraph below); pressing a non-focused or collapsed
tab's key expands it (if collapsed) and takes the focus. Switching to an
already-expanded tab keeps its current view; re-expanding a collapsed tab
resumes following the live tail. All tabs are collapsed by default; a
collapsed tab expands automatically the FIRST time content for it arrives —
the Output tab on any streamed output, the Round tab on a parsed summary
block or a finish reason, the Logs tab on any log record — so the interface
surfaces panes only when they have something to show. Subsequent content
arrivals do not re-expand a tab the user collapsed. Auto-expansion never
changes an existing focus: a tab popping open cannot steal attention from the
pane the user is reading, and it resumes following the tail; only when
no tab is focused does the first auto-expanded tab become the focus, so
keyboard navigation remains usable. The focused tab occupies twice the space
of each non-focused tab: the expanded tabs share the available space
proportionally to their weights (2 for the focused tab, 1 for every
other, total expanded+1), with the last tab absorbing the rounding
remainder; collapsed tabs take one column (vertical split) or one row
(horizontal split) each. The s key switches between vertical splitting (tabs
side by side, a vertical split line) and horizontal splitting (tabs
stacked, a horizontal split line). The default is horizontal splitting: the
tabs are stacked vertically, one above the other. Tab cycles the focus among
the expanded tabs, skipping collapsed ones; up/down and page up/down scroll
the focused pane; home/end jump to the start/end. Rendering is event-driven:
every path that appends display content — model output captured by the state
decorator, stderr pipe writes, and log records — notifies the render loop,
so streamed output appears live without user input. When the generation
finishes, the TUI stays open so the output can be browsed, and q (or Ctrl-C)
quits the TUI after a confirmation: the first press shows a confirmation bar
at the bottom of the screen, and a second press quits; any other key cancels
the confirmation and is processed normally, so an accidental q press never
loses the session.
`

// Tui enables the terminal UI mode.
type Tui bool

func (Module) Tui() Tui { return false }

var _ generators.State = tuiOutputState{}

func (t Tui) Handle(key string, args []string) (newDef any, remainArgs []string, err error) {
	ret := Tui(true)
	return &ret, args, nil
}

func (t Tui) Keys() map[string]string {
	return map[string]string{"-tui": "Use the TUI interface"}
}

// panelStyle styles the three tab panels: dark blue for unfocused tabs,
// dark gray for the focused tab, and a highlight color for the active
// request label. It is the single style definition shared by the panel
// rendering and the tests.
var panelStyle = taiui.PanelStyle{
	BaseBG:        taiui.HexColor(tabUnfocusBG),
	FocusBG:       taiui.HexColor(tabFocusBG),
	LabelFG:       color.PaletteColor(8),
	FocusLabelFG:  color.PaletteColor(15),
	ActiveLabelFG: color.PaletteColor(int(tabActiveLabelFg)),
}

var (
	outputColorUserLine    = color.PaletteColor(int(outputColorUser))
	outputColorToolLine    = color.PaletteColor(int(outputColorTool))
	outputColorSystemLine  = color.PaletteColor(int(outputColorSystem))
	outputColorLogLine     = color.PaletteColor(int(outputColorLog))
	outputColorThoughtLine = color.PaletteColor(int(outputColorThought))
)

// tuiOutputState is a State decorator that forwards the model's output
// content to the TUI for display. It observes every content appended to
// the generation state and extracts the parts the TUI displays: text and
// thoughts stream to the Output tab (thoughts wrapped in the
// thinking/response markers the terminal Output layer uses), function
// calls and results render as markers, errors are shown inline, and
// finish reasons go to the Round tab. It replaces stateFinishReason:
// finish reasons are captured here, not by a separate decorator. See
// TheoryOfTUI.
type tuiOutputState struct {
	upstream generators.State
	tui      *TUI
}

func (s tuiOutputState) AppendContent(content *generators.Content) (generators.State, error) {
	// Forward the content parts to the TUI before propagating to
	// upstream, so the display keeps up with the stream.
	s.tui.captureContent(content)
	newUpstream, err := s.upstream.AppendContent(content)
	if err != nil {
		return nil, err
	}
	return tuiOutputState{upstream: newUpstream, tui: s.tui}, nil
}

func (s tuiOutputState) Contents() iter.Seq[*generators.Content] {
	return s.upstream.Contents()
}

func (s tuiOutputState) Functions() iter.Seq[*generators.Function] {
	return s.upstream.Functions()
}

func (s tuiOutputState) SystemPrompt() string {
	return s.upstream.SystemPrompt()
}

func (s tuiOutputState) Flush() (generators.State, error) {
	newUpstream, err := s.upstream.Flush()
	if err != nil {
		return nil, err
	}
	return tuiOutputState{upstream: newUpstream, tui: s.tui}, nil
}

func (s tuiOutputState) Unwrap() generators.State {
	return s.upstream
}

// tuiState is the display state of the TUI. The content lines, tab state
// machine, and scroll state live in the taiui library; this command wires
// them with tai-specific capture (summary parsing, request lifecycle).
// See TheoryOfTUI.
type tuiState struct {
	mu       sync.Mutex
	output   *taiui.LineBuffer
	logs     *taiui.StringBuffer
	tabs     *taiui.Tabs
	scrolls  [3]taiui.ScrollState
	signals  []taiui.Line
	parseBuf []byte

	// finished reports whether the generation session has ended. It
	// clears the Output tab's "generating..." hint.
	finished bool
	// confirmQuit reports whether a quit confirmation is pending. The
	// first press of a quit key (q, Q, or Ctrl-C) sets it and shows a
	// confirmation bar at the bottom of the screen; a second quit key
	// press quits, and any other key cancels the confirmation before
	// its normal processing. See TheoryOfTUI.
	confirmQuit bool
	// generating reports whether a generation request is in flight. It
	// is set when the generator's "generating" log record is observed
	// and cleared by a finish line ("[Finish: ...]") or by the session
	// ending. While a request is in flight the Output tab title keeps
	// the "generating..." hint regardless of how long the model is
	// silent (e.g., long thinking phases without streamed output).
	// See TheoryOfTUI.
	generating bool

	// lastOutputRole is the role of the last content written to the
	// Output tab. It is used with lastWasThought to insert a blank line
	// separator when the output switches roles or switches between
	// thinking and non-thinking content. It is only accessed by the
	// generation goroutine via captureContent, so it is never accessed
	// concurrently. See TheoryOfTUI.
	lastOutputRole generators.Role
	// lastWasThought reports whether the last content written to the
	// Output tab was a thought. See lastOutputRole.
	lastWasThought bool
	// hasOutput reports whether any content has been written to the
	// Output tab. It is false until the first part is written, so the
	// first output never gets a leading blank line separator.
	hasOutput bool
}

// write appends uncolored output (stderr, panics) to the Output tab.
func (s *tuiState) write(p []byte) {
	s.writeColored(taiui.NoColor, p)
}

// writeColored appends output with the given display color, extracting
// summary blocks and collecting finish lines into the Round tab's
// signals. A collapsed Output tab expands automatically on the first
// streamed output. The color is carried per line, so wrapped lines keep
// their role color. See TheoryOfTUI.
func (s *tuiState) writeColored(color taiui.Color, p []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(p) == 0 {
		return
	}
	if s.tabs.AutoExpand(0) {
		s.scrolls[0].Follow = true
	}
	// Summary blocks are parsed before finish signals so signals sharing
	// one stream chunk keep their chronological order: the model's
	// summary block precedes the round's finish line. See TheoryOfTUI.
	s.parseSummaries(p)
	s.output.Append(color, string(p))
}

func (s *tuiState) finishReason(reason string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if reason == "" {
		return
	}
	if s.tabs.AutoExpand(1) {
		s.scrolls[1].Follow = true
	}
	// The finish reason marks the end of the generation request: the
	// Output tab's "generating..." hint clears once the request has
	// returned. A new request's "generating" log re-sets it.
	// See TheoryOfTUI.
	s.generating = false
	s.signals = append(s.signals, taiui.Line{Text: "[Finish: " + reason + "]", Color: outputColorLogLine})
	if len(s.signals) > maxTUISignals {
		s.signals = append([]taiui.Line(nil), s.signals[len(s.signals)-maxTUISignals:]...)
	}
}

// writeLogs appends log output to the logs buffer, splitting it into lines
// and retaining the incomplete trailing line for the next chunk. The
// generator logs a record at the start of each request; detecting it
// marks the request as in flight, so the Output tab's generating hint
// appears before the first output byte and persists for the whole
// request — including silent thinking phases. A collapsed Logs tab
// expands automatically on the first log record. See TheoryOfTUI.
func (s *tuiState) writeLogs(p []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(p) == 0 {
		return
	}
	if s.tabs.AutoExpand(2) {
		s.scrolls[2].Follow = true
	}
	for _, line := range s.logs.Append(p) {
		if isGeneratingLog(line) {
			s.generating = true
		}
	}
}

// isGeneratingLog reports whether a log line marks the start of a
// generation request. The generators package logs a record with message
// "generating" at the start of every API request, before any output is
// streamed (see gemini.go and open_ai.go). slog's TextHandler renders
// the message value bare ("msg=generating") when it contains no spaces
// and quoted (`msg="generating"`) when it does; both forms are accepted.
// The value is required to be followed by a space (the next field) or
// the line end, so a message that merely starts with "generating" is
// not mistaken for the request-start record. See TheoryOfTUI.
func isGeneratingLog(line string) bool {
	return strings.HasSuffix(line, "msg=generating") ||
		strings.Contains(line, "msg=generating ") ||
		strings.HasSuffix(line, `msg="generating"`) ||
		strings.Contains(line, `msg="generating" `)
}

// requesting reports whether a generation request is in flight: the
// session has not finished and the request-start log has been observed
// without a finish line (or the session ending) having cleared it. The
// hint therefore persists for the whole request — including silent
// thinking phases without streamed output — and clears when the request
// returns. Callers must hold mu (render does) or be single-threaded.
// See TheoryOfTUI.
func (s *tuiState) requesting() bool {
	if s.finished {
		return false
	}
	return s.generating
}

// outputTabLabel returns the Output tab's title with the session-state
// hint: "Output (generating...)" while the model is actively generating,
// "Output (done)" after the session ends, and "Output" otherwise. The
// highlight result reports whether the title should be drawn in
// tabActiveLabelFg. Callers must hold mu (render does) or be
// single-threaded. See TheoryOfTUI.
func (s *tuiState) outputTabLabel() (label string, highlight bool) {
	label = tabNames[0]
	switch {
	case s.finished:
		label = "Output (done)"
	case s.requesting():
		label = "Output (generating...)"
		highlight = true
	}
	return
}

func (s *tuiState) parseSummaries(p []byte) {
	s.parseBuf = append(s.parseBuf, p...)
	for {
		block, _, end, ok, err := blocks.ParseFirstBlock(s.parseBuf)
		if err != nil {
			// An unclosed or malformed block may still be streaming (its
			// closing line has not arrived yet), so the buffer is kept while
			// no complete block exists beyond the fragment's opening line.
			// When a complete block does exist beyond it, the fragment cannot
			// be a live block — its closing line would have arrived before
			// that block's opening marker. Such a fragment is a truncated
			// round, a malformed block, or prose that merely resembles an
			// opener; skipping it un-wedges the buffer so summaries emitted
			// after it are still extracted. See TheoryOfTUI.
			if end > 0 && end < len(s.parseBuf) {
				if _, _, _, tailOK, tailErr := blocks.ParseFirstBlock(s.parseBuf[end:]); tailErr == nil && tailOK {
					s.parseBuf = s.parseBuf[end:]
					continue
				}
			}
			return // still streaming: wait for more output
		}
		if !ok {
			// No block marker in the buffer. A block opener can be split
			// across chunk boundaries at any byte position, so a trailing
			// line that could become an opener ("<" or "<<"-prefixed) is
			// retained until the next chunk completes it; otherwise the
			// buffer holds only prose and is cleared. See TheoryOfTUI.
			retain := 0
			tail := s.parseBuf
			if i := bytes.LastIndexByte(s.parseBuf, '\n'); i >= 0 {
				tail = s.parseBuf[i+1:]
			}
			if len(tail) == 1 && tail[0] == '<' ||
				len(tail) >= 2 && tail[0] == '<' && tail[1] == '<' {
				retain = len(tail)
			}
			if retain > 0 {
				s.parseBuf = s.parseBuf[len(s.parseBuf)-retain:]
			} else {
				s.parseBuf = nil
			}
			return
		}
		if block.Kind == "summary" {
			body := strings.TrimSpace(block.Body)
			if body != "" {
				// A collapsed Round tab expands automatically on the first
				// summary block; the output text carrying the block
				// already expanded the Output tab. See TheoryOfTUI.
				if s.tabs.AutoExpand(1) {
					s.scrolls[1].Follow = true
				}
				for _, line := range strings.Split(body, "\n") {
					s.signals = append(s.signals, taiui.Line{Text: line})
				}
				s.signals = append(s.signals, taiui.Line{})
				if len(s.signals) > maxTUISignals {
					s.signals = append([]taiui.Line(nil), s.signals[len(s.signals)-maxTUISignals:]...)
				}
			}
		}
		s.parseBuf = s.parseBuf[end:]
		if len(s.parseBuf) == 0 {
			return
		}
	}
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
			output: taiui.NewLineBuffer(maxTUILines),
			logs:   taiui.NewStringBuffer(maxTUILines),
			tabs:   taiui.NewTabs(3),
			// All tabs are collapsed by default; a tab expands automatically
			// the first time content for it arrives, without changing the
			// focus. The scroll offsets start at the tail sentinel so the
			// first render sticks to the latest content. See TheoryOfTUI.
			scrolls: [3]taiui.ScrollState{
				{Offset: 1 << 30},
				{Offset: 1 << 30},
				{Offset: 1 << 30},
			},
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

// captureContent forwards the visible parts of a content to the TUI,
// coloring them by role to match the non-TUI output colors.
// Text parts stream to the Output tab colored by the content role;
// thoughts stream colored distinctly, separated from non-thought
// content by a blank line; function calls, call results, and errors
// render as markers colored by role; finish reasons go to the Round
// tab colored as log lines. Internal metadata parts (Usage) are
// skipped. It is called from the generation goroutine via
// tuiOutputState.AppendContent, the only goroutine that reads or
// writes the output-section state (lastOutputRole, lastWasThought,
// hasOutput). See TheoryOfTUI.
func (t *TUI) captureContent(content *generators.Content) {
	role := content.Role
	for _, part := range content.Parts {
		switch p := part.(type) {
		case generators.Text:
			if len(p) > 0 {
				t.writeOutputPart(role, roleColor(role), false, string(p))
			}
		case generators.Thought:
			if len(p) > 0 {
				t.writeOutputPart(role, outputColorThoughtLine, true, string(p))
			}
		case generators.FuncCall:
			t.writeOutputPart(role, roleColor(role), false, fmt.Sprintf("[Function Call: %s(%v)]", p.Name, p.Arguments))
		case generators.CallResult:
			t.writeOutputPart(role, roleColor(role), false, fmt.Sprintf("[Call Result: %s(%v)]", p.Name, p.Results))
		case generators.FinishReason:
			t.finishReason(string(p))
		case generators.Error:
			if p.Error != nil {
				t.writeOutputPart(role, roleColor(role), false, fmt.Sprintf("[Error: %v]", p.Error))
			}
		}
	}

	// Notify the render loop that new output is available. Captured
	// content appends directly to the display buffers, bypassing the
	// tuiWriter path that notifies; without a notification the render
	// loop stays blocked on the update channel and the pane appears
	// frozen until an input key forces a re-render. See TheoryOfTUI.
	t.notify()
}

// writeOutputPart writes one output part to the Output tab, inserting a
// blank line separator when the output switches roles or switches between
// thinking and non-thinking content. The section state (hasOutput,
// lastOutputRole, lastWasThought) is only accessed by the generation
// goroutine via captureContent, so it is read and written without a lock.
func (t *TUI) writeOutputPart(role generators.Role, color taiui.Color, isThought bool, text string) {
	if t.hasOutput && (role != t.lastOutputRole || isThought != t.lastWasThought) {
		t.separateOutput()
	}
	t.writeColored(color, []byte(text))
	t.lastOutputRole = role
	t.lastWasThought = isThought
	t.hasOutput = true
}

// separateOutput writes a blank line separator between different output
// sections. A single newline creates a blank line when the previous
// output ended with a newline; otherwise two newlines are needed to
// produce an empty line. It locks the state because the output buffer is
// shared with the stderr writer.
func (t *TUI) separateOutput() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.output.HasPartial() {
		t.output.Append(taiui.NoColor, "\n\n")
	} else {
		t.output.Append(taiui.NoColor, "\n")
	}
}

// roleColor maps a content role to the display color used in the TUI,
// matching the non-TUI output colors in generators/colors.go. Model and
// assistant content keep the default foreground; user input, tool calls
// and results, system messages, and log records get their role colors.
func roleColor(role generators.Role) taiui.Color {
	switch role {
	case generators.RoleUser:
		return outputColorUserLine
	case generators.RoleTool:
		return outputColorToolLine
	case generators.RoleSystem:
		return outputColorSystemLine
	case generators.RoleLog:
		return outputColorLogLine
	default:
		return taiui.NoColor
	}
}

func (t *TUI) notify() {
	select {
	case t.updateCh <- struct{}{}:
	default:
	}
}

// toggleTab implements the number-key semantics (keys 1, 2, 3): pressing
// a tab's key toggles its expansion. The state machine lives in
// taiui.Tabs; expanding a collapsed tab resumes following the live tail.
// See taiui.TheoryOfTabs.
func (t *TUI) toggleTab(idx int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.tabs.Toggle(idx) {
		t.scrolls[idx].Follow = true
	}
}

// cycleFocus advances the focus to the next expanded tab after the
// current one, wrapping around. Collapsed tabs are skipped.
func (t *TUI) cycleFocus() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.tabs.CycleFocus()
}

// handleQuitKey processes a quit key press (q, Q, or Ctrl-C). The
// first press sets confirmQuit, which shows a confirmation bar at the
// bottom of the screen; the second press confirms the quit. It returns
// true when the TUI should quit. Any other key cancels the
// confirmation via cancelConfirmQuit. See TheoryOfTUI.
func (t *TUI) handleQuitKey() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.confirmQuit {
		return true
	}
	t.confirmQuit = true
	return false
}

// cancelConfirmQuit cancels a pending quit confirmation. Every key
// other than a quit key calls it before its normal processing, so an
// accidental q press is undone by the next key. See TheoryOfTUI.
func (t *TUI) cancelConfirmQuit() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.confirmQuit = false
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
	go taiui.ReadKeys(t.tty, keyCh)

	go func() {
		defer func() {
			if r := recover(); r != nil {
				t.mu.Lock()
				t.runErr = fmt.Errorf("panic: %v", r)
				t.mu.Unlock()
				t.write([]byte(fmt.Sprintf("[panic] %v\n", r)))
			}
			t.mu.Lock()
			// The session has ended: clear the in-flight hint with the
			// finished state. A request that returned without a finish
			// line (e.g., an error path) must not leave the hint stuck
			// on. See TheoryOfTUI.
			t.finished = true
			t.generating = false
			t.mu.Unlock()
			t.notify()
		}()
		gen()
	}()

	for {
		t.render()
		select {
		case key := <-keyCh:
			// Any key other than a quit key cancels a pending quit
			// confirmation before its normal processing, so an
			// accidental q press never loses the session. See
			// TheoryOfTUI.
			if key != "quit" {
				t.cancelConfirmQuit()
			}
			switch key {
			case "tab":
				t.cycleFocus()
			case "1":
				t.toggleTab(0)
			case "2":
				t.toggleTab(1)
			case "3":
				t.toggleTab(2)
			case "split":
				t.mu.Lock()
				t.tabs.SplitVertical = !t.tabs.SplitVertical
				t.mu.Unlock()
			case "up":
				t.scroll(-1)
			case "down":
				t.scroll(1)
			case "pageup":
				t.pageScroll(-1)
			case "pagedown":
				t.pageScroll(1)
			case "home":
				t.scrollTo(0)
			case "end":
				t.scrollTo(1 << 30)
			case "quit":
				// The first quit key press shows a confirmation bar; a
				// second press confirms the quit. See TheoryOfTUI.
				if t.handleQuitKey() {
					io.WriteString(t.tty, "\x1b[0m")
					fmt.Fprintf(t.tty, "\x1b[%d;1H", t.height)
					io.WriteString(t.tty, "\x1b[?25h")
					t.mu.Lock()
					err := t.runErr
					t.mu.Unlock()
					return err
				}
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
	idx := t.tabs.Focus
	if idx < 0 || !t.tabs.Expanded[idx] {
		return
	}
	t.scrolls[idx].Scroll(delta)
}

// pageScroll scrolls the focused pane by one page. The page size is the
// pane's scroll view height minus one row, so one line of the previous
// view remains on screen: page down keeps the previous last row at the
// top, page up keeps the previous first row at the bottom. The page
// size is derived from the focused pane's actual layout, so in stacked
// (horizontal split) mode it matches the pane height rather than the
// full terminal height.
func (t *TUI) pageScroll(direction int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	idx := t.tabs.Focus
	if idx < 0 || !t.tabs.Expanded[idx] {
		return
	}
	boxes := t.tabs.Boxes(t.width, t.height)
	box := boxes[idx]
	if box.Width() <= 0 || box.Height() <= 0 {
		return
	}
	// The scroll view is the panel box minus the one-row label strip.
	paneHeight := max(box.Height()-1, 1)
	t.scrolls[idx].PageScroll(direction, paneHeight)
}

func (t *TUI) scrollTo(top int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	idx := t.tabs.Focus
	if idx < 0 || !t.tabs.Expanded[idx] {
		return
	}
	// Only a jump to the latest row (the end key reaches it via the sentinel)
	// resumes following; any other jump stops it. See TheoryOfTUI.
	t.scrolls[idx].ScrollTo(top)
}

func (t *TUI) render() {
	t.mu.Lock()
	defer t.mu.Unlock()

	outputLines := t.output.Lines()
	logsLines := t.logs.Lines()
	height := t.height
	if height < 1 {
		height = 1
	}
	width := t.width
	if width < 1 {
		width = 1
	}

	// Compute the panel box of each tab. Collapsed tabs take one column
	// (vertical split) or one row (horizontal split); expanded tabs share
	// the remaining space proportionally to their weights. See
	// taiui.Tabs.Boxes and TheoryOfTUI.
	panelBoxes := t.tabs.Boxes(width, height)

	// Render each tab: expanded tabs show the label strip and scroll
	// view; collapsed tabs show a thin strip with the key and title.
	// The focused tab occupies twice the space of each non-focused tab.
	// See TheoryOfTUI.
	//
	// The logs tab alternates line backgrounds derived from its tab
	// background, so consecutive log entries are visually distinct. The
	// base is the focused or unfocused tab background, whichever the
	// logs tab currently has. See taiui.PlainLines.
	logsBase := panelStyle.BaseBG
	if t.tabs.Focus == 2 {
		logsBase = panelStyle.FocusBG
	}
	contentByTab := [3][]taiui.Line{
		outputLines,
		t.signals,
		taiui.PlainLines(logsLines, logsBase),
	}
	var panels []taiui.Element
	for idx := 0; idx < 3; idx++ {
		box := panelBoxes[idx]
		if box.Width() <= 0 || box.Height() <= 0 {
			continue
		}
		if !t.tabs.Expanded[idx] {
			panels = append(panels, taiui.CollapsedPanel(
				box,
				fmt.Sprintf("%d %s", idx+1, tabNames[idx]),
				t.tabs.Focus == idx,
				panelStyle,
			))
			continue
		}
		// Each tab's content width reserves one column for its scrollbar,
		// matching the scroll's visible-width rendering. In the weighted
		// layout the tabs have different widths — the focused tab is twice
		// as wide as the others — so each tab's content wraps at its own
		// width. See TheoryOfTUI.
		tabContentWidth := max(box.Width()-1, 1)
		display := taiui.WrapLinesColored(contentByTab[idx], tabContentWidth)

		// The scroll offset is clamped against the ACTUAL scroll view height
		// — the panel height minus the one-row label strip. In horizontal
		// split (stacked) each panel is height/N tall, so its scroll view is
		// far shorter than a full-height pane; clamping against a full-height
		// pane would stop the scroll before the content tail became visible.
		// While a tab follows the latest content, its offset is stuck to the
		// newest row before clamping; a view manually placed at the latest
		// row resumes following. See TheoryOfTUI.
		paneHeight := max(box.Height()-1, 1)
		t.scrolls[idx].Update(len(display), paneHeight)

		label := tabNames[idx]
		highlight := false
		if idx == 0 {
			// The Output tab's title carries the session-state hint:
			// "generating..." while the model is actively working and
			// "(done)" after the session ends. The generating hint also
			// switches the title to the active-request highlight color.
			// See TheoryOfTUI.
			label, highlight = t.outputTabLabel()
		}
		panels = append(panels, taiui.Panel(
			box,
			label,
			highlight,
			display,
			t.scrolls[idx].Offset,
			t.tabs.Focus == idx,
			t.scrolls[idx].Follow,
			panelStyle,
		))
	}

	overlaySpecs := make([]any, len(panels))
	for i, p := range panels {
		overlaySpecs[i] = p
	}
	if t.confirmQuit {
		// A pending quit confirmation draws a confirmation bar over the
		// bottom row of the screen, on top of every tab, so it is always
		// visible. The first quit key press sets confirmQuit; a second
		// quit key press quits, and any other key cancels. See
		// TheoryOfTUI.
		overlaySpecs = append(overlaySpecs, taiui.Rect(
			taiui.Box{Top: height - 1, Left: 0, Bottom: height, Right: width},
			taiui.Fill(true),
			taiui.BGColor(taiui.HexColor(0x800000)),
			taiui.Bold(true),
			taiui.Text(" Quit? Press q again to confirm, any other key to cancel "),
		))
	}
	root := taiui.Root{Element: taiui.Overlay(overlaySpecs...)}
	taiui.Render(taiui.NewBaseScope(func() taiui.Root { return root }), t.screen)
}

var (
	tabNames = [...]string{"Output", "Round", "Logs"}
	// tabUnfocusBG is the dark blue background of every unfocused tab.
	tabUnfocusBG int32 = 0x0a1428
	// tabFocusBG is the dark gray background of the focused tab.
	tabFocusBG       int32 = 0x2e2e2e
	tabActiveLabelFg int32 = 10
)

// withTUIOutputObserver wraps a loops.Run so that model output content
// appended to the generation state is forwarded to the TUI. The
// decorator is passed through RunOptions.StateDecorators, so the loop
// applies it to the generation state before the phase chain runs. It
// replaces withFinishReasonObserver: output text, thoughts, tool calls,
// and finish reasons are all captured by the same decorator. Only
// content appended after the decorator wraps the state is displayed;
// initial contents are not re-parsed. See TheoryOfTUI.
func withTUIOutputObserver(run loops.Run, tui *TUI) loops.Run {
	return func(ctx context.Context, opts loops.RunOptions) (loops.Result, error) {
		opts.StateDecorators = append(opts.StateDecorators, func(state generators.State) generators.State {
			// The tuiOutputState layer observes only content appended
			// after it wraps the state. Initial contents are not parsed
			// or displayed: unstructured text must not be re-parsed for
			// display. See TheoryOfTUI.
			return tuiOutputState{
				upstream: state,
				tui:      tui,
			}
		})
		return run(ctx, opts)
	}
}

func runWithTUI(command Command, scope dscope.Scope) {
	tui, err := newTUI()
	if err != nil {
		fmt.Fprintf(os.Stderr, "cannot start TUI: %v; continuing without TUI\n", err)
		scope.Fork(command.Defs...).Call(command.Main)
		return
	}
	oldOut, oldErr := os.Stdout, os.Stderr

	// The TUI is the display. Model output is captured from the
	// generation state by the tuiOutputState decorator, and log records
	// are routed to the Logs pane via the forked logs.Writer. Writes to
	// stdout would corrupt the TUI rendering, so they are discarded by
	// redirecting to the null device; writes to stderr (e.g., command
	// error messages) stay visible in the Output tab via a pipe to the
	// TUI writer. The stderr pipe is the only remaining pipe: model
	// output never passes through it. See TheoryOfTUI.
	devNull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		_ = tui.Stop()
		fmt.Fprintf(os.Stderr, "cannot open %s: %v; continuing without TUI\n", os.DevNull, err)
		scope.Fork(command.Defs...).Call(command.Main)
		return
	}
	os.Stdout = devNull
	pr, pw, err := os.Pipe()
	if err != nil {
		_ = devNull.Close()
		_ = tui.Stop()
		fmt.Fprintf(os.Stderr, "cannot create stderr pipe: %v; continuing without TUI\n", err)
		scope.Fork(command.Defs...).Call(command.Main)
		return
	}
	os.Stderr = pw
	stderrDone := make(chan struct{})
	go func() {
		defer close(stderrDone)
		_, _ = io.Copy(tui.Writer(), pr)
	}()

	// The state decorator wraps the generation state so model output —
	// text, thoughts, tool calls, finish reasons — is read directly from
	// the state and forwarded to the TUI. This replaces the previous
	// stdout-pipe capture and finish-reason observer: the TUI no longer
	// captures model output through a pipe, and the Round tab never
	// scans rendered output for "[Finish: ...]" markers. The decorator
	// is passed through RunOptions.StateDecorators by wrapping the
	// loops.Run provider: the original Run is resolved before the fork,
	// and the wrapper appends the decorator to the options before
	// delegating. See TheoryOfTUI.
	originalRun := dscope.Get[loops.Run](scope)
	scope = scope.Fork(
		func() logs.Writer { return logs.Writer(tui.LogsWriter()) },
		func() loops.Run {
			return withTUIOutputObserver(originalRun, tui)
		},
	)
	// Display the user's chat input (flags.Chats) at the top of the
	// Output tab before the command starts generating. The chat lives in
	// the initial generation state, which the tuiOutputState decorator
	// does not display; writing it here gives the user a clear view of
	// what the model was asked. See TheoryOfTUI.
	displayChatInput(tui, dscope.Get[flags.Chats](scope))
	runErr := tui.Run(func() {
		scope.Fork(command.Defs...).Call(command.Main)
	})
	os.Stdout, os.Stderr = oldOut, oldErr
	_ = pw.Close()
	<-stderrDone
	_ = pr.Close()
	_ = devNull.Close()
	if runErr != nil {
		fmt.Fprintf(oldErr, "%v\n", runErr)
		os.Exit(1)
	}
}

// displayChatInput writes the user's chat input (flags.Chats) to the
// Output tab, colored as user input. The chat content lives in the
// initial generation state, which the tuiOutputState decorator does not
// display (only content appended after the decorator wraps the state is
// shown). Writing it before the command starts gives the user a clear
// view of what the model was asked. See TheoryOfTUI.
func displayChatInput(tui *TUI, chats flags.Chats) {
	if len(chats) == 0 {
		return
	}
	tui.writeColored(outputColorUserLine, []byte(strings.Join(chats, "\n")+"\n"))
}
