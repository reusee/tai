package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"iter"
	"os"
	"strconv"
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
	"github.com/reusee/tai/states"
	"github.com/reusee/tai/taiui"
)

const (
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
blocks are parsed into Summary-tab lines by parseSummaries, and the request
lifecycle is tracked by isGeneratingLog and outputTabLabel.

The TUI interface replaces stdout with a three-tab terminal UI: the Output
tab streams the model output, the Summary tab collects the round completion
signals — the bodies of summary blocks, the finish reasons ("[Finish: ...]"),
and the periodic thought summaries when -summarize-thoughts is enabled —
and the Logs tab collects log records. The Logs tab renders consecutive
lines with alternating background shades so entries are visually distinct;
the two shades derive from the tab's focused or unfocused background, so the
alternation stays subtle in either state. Model output is captured from the
generation state by the tuiOutputState decorator, passed through
RunOptions.StateDecorators by runWithTUI: text parts stream to the Output
tab, thoughts are colored distinctly and separated from non-thought content
by a blank line, tool calls render as markers, and finish reasons are read
directly from the state's FinishReason parts. Raw thoughts are suppressed
from the Output tab only when -no-thoughts is set; when
-summarize-thoughts is enabled, the raw stream keeps flowing to the
Output tab while the periodic summaries stream to the Summary tab through
the forked states.ThoughtSummaryWriter. Suppressing the raw stream under
-summarize-thoughts would blank the focused Output tab during long
thinking phases — leaving no live feedback and making the session look
stalled — so the two tabs show both streams concurrently. The tuiOutputState's Flush
terminates a partial last line of the Output tab: streamed model output
often ends without a trailing newline, and without termination a later
write to the Output tab — e.g., command output written via the Output
writer after generation completes — would be merged into the model's final
line. Because Flush is the completion signal of each generation round, the
termination also separates the output of consecutive rounds. Only content appended
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
1, 2, and 3 select the corresponding tab (Output, Summary, Logs respectively):
pressing a focused tab's key collapses it to a thin strip showing the tab's
key and title, and moves the focus to the expanded tab that was last focused
(see the focus-order paragraph below); pressing a non-focused or collapsed
tab's key expands it (if collapsed) and takes the focus. Switching to an
already-expanded tab keeps its current view; re-expanding a collapsed tab
resumes following the live tail. All tabs are collapsed by default; a
collapsed tab expands automatically the FIRST time content for it arrives —
the Output tab on any streamed output, the Summary tab on a parsed summary
block, a finish reason, or a thought summary, the Logs tab on any log
record — so the interface surfaces panes only when they have something
to show. Subsequent content
arrivals do not re-expand a tab the user collapsed. Auto-expansion never
changes an existing focus: a tab popping open cannot steal attention from the
pane the user is reading, and it resumes following the tail; only when
no tab is focused does the first auto-expanded tab become the focus, so
keyboard navigation remains usable. The focused tab occupies three times the space
of each non-focused tab: the expanded tabs share the available space
proportionally to their weights (3 for the focused tab, 1 for every
other, total expanded+2), with the last tab absorbing the rounding
remainder; collapsed tabs take one column (vertical split) or one row
(horizontal split) each. The s key switches between vertical splitting (tabs
side by side, a vertical split line) and horizontal splitting (tabs
stacked, a horizontal split line). The default is horizontal splitting: the
tabs are stacked vertically, one above the other. Tab cycles the focus among
the expanded tabs, skipping collapsed ones; the [ and ] keys jump
the Output tab's view to the previous and next section transition — a role change or a
thought/non-thought change — so the user can quickly browse the whole output:
the Output tab colors each section by its role and thinking state, so a
transition is a color change between consecutive wrapped display lines. A
backward jump with no earlier transition falls back to the very beginning of
the output, so the start of the first section — a display line that is never
itself a transition — is always reachable by section navigation. The
jump stops following the tail, and a collapsed Output tab expands and takes
the focus so the jump result is visible. When the generation
finishes, the TUI stays open so the output can be browsed, and q (or Ctrl-C)
quits the TUI after a confirmation: the first press shows a confirmation bar
at the bottom of the screen, and a second press quits; any other key cancels
the confirmation and is processed normally, so an accidental q press never
loses the session.

- Rendering is a plain function of the TUI's state, following the
taiuidemo pattern: render() computes the wrapped display lines of each
expanded tab (wrappedDisplay), updates the scroll offsets against the
fresh display lengths, and builds the element tree with plain functions
(outputPanel, summaryPanel, logsPanel, buildRoot). The TUI holds nothing
but the raw state values — line buffers, tab machine, scroll offsets,
signals, and session flags. There is no provider graph, no cached view
scope, and no dirty tracking: function calls carry the state values
through the derivation chain directly. (The view used to be a dscope
provider tree whose per-component caching the provider graph recomputed;
the machinery was dropped because building a Frame is cheap and the
screens diff whole frames anyway, so the caching saved nothing while
adding a provider layer to read and reason about. See
taiui.TheoryOfTaiUI.)
`

const TheoryOfSummaryExtraction = `
taiui summary extraction theory:
- Summary blocks are extracted into the Summary tab only from the model's
  response stream (assistant/model roles) and from the loop's synthesized
  completion signals (log-role summary blocks).
- User-role content is displayed in the Output tab but never extracted:
  the loop's retry feedback embeds the synthesized summary as a summary
  block for the model's benefit, and extracting it would duplicate the
  same summary content in the Summary tab whenever a truncated round is
  retried (the retry prompt's summary plus the retry round's own summary
  or the final synthesized completion signal).
- Command output and the user's chat input are displayed but not scanned:
  they are not model completion signals.
`

const TheoryOfTUINoTruncation = `
Display buffers are unbounded and no information is ever truncated: the
Output tab retains every streamed line, the Logs tab every log record, and
the Summary tab every signal (summary block bodies and finish reasons). A
bounded buffer silently drops the oldest entries past its ceiling, making
the TUI's record of the session incomplete and shifting a scrolled-back
view by one row on each new line. Whatever the volume, losing information
is unacceptable; the memory cost of unbounded retention is accepted in
exchange for a complete browsable session record.
`

const TheoryOfMouseSupport = `
Mouse support extends the TUI's keyboard model with wheel scrolling,
click-based tab switching, and drag scrolling.

Wheel events scroll the tab whose panel is under the cursor, without
changing the focus: the user can read any pane while keyboard navigation
stays put, and a wheel over a collapsed tab is a no-op.

A left press on a collapsed tab's strip expands it and takes the focus,
resuming the live tail — the same as pressing its number key. A press on
an expanded tab's label strip toggles it like its number key: pressing
the focused tab's strip collapses it and moves the focus to the expanded
tab that was last focused; pressing another tab's strip takes the focus
without collapsing and keeps that tab's current view. A press inside an
expanded tab's scroll area focuses the tab (when it was not already
focused) and records the origin of a drag-scroll. Presses outside every
panel, middle and right presses, and no-button motion (mode 1003) are
ignored.

