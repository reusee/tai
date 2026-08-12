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
	"time"

	"github.com/gdamore/tcell/v3/color"
	"github.com/gdamore/tcell/v3/tty"
	"github.com/reusee/dscope"
	"github.com/reusee/tai/blocks"
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
The TUI interface replaces stdout with a three-tab terminal UI: the Output
tab streams the model output, the Round tab collects the round completion
signals — the bodies of summary blocks and the finish reasons ("[Finish: ...]")
— and the Logs tab collects log records. Model output is captured from the
generation state by the tuiOutputState decorator, passed through
RunOptions.StateDecorators by runWithTUI: text parts stream to the Output
tab, thoughts are colored distinctly and separated from non-thought content
by a blank line, tool calls render as markers, and finish reasons are
read directly from the state's FinishReason parts. Initial contents of
the generation state — the user's chat input and any plain-text content —
are captured when the decorator is applied, so the user's input appears in
the Output tab alongside the model output; file-context blocks wrapped
with the file begin/end markers are skipped so the code context does not
flood the tab. Summary bodies are
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
finishes, the TUI stays open so the output can be browsed, and q quits the
TUI.
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

type (
	// outputLine is one source line of the Output or Round tab, with the
	// display color carried from its content role.
	outputLine struct {
		text  string
		color int32
	}

	// displayLine is one wrapped display line, carrying the color of the
	// source line it came from.
	displayLine struct {
		text  string
		color int32
	}
)

type tuiState struct {
	mu          sync.Mutex
	lines       []outputLine
	partial     outputLine
	signals     []outputLine
	logs        []string
	logsPartial string
	parseBuf    []byte

	// expanded reports whether each tab is expanded: index 0 is the
	// output tab, 1 the round tab, 2 the logs tab. The keys 1, 2, and 3
	// toggle them; a collapsed tab expands automatically the first time
	// content for it arrives, without changing the focus. splitVertical
	// reports whether the tabs are laid out side by side (vertical
	// split line) or stacked (horizontal split line); the s key toggles
	// it. See TheoryOfTUI.
	expanded [3]bool
	// hasContent reports whether each tab has ever received content.
	// Only the first content arrival auto-expands a collapsed tab;
	// subsequent content does not re-expand a tab the user collapsed.
	// See TheoryOfTUI.
	hasContent [3]bool
	// lastFocus records the focus order of each tab: a monotonically
	// increasing counter assigned when a tab gains focus. When a tab
	// collapses, focus moves to the expanded tab with the most recent
	// lastFocus value; ties (tabs that were never focused) break by
	// index order. See TheoryOfTUI.
	lastFocus [3]int
	// focusOrder is the counter for lastFocus.
	focusOrder int

	splitVertical bool

	// view state
	topLeft  int
	topRight int
	topLogs  int
	focus    int // 0 = output, 1 = round, 2 = logs, -1 = none expanded
	finished bool

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

	// follow reports whether each tab's view sticks to the latest content.
	// It starts true; manual scrolling away clears it, and reaching the
	// latest row sets it again. See TheoryOfTUI.
	follow [3]bool

	// maxOffsets holds the maximum scroll offset of each tab computed at the
	// last render. scroll and scrollTo use it to decide whether the user's
	// view is at the latest row.
	maxOffsets [3]int
}

// write appends uncolored output (stderr, panics) to the Output tab.
func (s *tuiState) write(p []byte) {
	s.writeColored(0, p)
}

// writeColored appends output with the given display color, extracting
// summary blocks and collecting finish lines into the Round tab's
// signals. A collapsed Output tab expands automatically on the first
// streamed output. The color is carried per line, so wrapped lines keep
// their role color. See TheoryOfTUI.
func (s *tuiState) writeColored(color int32, p []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(p) == 0 {
		return
	}
	s.autoExpand(0)
	// Summary blocks are parsed before finish signals so signals sharing
	// one stream chunk keep their chronological order: the model's
	// summary block precedes the round's finish line. See TheoryOfTUI.
	s.parseSummaries(p)
	s.appendOutput(color, p)
}