Drag-scrolling follows the pointer: holding the left button inside a
scroll area and dragging up reveals earlier content, dragging down
reveals the tail. The drag is anchored to the press origin, so the
content moves with the pointer rather than tracking incremental motion
deltas that would be lost when a motion event is skipped. The release
that ends the drag carries no button number; any release ends it.

Mouse reporting is enabled on start and disabled on every exit path
(MouseEnableSequence and MouseDisableSequence in the taiui package), so
the terminal returns to ordinary input handling when the TUI stops. The
message parser in taiui.ReadKeys decodes the SGR mouse sequences into
key names carrying the cell coordinates; the TUI routes those names
through handleMouseKey. See taiui.TheoryOfMouseInput.
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
	// Streamed model output often ends without a trailing newline.
	// Terminating the last output line here ensures that any subsequent
	// write to the Output tab — e.g., command output via the Output
	// writer (fmt.Fprintf) after generation completes — starts on a new
	// line instead of being merged into the model's final line. Flush is
	// the completion signal of one generation round, so the output of
	// consecutive rounds is separated as well. See TheoryOfTUI.
	s.tui.ensureOutputNewline()
	return tuiOutputState{upstream: newUpstream, tui: s.tui}, nil
}

func (s tuiOutputState) Unwrap() generators.State {
	return s.upstream
}