func (s *tuiState) finishReason(reason string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if reason == "" {
		return
	}
	s.autoExpand(1)
	// The finish reason marks the end of the generation request: the
	// Output tab's "generating..." hint clears once the request has
	// returned. A new request's "generating" log re-sets it.
	// See TheoryOfTUI.
	s.generating = false
	s.signals = append(s.signals, outputLine{text: "[Finish: " + reason + "]", color: outputColorLog})
	if len(s.signals) > maxTUISignals {
		s.signals = append([]outputLine(nil), s.signals[len(s.signals)-maxTUISignals:]...)
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
	s.autoExpand(2)
	s.logsPartial += string(p)
	for {
		idx := strings.IndexByte(s.logsPartial, '\n')
		if idx < 0 {
			break
		}
		line := s.logsPartial[:idx]
		s.logs = append(s.logs, line)
		if isGeneratingLog(line) {
			s.generating = true
		}
		s.logsPartial = s.logsPartial[idx+1:]
		if len(s.logs) > maxTUILines {
			s.logs = append([]string(nil), s.logs[len(s.logs)-maxTUILines:]...)
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

// autoExpand expands a collapsed tab the first time content for it
// arrives. It never changes an existing focus: a tab popping open cannot
// steal attention from the pane the user is reading, and the pane resumes
// following the tail. Only when no tab is focused does the first
// auto-expanded tab become the focus, so keyboard navigation remains
// usable. Subsequent content arrivals do not re-expand a tab the user
// collapsed. Callers must hold mu. See TheoryOfTUI.
func (s *tuiState) autoExpand(idx int) {
	if idx < 0 || idx > 2 {
		return
	}
	// Only the first content arrival auto-expands a collapsed tab;
	// subsequent content does not re-expand a tab the user collapsed.
	if s.hasContent[idx] {
		return
	}
	s.hasContent[idx] = true
	if s.expanded[idx] {
		return
	}
	s.expanded[idx] = true
	s.follow[idx] = true
	if s.focus == -1 {
		s.focus = idx
		s.lastFocus[idx] = s.focusOrder
		s.focusOrder++
	}
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

// appendOutput appends output bytes to the Output tab, splitting them
// into lines. A line keeps the color of the chunk that started it; a
// partial line retains its color until the next newline arrives.
func (s *tuiState) appendOutput(color int32, p []byte) {
	if s.partial.text == "" {
		s.partial.color = color
	}
	s.partial.text += string(p)
	for {
		idx := strings.IndexByte(s.partial.text, '\n')
		if idx < 0 {
			break
		}
		line := s.partial.text[:idx]
		s.lines = append(s.lines, outputLine{text: line, color: s.partial.color})
		s.partial.text = s.partial.text[idx+1:]
		s.partial.color = color
		if len(s.lines) > maxTUILines {
			s.lines = append([]outputLine(nil), s.lines[len(s.lines)-maxTUILines:]...)
		}
	}
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
				s.autoExpand(1)
				for _, line := range strings.Split(body, "\n") {
					s.signals = append(s.signals, outputLine{text: line})
				}
				s.signals = append(s.signals, outputLine{})
				if len(s.signals) > maxTUISignals {
					s.signals = append([]outputLine(nil), s.signals[len(s.signals)-maxTUISignals:]...)
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
func (s *tuiState) outputLinesForRender() []outputLine {
	if s.partial.text == "" {
		return s.lines
	}
	ret := make([]outputLine, 0, len(s.lines)+1)
	ret = append(ret, s.lines...)
	ret = append(ret, s.partial)
	return ret
}

// wrapTabLines wraps each source output line to the given width,
// carrying the line's color onto every wrapped display line.
func wrapTabLines(lines []outputLine, width int) []displayLine {
	var out []displayLine
	for _, line := range lines {
		for _, text := range taiui.WrapLines([]string{line.text}, width) {
			out = append(out, displayLine{text: text, color: line.color})
		}
	}
	return out
}

// plainOutputLines converts plain text lines into uncolored output
// lines.
func plainOutputLines(lines []string) []outputLine {
	out := make([]outputLine, 0, len(lines))
	for _, line := range lines {
		out = append(out, outputLine{text: line})
	}
	return out
}

func coloredText(lines []displayLine, box taiui.Box) taiui.Element {
	if len(lines) == 0 {
		return taiui.Text("")
	}
	var children []any
	start := 0
	for i := 1; i <= len(lines); i++ {
		if i == len(lines) || lines[i].color != lines[start].color {
			count := i - start
			texts := make([]string, 0, count)
			for j := start; j < i; j++ {
				texts = append(texts, lines[j].text)
			}
			groupBox := taiui.Box{Top: box.Top + start, Left: box.Left, Bottom: box.Top + i, Right: box.Right}
			specs := []any{taiui.Box(groupBox), texts}
			if lines[start].color != 0 {
				specs = append(specs, taiui.FGColor(color.PaletteColor(int(lines[start].color))))
			}
			children = append(children, taiui.Text(specs...))
			start = i
		}
	}
	return taiui.Overlay(children...)
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
			// All tabs are collapsed by default; a tab expands automatically
			// the first time content for it arrives, without changing the
			// focus. See TheoryOfTUI.
			expanded:   [3]bool{false, false, false},
			hasContent: [3]bool{false, false, false},
			lastFocus:  [3]int{-1, -1, -1},
			focusOrder: 0,
			// Tabs are stacked vertically by default (horizontal split);
			// the s key toggles to side-by-side (vertical split).
			// See TheoryOfTUI.
			splitVertical: false,
			focus:         -1,
			topLeft:       1 << 30,
			topRight:      1 << 30,
			topLogs:       1 << 30,
			follow:        [3]bool{false, false, false},
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
				t.writeOutputPart(role, outputColorThought, true, string(p))
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
func (t *TUI) writeOutputPart(role generators.Role, color int32, isThought bool, text string) {
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
	if t.partial.text == "" {
		t.appendOutput(0, []byte("\n"))
	} else {
		t.appendOutput(0, []byte("\n\n"))
	}
}

// roleColor maps a content role to the display color used in the TUI,
// matching the non-TUI output colors in generators/colors.go. Model and
// assistant content keep the default foreground; user input, tool calls
// and results, system messages, and log records get their role colors.
func roleColor(role generators.Role) int32 {
	switch role {
	case generators.RoleUser:
		return outputColorUser
	case generators.RoleTool:
		return outputColorTool
	case generators.RoleSystem:
		return outputColorSystem
	case generators.RoleLog:
		return outputColorLog
	default:
		return 0
	}
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

// toggleTab implements the number-key semantics (keys 1, 2, 3): pressing
// a tab's key toggles its expansion. When the tab is focused, it is
// collapsed to a thin strip and the focus moves to the expanded tab that
// was last focused (see focusLastExpanded). When it is not focused (or
// collapsed), it is expanded (if collapsed) and becomes the focus.
// Re-expanding a collapsed tab resumes following the live tail, even if
// the user had scrolled away before collapsing it; switching to an
// already-expanded tab keeps its current view. See TheoryOfTUI.
func (t *TUI) toggleTab(idx int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.focus == idx {
		// A focused tab's key collapses it and moves the focus to the
		// expanded tab that was last focused.
		t.expanded[idx] = false
		t.focusLastExpanded()
		return
	}
	if !t.expanded[idx] {
		// Expanding a collapsed tab resumes following the live tail, even
		// if the user had scrolled away before collapsing it: the pane
		// returns to the latest content. See TheoryOfTUI.
		t.expanded[idx] = true
		t.follow[idx] = true
	}
	// A non-focused tab's key (expanded or collapsed) makes it the focus;
	// switching to an already-expanded tab keeps its current view.
	t.focus = idx
	t.lastFocus[idx] = t.focusOrder
	t.focusOrder++
}

// focusLastExpanded moves the focus to the expanded tab that was last
// focused, based on the lastFocus order. Tabs that were never focused
// (lastFocus -1) tie-break by index order. When no tab is expanded, the
// focus becomes -1. See TheoryOfTUI.
func (t *TUI) focusLastExpanded() {
	best := -1
	bestOrder := -2
	for i := 0; i < 3; i++ {
		if !t.expanded[i] {
			continue
		}
		if t.lastFocus[i] > bestOrder {
			bestOrder = t.lastFocus[i]
			best = i
		}
	}
	t.focus = best
}

// cycleFocus advances the focus to the next expanded tab after the
// current one, wrapping around. Collapsed tabs are skipped. When no tab
// is expanded, the focus becomes -1. The new focus's lastFocus is
// updated so a later collapse returns to the most recently focused tab.
// See TheoryOfTUI.
func (t *TUI) cycleFocus() {
	if t.focus >= 0 {
		for i := 1; i <= 3; i++ {
			f := (t.focus + i) % 3
			if t.expanded[f] {
				t.focus = f
				t.lastFocus[f] = t.focusOrder
				t.focusOrder++
				return
			}
		}
	}
	for i := 0; i < 3; i++ {
		if t.expanded[i] {
			t.focus = i
			t.lastFocus[i] = t.focusOrder
			t.focusOrder++
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

// computeTabBoxes computes the panel box of each tab. In vertical split
// (side by side), collapsed tabs take one column each and expanded tabs
// share the remaining width proportionally to their weights (the focused
// tab has weight 2, every other expanded tab weight 1); in horizontal
// split (stacked), collapsed tabs take one row each and expanded tabs
// share the remaining height. The last expanded tab absorbs the rounding
// remainder. Tabs are laid out in index order, so a collapsed tab stays
// in its original position rather than being pushed to the edge.
// See TheoryOfTUI.
func computeTabBoxes(splitVertical bool, expanded [3]bool, focused int, width, height int) [3]taiui.Box {
	var boxes [3]taiui.Box

	// Collect expanded tab indices in order and compute the total weight.
	var expandedIndices []int
	totalWeight := 0
	for i := 0; i < 3; i++ {
		if expanded[i] {
			expandedIndices = append(expandedIndices, i)
			weight := 1
			if i == focused {
				weight = 2
			}
			totalWeight += weight
		}
	}
	collapsedCount := 3 - len(expandedIndices)
	if totalWeight <= 0 {
		totalWeight = 1
	}

	if splitVertical {
		// Collapsed tabs take one column each; expanded tabs share the
		// remaining width.
		expandedWidth := width - collapsedCount
		if expandedWidth < 0 {
			expandedWidth = 0
		}
		edge := 0
		expandedEdge := 0
		expandedPos := 0
		for i := 0; i < 3; i++ {
			if expanded[i] {
				weight := 1
				if i == focused {
					weight = 2
				}
				var size int
				if expandedPos == len(expandedIndices)-1 {
					// The last expanded tab absorbs the rounding remainder.
					size = expandedWidth - expandedEdge
				} else {
					size = expandedWidth * weight / totalWeight
				}
				boxes[i] = taiui.Box{Top: 0, Left: edge, Bottom: height, Right: edge + size}
				edge += size
				expandedEdge += size
				expandedPos++
			} else {
				// A collapsed tab stays in its original position, taking
				// one column.
				boxes[i] = taiui.Box{Top: 0, Left: edge, Bottom: height, Right: edge + 1}
				edge++
			}
		}
		return boxes
	}

	// Horizontal split: collapsed tabs take one row each; expanded tabs
	// share the remaining height.
	expandedHeight := height - collapsedCount
	if expandedHeight < 0 {
		expandedHeight = 0
	}
	edge := 0
	expandedEdge := 0
	expandedPos := 0
	for i := 0; i < 3; i++ {
		if expanded[i] {
			weight := 1
			if i == focused {
				weight = 2
			}
			var size int
			if expandedPos == len(expandedIndices)-1 {
				size = expandedHeight - expandedEdge
			} else {
				size = expandedHeight * weight / totalWeight
			}
			boxes[i] = taiui.Box{Top: edge, Left: 0, Bottom: edge + size, Right: width}
			edge += size
			expandedEdge += size
			expandedPos++
		} else {
			// A collapsed tab stays in its original position, taking one
			// row.
			boxes[i] = taiui.Box{Top: edge, Left: 0, Bottom: edge + 1, Right: width}
			edge++
		}
	}
	return boxes
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

	// Compute the panel box of each tab. Collapsed tabs take one column
	// (vertical split) or one row (horizontal split); expanded tabs share
	// the remaining space proportionally to their weights. See
	// computeTabBoxes and TheoryOfTUI.
	panelBoxes := computeTabBoxes(t.splitVertical, t.expanded, t.focus, width, height)

	// Render each tab: expanded tabs show the label strip and scroll
	// view; collapsed tabs show a thin strip with the key and title.
	// The focused tab occupies twice the space of each non-focused tab.
	// See TheoryOfTUI.
	contentByTab := [3][]outputLine{
		outputLines,
		t.signals,
		plainOutputLines(logsLines),
	}
	displays := [3][]displayLine{}
	tops := [3]int{t.topLeft, t.topRight, t.topLogs}
	var maxOffsets [3]int
	var panels []taiui.Element
	for idx := 0; idx < 3; idx++ {
		box := panelBoxes[idx]
		if box.Width() <= 0 || box.Height() <= 0 {
			continue
		}
		if !t.expanded[idx] {
			panels = append(panels, t.collapsedPanel(idx, box, t.focus == idx))
			continue
		}
		// Each tab's content width reserves one column for its scrollbar,
		// matching the scroll's visible-width rendering. In the weighted
		// layout the tabs have different widths — the focused tab is twice
		// as wide as the others — so each tab's content wraps at its own
		// width. See TheoryOfTUI.
		tabContentWidth := max(box.Width()-1, 1)
		displays[idx] = wrapTabLines(contentByTab[idx], tabContentWidth)

		// The scroll offset is clamped against the ACTUAL scroll view height
		// — the panel height minus the one-row label strip. In horizontal
		// split (stacked) each panel is height/N tall, so its scroll view is
		// far shorter than a full-height pane; clamping against a full-height
		// pane would stop the scroll before the content tail became visible.
		// While a tab follows the latest content, its offset is stuck to the
		// newest row before clamping; a view manually placed at the latest
		// row resumes following. See TheoryOfTUI.
		paneHeight := max(box.Height()-1, 1)
		maxOffsets[idx] = max(len(displays[idx])-paneHeight, 0)
		if t.follow[idx] {
			tops[idx] = maxOffsets[idx]
		}
		tops[idx] = scrollClamp(tops[idx], len(displays[idx]), paneHeight)
		if tops[idx] == maxOffsets[idx] {
			t.follow[idx] = true
		}
		panels = append(panels, t.panel(idx, box, displays[idx], tops[idx], t.focus == idx))
	}
	t.topLeft, t.topRight, t.topLogs = tops[0], tops[1], tops[2]
	t.maxOffsets = maxOffsets

	overlaySpecs := make([]any, len(panels))
	for i, p := range panels {
		overlaySpecs[i] = p
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

func (t *TUI) panel(idx int, box taiui.Box, lines []displayLine, top int, focus bool) taiui.Element {
	// Exactly two background colors: dark gray for the focused tab, dark
	// blue for every unfocused tab. The label strip shares the tab's
	// background; focus is conveyed by the label's foreground and bold
	// weight. See TheoryOfTUI.
	base := tabUnfocusBG
	if focus {
		base = tabFocusBG
	}
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
	labelFg := int32(8) // bright black (gray)
	if highlight {
		labelFg = tabActiveLabelFg
	} else if focus {
		labelFg = 15 // bright white
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

	// The scrollbar is hidden while the pane follows the tail: at the
	// latest position there is nothing left to scroll toward, so the
	// thumb would only add visual noise and waste a column. Scrolling
	// away from the tail brings the scrollbar back. See TheoryOfTUI.
	scrollSpecs := []any{
		taiui.Box(scrollBox),
		taiui.Fill(true),
		taiui.BGColor(taiui.HexColor(base)),
	}
	if !t.follow[idx] {
		scrollSpecs = append(scrollSpecs, taiui.Scrollbar(true))
	}

	return taiui.Overlay(
		taiui.Text(
			"  "+label+"  ",
			taiui.Box(headerBox),
			taiui.Fill(true),
			taiui.BGColor(taiui.HexColor(base)),
			taiui.Bold(focus),
			taiui.FGColor(color.PaletteColor(int(labelFg))),
		),
		taiui.VerticalScroll(
			coloredText(lines, scrollBox),
			top,
			scrollSpecs...,
		),
	)
}

func (t *TUI) collapsedPanel(idx int, box taiui.Box, focus bool) taiui.Element {
	base := tabUnfocusBG
	if focus {
		base = tabFocusBG
	}
	label := fmt.Sprintf("%d %s", idx+1, tabNames[idx])
	labelFg := int32(8) // bright black (gray)
	if focus {
		labelFg = 15 // bright white
	}
	if box.Width() < box.Height() {
		// Vertical strip: one column, label written vertically.
		var lines []string
		for _, r := range label {
			lines = append(lines, string(r))
		}
		return taiui.Rect(
			taiui.Box(box),
			taiui.Fill(true),
			taiui.BGColor(taiui.HexColor(base)),
			taiui.Text(
				lines,
				taiui.Bold(focus),
				taiui.FGColor(color.PaletteColor(int(labelFg))),
			),
		)
	}
	// Horizontal strip: one row, label written horizontally.
	return taiui.Rect(
		taiui.Box(box),
		taiui.Fill(true),
		taiui.BGColor(taiui.HexColor(base)),
		taiui.Text(
			"  "+label+"  ",
			taiui.Bold(focus),
			taiui.FGColor(color.PaletteColor(int(labelFg))),
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

// withTUIOutputObserver wraps a loops.Run so that model output content
// appended to the generation state is forwarded to the TUI. The
// decorator is passed through RunOptions.StateDecorators, so the loop
// applies it to the generation state before the phase chain runs. It
// replaces withFinishReasonObserver: output text, thoughts, tool calls,
// and finish reasons are all captured by the same decorator. Initial
// contents of the generation state are captured when the decorator is
// applied, so the user's chat input appears in the Output tab. See
// TheoryOfTUI.
func withTUIOutputObserver(run loops.Run, tui *TUI) loops.Run {
	return func(ctx context.Context, opts loops.RunOptions) (loops.Result, error) {
		opts.StateDecorators = append(opts.StateDecorators, func(state generators.State) generators.State {
			// The tuiOutputState layer observes only content appended
			// after it wraps the state; the initial contents of the
			// generation state are already present when the decorator is
			// applied and would otherwise never reach the Output tab.
			// Capture them now, skipping file-context blocks so the code
			// context does not flood the tab. See TheoryOfTUI.
			captureInitialContents(tui, state)
			return tuiOutputState{
				upstream: state,
				tui:      tui,
			}
		})
		return run(ctx, opts)
	}
}

// fileContextMarkers are the markers that open or close a file-context
// block in the initial user content: the gocodes and anytexts code
// providers wrap each file with "``` begin of focus file" / "``` begin
// of context file" / "``` begin of file" markers, and -doc package
// documentation uses "``` begin of context package". Such blocks are
// skipped when capturing initial contents so the code context does not
// flood the Output tab. See TheoryOfTUI.
var fileContextMarkers = []string{
	"``` begin of focus file ",
	"``` begin of context file ",
	"``` begin of context package ",
	"``` begin of file ",
	"``` end of focus file ",
	"``` end of context file ",
	"``` end of context package ",
	"``` end of file ",
}

// userInputBeginMarker wraps the ai command's chat input in the initial
// content. The markers are stripped so the user sees their raw input.
const userInputBeginMarker = "``` begin of user input"

// userInputEndMarker closes the ai command's chat input block.
const userInputEndMarker = "``` end of user input"

// captureInitialContents captures the initial contents of a generation
// state into the TUI, skipping file-context blocks so the code context
// does not flood the Output tab. The user's chat input — wrapped with
// user-input markers in the ai command, or a separate plain-text content
// in the codes pipeline — is shown. See TheoryOfTUI.
func captureInitialContents(tui *TUI, state generators.State) {
	if state == nil {
		return
	}
	for content := range state.Contents() {
		var parts []generators.Part
		for _, part := range content.Parts {
			text, ok := part.(generators.Text)
			if !ok {
				parts = append(parts, part)
				continue
			}
			if displayed := stripFileContext(string(text)); displayed != "" {
				parts = append(parts, generators.Text(displayed))
			}
		}
		if len(parts) > 0 {
			tui.captureContent(&generators.Content{
				Role:  content.Role,
				Parts: parts,
			})
		}
	}
}

// stripFileContext returns the displayable portion of a text part from
// an initial content: file-context blocks are dropped entirely, and the
// ai command's user-input markers are stripped so the raw input is
// displayed. Other text is returned unchanged. See TheoryOfTUI.
func stripFileContext(text string) string {
	trimmed := strings.TrimSpace(text)
	for _, marker := range fileContextMarkers {
		if strings.HasPrefix(trimmed, marker) {
			return ""
		}
	}
	if strings.HasPrefix(trimmed, userInputBeginMarker) {
		trimmed = strings.TrimSpace(strings.TrimPrefix(trimmed, userInputBeginMarker))
		if idx := strings.Index(trimmed, userInputEndMarker); idx >= 0 {
			trimmed = trimmed[:idx]
		}
		return strings.TrimSpace(trimmed)
	}
	return text
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