func (t *TUI) write(p []byte) {
	t.writeColored(taiui.NoColor, p)
}

func (t *TUI) writeColored(color taiui.Color, p []byte) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(p) == 0 {
		return
	}
	if t.tabs.AutoExpand(0) {
		t.scrolls[0].Follow = true
	}
	// Summary blocks are not parsed here: extraction happens only for
	// the model's response stream and the loop's synthesized completion
	// signals in captureContent, so user-role retry feedback and command
	// output never duplicate summary content into the Summary tab. The
	// display order still matches the stream: captureContent parses each
	// text part before the finish reason of the same content is
	// processed. See TheoryOfSummaryExtraction.
	t.output.Append(color, string(p))
}

func (t *TUI) finishReason(reason string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if reason == "" {
		return
	}
	if t.tabs.AutoExpand(1) {
		t.scrolls[1].Follow = true
	}
	// The finish reason marks the end of the generation request: the
	// Output tab's "generating..." hint clears once the request has
	// returned. A new request's "generating" log re-sets it.
	// See TheoryOfTUI.
	t.generating = false
	t.signals = append(t.signals, taiui.Line{Text: "[Finish: " + reason + "]", Color: outputColorLogLine})
}

func (t *TUI) writeLogs(p []byte) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(p) == 0 {
		return
	}
	if t.tabs.AutoExpand(2) {
		t.scrolls[2].Follow = true
	}
	for _, line := range t.logs.Append(p) {
		if isGeneratingLog(line) {
			t.generating = true
		}
	}
}

// writeThoughtSummary appends a periodic thought summary to the Summary
// tab. The states writer prefixes each summary with a "[Thought
// Summary]:" header line; the header line is colored with the thought
// color to distinguish thought summaries from round summaries and finish
// signals, and a blank separator terminates the entry. The summary
// bypasses the Output tab entirely: the Output tab's write path parses
// blocks (parseSummaries), and thought summaries are plain text destined
// for the Summary tab, not the output stream. See TheoryOfTUI.
func (t *TUI) writeThoughtSummary(p []byte) {
	t.mu.Lock()
	defer t.mu.Unlock()
	text := strings.TrimSpace(string(p))
	if text == "" {
		return
	}
	if t.tabs.AutoExpand(1) {
		t.scrolls[1].Follow = true
	}
	for i, line := range strings.Split(text, "\n") {
		color := taiui.NoColor
		if i == 0 {
			color = outputColorThoughtLine
		}
		t.signals = append(t.signals, taiui.Line{Text: line, Color: color})
	}
	t.signals = append(t.signals, taiui.Line{})
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

func (t *TUI) parseSummaries(p []byte) {
	t.parseBuf = append(t.parseBuf, p...)
	for {
		block, _, end, ok, err := blocks.ParseFirstBlock(t.parseBuf)
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
			if end > 0 && end < len(t.parseBuf) {
				if _, _, _, tailOK, tailErr := blocks.ParseFirstBlock(t.parseBuf[end:]); tailErr == nil && tailOK {
					t.parseBuf = t.parseBuf[end:]
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
			tail := t.parseBuf
			if i := bytes.LastIndexByte(t.parseBuf, '\n'); i >= 0 {
				tail = t.parseBuf[i+1:]
			}
			if len(tail) == 1 && tail[0] == '<' ||
				len(tail) >= 2 && tail[0] == '<' && tail[1] == '<' {
				retain = len(tail)
			}
			if retain > 0 {
				t.parseBuf = t.parseBuf[len(t.parseBuf)-retain:]
			} else {
				t.parseBuf = nil
			}
			return
		}
		if block.Kind == "summary" {
			body := strings.TrimSpace(block.Body)
			if body != "" {
				// A collapsed Summary tab expands automatically on the first
				// summary block; the output text carrying the block
				// already expanded the Output tab. See TheoryOfTUI.
				if t.tabs.AutoExpand(1) {
					t.scrolls[1].Follow = true
				}
				for _, line := range strings.Split(body, "\n") {
					t.signals = append(t.signals, taiui.Line{Text: line})
				}
				t.signals = append(t.signals, taiui.Line{})
			}
		}
		t.parseBuf = t.parseBuf[end:]
		if len(t.parseBuf) == 0 {
			return
		}
	}
}

type tuiWriter struct{ t *TUI }

type logsWriter struct{ t *TUI }

type thoughtSummaryWriter struct{ t *TUI }

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

func (w thoughtSummaryWriter) Write(p []byte) (int, error) {
	w.t.writeThoughtSummary(p)
	w.t.notify()
	return len(p), nil
}

type TUI struct {
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
	// showHelp reports whether the operation help overlay is visible.
	// The ? key toggles it. The overlay is derived from state like the
	// quit confirmation bar: toggling showHelp re-renders the overlay.
	// See TheoryOfTUI.
	showHelp bool

	// showThoughts reports whether raw reasoning thoughts are displayed
	// in the Output tab. It is false only when -no-thoughts is set;
	// -summarize-thoughts never suppresses the raw stream in the TUI —
	// the periodic summaries stream to the Summary tab concurrently, so
	// the focused Output tab keeps live feedback during long thinking
	// phases. runWithTUI sets it before the generation goroutine starts;
	// it is read only by captureContent on the generation goroutine. See
	// TheoryOfTUI.
	showThoughts bool

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

	// mouseDragTab is the tab whose scroll view a drag-scroll is
	// attached to, or -1 when the mouse is not dragging. mouseDragStartY
	// and mouseDragStartOffset anchor the drag to the press origin, so
	// the content moves with the pointer even when motion events are
	// skipped. See TheoryOfMouseSupport.
	mouseDragTab         int
	mouseDragStartY      int
	mouseDragStartOffset int

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
		// Every display buffer is unbounded: the Output tab retains each
		// streamed line, the Logs tab each log record, and the Summary
		// tab each signal. Truncation would silently discard information,
		// leaving the TUI's record of the session incomplete. See
		// TheoryOfTUINoTruncation.
		output: taiui.NewLineBuffer(0),
		logs:   taiui.NewStringBuffer(0),
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
		// No drag-scroll is in progress until a mouse press starts one.
		// See TheoryOfMouseSupport.
		mouseDragTab: -1,
		tty:          t,
		screen:       taiui.NewTerminalScreen(t, width, height),
		updateCh:     make(chan struct{}, 1),
		width:        width,
		height:       height,
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

// ThoughtSummaryWriter returns the writer that appends periodic thought
// summaries (-summarize-thoughts) to the Summary tab. runWithTUI forks
// the states.ThoughtSummaryWriter provider to this writer so the
// condensed reasoning appears in the Summary tab alongside the round
// summaries and finish signals, not in the Output tab. See TheoryOfTUI.
func (t *TUI) ThoughtSummaryWriter() io.Writer {
	return thoughtSummaryWriter{t}
}

func (t *TUI) captureContent(content *generators.Content) {
	role := content.Role
	for _, part := range content.Parts {
		switch p := part.(type) {
		case generators.Text:
			if len(p) > 0 {
				text := string(p)
				// Summary blocks are extracted only from the model's
				// response stream (model/assistant roles) and from the
				// loop's synthesized completion signals (log-role summary
				// blocks). User-role content — notably the retry feedback,
				// which embeds the synthesized summary as a summary block
				// for the model's benefit — is displayed in the Output tab
				// but never extracted, so a truncated round's retry does
				// not duplicate the same summary in the Summary tab. The
				// parse runs under the state lock, like every other write
				// to the display buffers, because render reads t.signals
				// concurrently. See TheoryOfSummaryExtraction.
				if role == generators.RoleModel ||
					role == generators.RoleAssistant ||
					role == generators.RoleLog {
					t.mu.Lock()
					t.parseSummaries([]byte(text))
					t.mu.Unlock()
				}
				t.writeOutputPart(role, roleColor(role), false, text)
			}
		case generators.Thought:
			if !t.showThoughts {
				// Raw thoughts are suppressed when -no-thoughts is set.
				// With -summarize-thoughts they keep streaming here while
				// the periodic summaries go to the Summary tab. See
				// TheoryOfTUI.
				continue
			}
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

// ensureOutputNewline terminates a partial last line in the Output tab.
// The streamed model output frequently ends without a trailing newline;
// without termination, a subsequent write to the Output tab — e.g.,
// command output written via the Output writer after generation
// completes — would be merged into the model's final line. It is called
// from tuiOutputState.Flush, the completion signal of each generation
// round, so the termination also separates the output of consecutive
// rounds. The display content is unchanged: a partial line and its
// completed form render identically, only the line-boundary state
// differs.
func (t *TUI) ensureOutputNewline() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.output.HasPartial() {
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

// toggleHelp shows or hides the operation help overlay. The ? key
// controls it; the overlay is part of the element tree, derived from
// the showHelp state, so no imperative layer management is needed.
func (t *TUI) toggleHelp() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.showHelp = !t.showHelp
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
	// Enable SGR mouse reporting (button events, button-held motion, and
	// SGR extended coordinates) so wheel, click, and drag events arrive
	// as input, and disable it on every exit path so the terminal returns
	// to ordinary input handling. See TheoryOfMouseSupport.
	t.enableMouse()
	defer t.disableMouse()

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
			switch {
			case strings.HasPrefix(key, taiui.MouseKeyPrefix):
				t.handleMouseKey(key)
			case key == "tab":
				t.cycleFocus()
			case key == "1":
				t.toggleTab(0)
			case key == "2":
				t.toggleTab(1)
			case key == "3":
				t.toggleTab(2)
			case key == "split":
				t.mu.Lock()
				t.tabs.SplitVertical = !t.tabs.SplitVertical
				t.mu.Unlock()
			case key == "prev-transition":
				t.jumpToTransition(-1)
			case key == "next-transition":
				t.jumpToTransition(1)
			case key == "up":
				t.scroll(-1)
			case key == "down":
				t.scroll(1)
			case key == "pageup":
				t.pageScroll(-1)
			case key == "pagedown":
				t.pageScroll(1)
			case key == "home":
				t.scrollTo(0)
			case key == "end":
				t.scrollTo(1 << 30)
			case key == "help":
				t.toggleHelp()
			case key == "quit":
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

// enableMouse switches the terminal into SGR mouse reporting (button
// events, button-held motion, and extended coordinates). It is called
// when the TUI starts, so wheel, click, and drag events arrive as input.
// See TheoryOfMouseSupport.
func (t *TUI) enableMouse() {
	io.WriteString(t.tty, taiui.MouseEnableSequence)
}

// disableMouse restores the terminal to ordinary input handling. It is
// deferred from every path out of TUI.Run, so mouse reporting never
// leaks after the TUI stops. See TheoryOfMouseSupport.
func (t *TUI) disableMouse() {
	io.WriteString(t.tty, taiui.MouseDisableSequence)
}

// tabAt returns the index of the tab whose panel box contains the given
// 0-based cell coordinates, or -1 when the point is outside every panel.
// The tab boxes tile the screen (expanded panels and collapsed strips are
// laid out without gaps), so a point normally falls in exactly one panel.
// See TheoryOfMouseSupport.
func (t *TUI) tabAt(x, y int) int {
	boxes := t.tabs.Boxes(t.width, t.height)
	for idx, box := range boxes {
		if x >= box.Left && x < box.Right && y >= box.Top && y < box.Bottom {
			return idx
		}
	}
	return -1
}

// parseMouseKey splits a mouse key name emitted by taiui.ReadKeys into its
// event kind and 0-based cell coordinates: "mouse-left@12,34" returns
// ("left", 12, 34, true). See taiui.TheoryOfMouseInput.
func parseMouseKey(key string) (button string, x, y int, ok bool) {
	name, coord, found := strings.Cut(key, "@")
	if !found || name == "" {
		return "", 0, 0, false
	}
	button = strings.TrimPrefix(name, taiui.MouseKeyPrefix)
	if button == "" || button == name {
		return "", 0, 0, false
	}
	xStr, yStr, found := strings.Cut(coord, ",")
	if !found {
		return "", 0, 0, false
	}
	var err error
	if x, err = strconv.Atoi(xStr); err != nil {
		return "", 0, 0, false
	}
	if y, err = strconv.Atoi(yStr); err != nil {
		return "", 0, 0, false
	}
	return button, x, y, true
}

// handleMouseKey routes a mouse key name to the tab interaction it
// describes. Wheel events scroll the pane under the cursor; a left press
// switches or collapses tabs and starts a drag-scroll; a release ends it.
// Middle and right presses, no-button motion, and wheel releases are
// ignored. See TheoryOfMouseSupport.
func (t *TUI) handleMouseKey(key string) {
	button, x, y, ok := parseMouseKey(key)
	if !ok {
		return
	}
	switch button {
	case "wheel-up":
		t.mouseScroll(x, y, -1)
	case "wheel-down":
		t.mouseScroll(x, y, 1)
	case "left":
		t.mousePress(x, y)
	case "release":
		t.mouseRelease()
	case "leftdrag":
		t.mouseDrag(x, y)
	}
}

// mousePress handles a left-button press at the given cell. A press on a
// collapsed tab's strip expands and focuses it, resuming the live tail. A
// press on an expanded tab's label strip toggles it like its number key:
// pressing the focused tab collapses it, pressing another tab's strip
// takes the focus without collapsing. A press inside an expanded tab's
// scroll area focuses the tab and records the drag origin for
// drag-scrolling. See TheoryOfMouseSupport.
func (t *TUI) mousePress(x, y int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	// A new press always ends any previous drag interaction.
	t.mouseDragTab = -1
	idx := t.tabAt(x, y)
	if idx < 0 {
		return
	}
	box := t.tabs.Boxes(t.width, t.height)[idx]
	if !t.tabs.Expanded[idx] {
		t.tabs.Toggle(idx)
		t.scrolls[idx].Follow = true
		return
	}
	if y == box.Top {
		t.tabs.Toggle(idx)
		return
	}
	if t.tabs.Focus != idx {
		t.tabs.Toggle(idx)
	}
	t.mouseDragTab = idx
	t.mouseDragStartY = y
	t.mouseDragStartOffset = t.scrolls[idx].Offset
}

// mouseScroll scrolls the tab whose panel is under the given cell by delta
// rows in response to a wheel event. The wheel targets the pane under the
// cursor without changing the focus, so the user can read any pane while
// keyboard navigation stays put; scrolling a collapsed tab is a no-op. See
// TheoryOfMouseSupport.
func (t *TUI) mouseScroll(x, y, delta int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	idx := t.tabAt(x, y)
	if idx < 0 || !t.tabs.Expanded[idx] {
		return
	}
	t.scrolls[idx].Scroll(delta)
}

// mouseDrag scrolls the tab that the press started in by the pointer's
// movement since the press: dragging up reveals earlier content, dragging
// down reveals the tail. The scroll offset is anchored to the press origin
// so the content follows the pointer even when motion events are skipped.
// See TheoryOfMouseSupport.
func (t *TUI) mouseDrag(x, y int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.mouseDragTab < 0 || !t.tabs.Expanded[t.mouseDragTab] {
		return
	}
	offset := t.mouseDragStartOffset + (t.mouseDragStartY - y)
	t.scrolls[t.mouseDragTab].ScrollTo(offset)
}

// mouseRelease ends an in-progress drag-scroll.
func (t *TUI) mouseRelease() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.mouseDragTab = -1
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

// jumpToTransition moves the Output tab's view to the nearest section
// transition in the given direction: -1 for the previous role or
// thoughts change, +1 for the next. A transition is a color change
// between consecutive wrapped display lines: the Output tab colors each
// section by its role and thinking state (see captureContent), and
// WrapLinesColored carries a source line's color onto every wrapped
// display line. The jump targets the same display-line coordinate space
// as the scroll offsets. A collapsed Output tab expands on the jump, and
// the jump takes the focus so the result is visible; the jump stops
// following the tail. A backward jump with no earlier transition falls
// back to the very beginning of the content, so the [ key always reaches
// the start of the first section — a display line that is never itself a
// transition (see transitionBoundaries). See TheoryOfTUI.
func (t *TUI) jumpToTransition(direction int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	// The jump result must be visible: expand the Output tab when it is
	// collapsed and take the focus so the view switches to it. Toggle
	// on an expanded non-focused tab switches the focus without
	// collapsing.
	if !t.tabs.Expanded[0] || t.tabs.Focus != 0 {
		t.tabs.Toggle(0)
	}
	boxes := t.tabs.Boxes(t.width, t.height)
	box := boxes[0]
	if box.Width() <= 0 || box.Height() <= 0 {
		return
	}
	display := wrappedDisplay(t, 0, box)
	if len(display) == 0 {
		return
	}
	// The anchor offset is clamped against the fresh display so a stale
	// offset (e.g., the tail sentinel before the first render) anchors
	// the jump at the content end. The pane height is the panel box
	// minus its one-row label strip, matching render's scroll updates.
	paneHeight := max(box.Height()-1, 1)
	offset := taiui.ClampOffset(t.scrolls[0].Offset, len(display), paneHeight)
	boundaries := transitionBoundaries(display)
	target := -1
	if direction < 0 {
		// previous: the largest boundary before the view start
		for i := len(boundaries) - 1; i >= 0; i-- {
			if boundaries[i] < offset {
				target = boundaries[i]
				break
			}
		}
		// When no boundary precedes the view start — the view is at or
		// before the first section's transition — jump to the very
		// beginning of the content. The first display line is never a
		// boundary (see transitionBoundaries), so without this fallback
		// the [ key could not reach the start of the first section.
		if target < 0 {
			target = 0
		}
	} else {
		// next: the smallest boundary after the view start
		for _, b := range boundaries {
			if b > offset {
				target = b
				break
			}
		}
	}
	if target < 0 {
		return
	}
	// The target is clamped to the content extent so the offset is valid
	// immediately; a transition beyond the last view position shows at
	// the bottom of the final view. The jump is a deliberate navigation
	// away from the live tail: following resumes only when the view
	// reaches the latest row (see ScrollState.Update).
	t.scrolls[0].Offset = taiui.ClampOffset(target, len(display), paneHeight)
	t.scrolls[0].Follow = false
}

// render presents the current state through a fresh element tree. It
// computes the wrapped display lines of each expanded tab, updates the
// scroll offsets against the fresh display lengths, and builds the root
// element with plain functions; there is no provider graph or cached view
// scope. See TheoryOfTUI.
func (t *TUI) render() {
	t.mu.Lock()
	defer t.mu.Unlock()

	// The terminal size is clamped so the layout always has a usable
	// extent.
	width := max(t.width, 1)
	height := max(t.height, 1)

	// Compute the wrapped display lines for the expanded tabs, then
	// update the authoritative scroll offsets against the FRESH display
	// lengths, so the panels read post-update offsets. Collapsed (or
	// degenerate) tabs are skipped: their scroll state stays frozen until
	// they expand. See TheoryOfTUI.
	var displays [3][]taiui.Line
	boxes := t.tabs.Boxes(width, height)
	for idx := 0; idx < 3; idx++ {
		if !t.tabs.Expanded[idx] || boxes[idx].Width() <= 0 || boxes[idx].Height() <= 0 {
			continue
		}
		displays[idx] = wrappedDisplay(t, idx, boxes[idx])
	}
	for idx := 0; idx < 3; idx++ {
		if !t.tabs.Expanded[idx] || boxes[idx].Width() <= 0 || boxes[idx].Height() <= 0 {
			continue
		}
		t.scrolls[idx].Update(len(displays[idx]), max(boxes[idx].Height()-1, 1))
	}

	taiui.Render(buildRoot(t, width, height, displays), t.screen)
}

var (
	tabNames = [...]string{"Output", "Summary", "Logs"}
	// tabUnfocusBG is the dark blue background of every unfocused tab.
	tabUnfocusBG int32 = 0x0a1428
	// tabFocusBG is the dark gray background of the focused tab.
	tabFocusBG       int32 = 0x2e2e2e
	tabActiveLabelFg int32 = 10
)

func withTUIOutputObserver(run loops.Run, tui *TUI) loops.Run {
	return func(ctx context.Context, opts loops.RunOptions, result *loops.Result) iter.Seq[error] {
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
		return run(ctx, opts, result)
	}
}

// tuiShowThoughts returns whether the TUI's Output tab displays raw
// reasoning thoughts. Only the -thoughts flag governs it:
// -summarize-thoughts adds periodic summaries in the Summary tab but never
// suppresses the raw stream, because blanking the focused Output tab during
// long thinking phases leaves no live feedback and makes the session look
// stalled. See TheoryOfTUI.
func tuiShowThoughts(thoughts flags.Thoughts) bool {
	if thoughts.Value != nil {
		return *thoughts.Value
	}
	return true
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
	originalRun := scope.Get[loops.Run]()
	// The TUI's raw-thought display is governed by -no-thoughts alone:
	// -summarize-thoughts adds periodic summaries in the Summary tab but
	// never suppresses the raw stream, because blanking the focused
	// Output tab during long thinking phases leaves no live feedback and
	// makes the session look stalled. The flag is resolved from the
	// scope before the generation goroutine starts, so the policy is
	// fixed for the session. See TheoryOfTUI.
	tui.showThoughts = tuiShowThoughts(scope.Get[flags.Thoughts]())
	scope = scope.Fork(
		func() logs.Writer { return logs.Writer(tui.LogsWriter()) },
		// Command-level output (ping verdicts, goal banners, applied
		// notices) goes to the Output tab via the dscope-resolved Output
		// writer. Generation output is captured separately and never
		// routed here. See TheoryOfCommandOutput.
		func() Output { return Output(tui.Writer()) },
		// Periodic thought summaries are routed to the Summary tab so
		// the condensed reasoning appears alongside the round summaries
		// and finish signals, while the Output tab keeps streaming the
		// raw thoughts. See states.TheoryOfThoughtsSummarize and
		// TheoryOfTUI.
		func() states.ThoughtSummaryWriter { return states.ThoughtSummaryWriter(tui.ThoughtSummaryWriter()) },
		func() loops.Run {
			return withTUIOutputObserver(originalRun, tui)
		},
	)
	// Display the user's chat input (flags.Chats) at the top of the
	// Output tab before the command starts generating. The chat lives in
	// the initial generation state, which the tuiOutputState decorator
	// does not display; writing it here gives the user a clear view of
	// what the model was asked. See TheoryOfTUI.
	displayChatInput(tui, scope.Get[flags.Chats]())
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
