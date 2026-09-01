package main

import (
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
	"github.com/reusee/tai/apps"
	"github.com/reusee/tai/flags"
	"github.com/reusee/tai/generators"
	"github.com/reusee/tai/logs"
	"github.com/reusee/tai/pipeline"
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
focus order, weighted panel layout, collapsed strips, follow-tail
scroll offsets, together with the incremental wrap cache (taiui.WrapCache),
per-tab panel construction (taiui.TabPanel and taiui.PaneHeight), pointer
tab interaction (taiui.TabMouse), section navigation, quit confirmation
and help overlays, and the session event loop (taiui.Session). This
command wires them with tai-specific capture:
generators.Content is converted to taiui.Line by captureContent, pipeline
events are rendered into Events-tab lines by handleEvent and eventLines,
and the request lifecycle is tracked by isGeneratingLog and outputTabLabel.

The TUI interface replaces stdout with a three-tab terminal UI: the Output
tab streams the model output, the Events tab renders the generation loop's
event stream — attempt starts ("🚀 [Attempt N start]"), the request
parameter lines ("📡 [Attempt N request] ...") that precede each request,
attempt summaries, truncations, retries, handoff starts and
synthesized summaries, the finish reasons ("🏁 [Finish: stop]"), the
per-attempt usage lines ("📊 [Usage] ..."), the thought summaries when
-summarize-thoughts is enabled, the component/idle continuations, the
loop-start that roots each goal loop's branch, and the goal-mode
verdicts ("🎯 [Goal Achieved after N loop(s)]") and failure
notes from EventGoal — and the Logs tab collects
log records. Every event kind renders: each event's first line starts
with the kind's emoji (eventEmoji) followed by a bracketed label, one
display style shared by every kind and by the goal verdicts the pipeline
emits (pipeline.RunGoal).
Goal-loop runs attribute the per-attempt events to their loop: attempt
starts, requests, and completions render "[loop L attempt N ...]" and
usage lines render "[Usage] loop L attempt N: ..."; non-goal runs omit
the attribution and keep their display bytes unchanged.
A completed attempt with no summary
shows a completion line ("✅ [Attempt N complete]"), and an unknown kind shows
a generic "❓ [Event <kind>]" line, so no pipeline event type is silently
dropped. Events are constructed and yielded the moment their facts are
known: an attempt-start precedes its work, a request event precedes the
attempt's request, a handoff-start precedes the handoff request, and a
truncation fires before the handoff summary is
requested. The Events tab's only content source is pipeline.Run:
withTUIOutputObserver taps the run's event iterator and forwards every
event to handleEvent, so every Events-tab line originates from a pipeline
event (see pipeline.TheoryOfLoopEvents), and EventFinish clears the
Output tab's "generating..." hint. The Logs tab renders consecutive
lines with alternating background shades so entries are visually distinct;
the two shades derive from the tab's focused or unfocused background, so the
alternation stays subtle in either state. The Events tab renders the
stream as a tree (see TheoryOfEventTree): each goal loop is one branch
rooted at its loop-start event, an attempt nests under it, and the
attempt's lifecycle events nest under its start; display order is a
depth-first walk, so out-of-order arrival renders in tree order, and
every line carries two Han-character widths of indent per depth. The tab
alternates the same two shades per event: all display lines of one event
share one shade, and consecutive events alternate. Model output is captured from the
generation state by the tuiOutputState decorator, passed through
RunOptions.StateDecorators by runWithTUI: text parts stream to the Output
tab, thoughts are colored distinctly and separated from non-thought content
by a blank line, tool calls render as markers, and errors are shown inline.
Raw thoughts are suppressed
from the Output tab only when -no-thoughts is set; when
-summarize-thoughts is enabled, the raw stream keeps flowing to
the Output tab while the periodic summaries render in the Events tab from
EventThoughtSummary (see pipeline.TheoryOfThoughtsSummarize), and the
per-attempt usage lines render from EventUsage (see
pipeline.TheoryOfUsageLogging). Suppressing the raw stream under
-summarize-thoughts would blank the focused Output tab during long
thinking phases — leaving no live feedback and making the session look
stalled — so the two tabs show both streams concurrently. The tuiOutputState's Flush
terminates a partial last line of the Output tab: streamed model output
often ends without a trailing newline, and without termination a later
write to the Output tab — e.g., command output written via the Output
writer after generation completes — would be merged into the model's final
line. Because Flush is the completion signal of each generation, the
termination also separates the output of consecutive generations. Only content appended
after the decorator wraps the state is displayed; initial contents are not
re-parsed or re-displayed, because unstructured text must not be
imperfectly parsed. The one exception is the user's chat input: runWithTUI
writes the flags.Chats content to the Output tab in the user role color
before the command starts, so the user sees what the model was asked even
though the chat lives in the initial state. Attempt summaries are rendered
from EventAttemptCompleted, EventTruncated, and
EventSynthesizedSummary, so the TUI never parses streamed text for
blocks, never scans rendered text for "[Finish: ...]" markers, and never
captures model output through a
stdout pipe; retry feedback cannot duplicate summary content because the
loop's events are the single authority. The goal-mode verdicts and
failure notes are pipeline events too (EventGoal), so they render in the
Events tab and never reach the Output
tab. stdout is discarded in TUI mode, while stderr stays visible
in the Output tab. Content is colored by role, matching the non-TUI output
colors (see generators/colors.go): user input is blue, tool calls and
results yellow, system messages cyan, log records red, and thoughts bright
magenta; model output keeps the default foreground. Role colors are ANSI 16
palette colors, so text uses only the standard 16-color SGR codes; only
backgrounds use true-color hex values. Colors are carried per output line
through wrapping, so a wrapped line keeps its role color. The keys
1, 2, and 3 select the corresponding tab (Output, Events, Logs
respectively); the number-key collapse/expand and focus-handoff semantics,
first-content auto-expansion, the unseen emoji on collapsed strips, and
the weighted layout (the focused tab weighs 3, every other expanded tab 1)
are the taiui tab state machine's (taiui.TheoryOfTabs) and are not
repeated here. The Output tab starts expanded and focused, following the
live tail — the model's stream is the pane the user watches, so it is open
from the first frame — while the Events and Logs tabs stay collapsed and
expand on their first content (the Events tab on its first rendered event
line, the Logs tab on any log record), so the interface surfaces panes
only when they have something to show. The Logs tab caps its box at
logsMaxBoxHeight rows while expanded but not focused — logs are internal
diagnostics, so an unfocused pane shows only the latest lines — and the
freed rows go to the other expanded tabs by weight; focusing Logs
restores the usual ratio. The s key switches between vertical splitting
(tabs side by side, a vertical split line) and horizontal splitting (tabs
stacked, one above the other). Tab cycles the focus among the expanded
tabs, skipping collapsed ones; the [ and ] keys jump the Output tab's view
through the section transitions — in the Output tab a transition is a role
change or a thought/non-thought change, i.e., a color change between
consecutive wrapped display lines — using the exit and entry jump stops of
taiui.TheoryOfSectionNavigation. A forward jump with no later stop falls
back to the live tail, the symmetric endpoint of taiui's backward
fallback to the beginning, so the ] key always moves the view to a
defined position — without the fallback the key would silently do nothing
whenever the view sits in the last section or the output has one uniform
section. The jump stops following the tail, and a collapsed Output tab
expands and takes the focus so the jump result is visible. When the
generation finishes, the TUI stays open so the output can be browsed, and
q (or Ctrl-C) quits the TUI after a confirmation: the first press shows a
confirmation bar at the bottom of the screen, and a second press quits;
any other key cancels the confirmation and is processed normally, so an
accidental q press never loses the session.

- Rendering is a plain function of the TUI's state, following the
taiuidemo pattern: render() computes the wrapped display lines of each
expanded tab (wrappedDisplay), updates the scroll offsets against the
fresh display lengths, and builds the element tree with plain functions
(one taiui.TabPanel per tab, plus buildRoot). wrappedDisplay feeds the
Output and Logs tabs' content through a taiui.WrapCache so that when new
output streams in, only the newly arrived completed lines and the
trailing partial line are wrapped, avoiding O(N) full re-wrapping of
large buffers on every frame; the Events tab caches each event node's
wrapped lines instead (see TheoryOfEventTree), so a frame re-wraps only
nodes that are new or repositioned.
When the display width or tab background changes, the cache is reset
and recomputed. The TUI holds nothing but the raw state values — line
buffers, tab machine, scroll offsets, events, and session flags.
`

// logsMaxBoxHeight bounds the Logs tab's box height while it is expanded
// but not focused: one label-strip row plus two log-content rows. Logs
// collect internal diagnostics, so an unfocused pane shows only the
// latest lines; the freed rows go to the other expanded tabs by weight,
// and focusing Logs lifts the cap and restores the usual ratio. See
// TheoryOfTUI and taiui.TheoryOfTabs.
const logsMaxBoxHeight = 3

const TheoryOfTUIChatInput = `
TUI mode replaces the liner-based chat prompt with the TUI's own input
bar: the terminal has exactly one input reader (taiui.ReadKeys), and a
liner prompt would open a second raw-mode reader on the same tty,
racing for keystrokes and resetting the terminal mode under the TUI —
the reported "TUI slows down and cannot switch tabs after ai output"
was exactly that race, because the ai command's idle handler prompted
with liner once the first generation finished. forkTUIDisplay forks
pipeline.ChatInput to TUI.ChatInput, so both interactive chat paths
(the ai command's OnIdle handler and the next command's chat phase) get
the bar in TUI mode while plain command-line mode keeps the liner
default. See pipeline.TheoryOfChatInput.

Only interactive sessions render the bar: apps that call
pipeline.ChatInput while running fork apps.Interactive(true) into their
Defs, and runWithTUI reads it from the app's scope into TUI.interactive.
The other commands (goal
mode, any, ping, patch, record) never read interactive input, so their
TUI omits the bar entirely — the Output pane keeps its full height, a
press on the bottom row drives ordinary tab interaction, and the help
overlay drops the input-bar entries. In an interactive session the bar
is non-modal and rendered as the bottom row of the Output tab — part of
the tab's layout, never a screen-wide overlay — so the rest of the
interface stays operable while typing: pointer
events route through the ordinary mouse path (wheel and drag scrolling,
press-driven tab switching), and navigation keys (arrows, page keys,
tab) fall through to the normal dispatch instead of being consumed.
Only printable runes and line-editing keys are consumed by the bar;
keys that double as navigation bindings (q, 1..3, s, [, ]) are typed
as characters while the bar is focused, and Esc (or Ctrl-C) releases
the keyboard back to navigation without cancelling anything. The bar's
background follows the Output tab's focus state, using the same focused
and unfocused backgrounds as the tab panels, so a bar in an unfocused
tab never reads as a focused element. The editing state and the bar
rendering come from the reusable taiui.InputBar (see
taiui.TheoryOfInputBar); this TUI keeps only the focus policy, the
delivery, and the blocked-wait protocol around it.

Focus is pointer-driven: the bar takes focus ONLY when the user clicks
its row, and it keeps focus after a submit, so a line typed while the
model generated is sent with the next Enter. A waiting ChatInput call
never takes the keyboard — until the click the keys keep driving
navigation and the terminal cursor stays hidden. A left press outside
the bar's row releases the focus; wheel events never change it. While
focused, Enter submits the line ONLY when a ChatInput call is actually
waiting — during generation typing works but Enter is a no-op and the
line is kept, so the content reaches the model's next round. Esc and
Ctrl-C only unfocus; the blocked ChatInput keeps waiting, and io.EOF
reaches it only through the quit path (cancelChatInput), so ending the
session always goes through the quit confirmation. A fall-through
navigation key (arrows, page keys, tab) that changes other elements —
scrolling the focused pane, cycling the tab focus — also releases the
focus: handleKey snapshots the view state before the dispatch and
compares it after, so a key that changes nothing (a page-up already at
the top) keeps the bar focused. After any loss the cursor hides and
ONLY a click on the bar's row regains the focus.

The terminal cursor is shown while the bar is focused — the focused
bar renders an Input element whose CursorAt records the editing
position in the frame — and hidden on the focus-loss transition,
written from render after the frame is presented so the sequence stays
serial with the screen's output. The interactive Output tab's scroll
view shrinks by one row to make room for the bar (tuiPaneHeight), and
the pane arithmetic — scroll updates, page scrolling, section jumps —
uses the same adjusted height so the view and the layout never
disagree; a non-interactive Output pane keeps the full height because
no row is reserved.
`

const TheoryOfTUIHandoff = `
The Output tab title reflects the handoff process: while a handoff
request is being generated (see pipeline.TheoryOfHandoff), the title shows
"Output (handoff...)" with the active highlight, taking precedence over
the "generating..." hint. The handoff request's contents reach the Output
tab through the forked pipeline.HandoffStateDecorator, which observes
every content part with its role and thinking state, so text and
reasoning thoughts are highlighted per part and per thought, the same as
regular generation output. The pipeline.HandoffObserver provider drives
the title state: HandoffStart sets the handoff flag, HandoffEnd clears it.
`

const TheoryOfTUIDisplayFork = `
The TUI display writers (the Logs pane and the Output tab) are forked
into the scope before the generation loop is resolved from it.
pipeline.Run — Module.Run — binds logs.Logger from the scope at
provider-resolution time, so the loop must be resolved AFTER the forks
take effect: a loop resolved before them binds the pre-fork Logger built
during startup on the real stderr, painting the raw terminal where the
next repaint erases it. The Events tab needs no writer fork:
withTUIOutputObserver, layered in the second fork's Run wrapper, taps
the run's event iterator and forwards every event to the TUI (see
pipeline.TheoryOfLoopEvents), and the goal event observer is forked to
the same handler, so the goal runner's EventGoal verdicts reach the
Events tab through the tap path. The state-decorator and
event-tap Run wrapper is layered in a second fork so its pipeline.Run
def does not resolve itself recursively.
`

const TheoryOfTUINoTruncation = `
Display buffers are unbounded and no information is ever truncated: the
Output tab retains every streamed line, the Logs tab every log record, and
the Events tab every event line. A
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
focused) and records the origin of a drag-scroll. A left press on an
Events row additionally jumps the Output tab to the output section the
row's event owns, so every event row reaches its attempt's output (see
TheoryOfTUIOutputSections); the same press still toggles a handoff
node. Presses outside every panel, middle and right presses, and
no-button motion (mode 1003) are ignored.

In interactive sessions, the Output tab's input row is the one press
target with its own semantics: a left press on the chat input bar's row
focuses the input instead of driving tab interaction (the Output tab
takes the keyboard focus with it, so the scroll keys act on the pane
the bar belongs to), and a left press anywhere else releases the input
focus before the ordinary press handling runs. Wheel events never
change the input focus; a non-interactive session has no input row, so
every press drives the ordinary tab interaction. See
TheoryOfTUIChatInput.

Drag-scrolling follows the pointer: holding the left button inside a
scroll area and dragging up reveals earlier content, dragging down
reveals the tail. The drag is anchored to the press origin, so the
content moves with the pointer rather than tracking incremental motion
deltas that would be lost when a motion event is skipped. The release
that ends the drag carries no button number; any release ends it.

Mouse reporting is enabled on start and disabled on every exit path by
the taiui.Session that drives the TUI loop (MouseEnableSequence and
MouseDisableSequence in the taiui package), so the terminal returns to
ordinary input handling when the TUI stops. taiui.ReadKeys decodes the
SGR mouse sequences into key names carrying the cell coordinates, and
the parsed events route onto taiui.TabMouse through handleMouseKey. See
taiui.TheoryOfMouseInput and taiui.TheoryOfMouseInteraction.

Mouse reporting is also runtime-switchable: the m key toggles it via
toggleMouse, which flips the recorded state and calls
taiui.Session.SetMouse to write the enable or disable sequence. While
reporting is off, the terminal performs its own text selection and copy
and the wheel feeds the terminal scrollback, so the displayed output can
be selected and copied; most terminals also offer Shift+drag as a
selection bypass while reporting is on. Pressing m again restores the
TUI's pointer interaction. Each toggle records the new state as a log
line in the Logs tab.
`

// Tui controls the terminal UI mode. The default is the TUI when stdout
// is a terminal and CLI when stdout is redirected to another program or
// a file, so piped consumers receive the generation output. The -tui
// flag forces the TUI explicitly, and the -cli flag forces plain
// command-line output. See TheoryOfDisplayMode.
type Tui bool

const TheoryOfDisplayMode = `
Display mode policy: every command runs in the TUI by default when
stdout is a terminal. When stdout is redirected to another program or a
file (tai next | tee .AI), the default is CLI: TUI mode discards stdout
— redirecting it to the null device and capturing output through state
decorators — so a piped consumer would receive nothing. The -cli flag
opts out to plain command-line output, where generation output writes
to the real stdout instead of the TUI's redirected null device; the
-tui flag states the TUI choice explicitly and overrides the redirected
default. The default lives in Module.Tui, which consults the injected
StdoutIsTerminal check so tests control the environment (under go test
stdout is itself a pipe), and main routes the command through
runWithTUI when the resolved Tui value is true. A newTUI failure (no
usable terminal) falls back to the plain command-line path, so
non-interactive environments degrade gracefully without the flag.
Terminal acquisition is part of the same entry gate as the check:
tryOpenTty tries stdin/stdout and then /dev/tty, retrying each backend
once, so a transient start failure (an interleaved signal or a previous
session's teardown racing setup) or a non-terminal stdin no longer
drops an otherwise interactive session to the command line for that
invocation.
`

// StdoutIsTerminal reports whether standard output is attached to a
// terminal. The display-mode default consults it to detect a redirected
// stdout — a pipe to another program or a file — where TUI mode would
// discard the output the consumer expects. It is a dscope-injected
// function type so tests control the environment. See
// TheoryOfDisplayMode.
type StdoutIsTerminal func() bool

// Tui provides the display-mode default: the TUI when stdout is a
// terminal, CLI when stdout is redirected, so piped consumers receive
// the generation output instead of the TUI's discarded stdout. The -tui
// and -cli flags override the default. See TheoryOfDisplayMode.
func (Module) Tui(stdoutIsTerminal StdoutIsTerminal) Tui {
	return Tui(stdoutIsTerminal())
}

// StdoutIsTerminal provides the production check: os.Stdout is a
// terminal when its file mode has the character-device bit set; a pipe
// or a regular file lacks the bit. /dev/null carries the bit, which is
// harmless — redirecting to /dev/null discards the output either way.
// See TheoryOfDisplayMode.
func (Module) StdoutIsTerminal() StdoutIsTerminal {
	return func() bool {
		info, err := os.Stdout.Stat()
		if err != nil {
			return false
		}
		return info.Mode()&os.ModeCharDevice != 0
	}
}

var _ generators.State = tuiOutputState{}

func (t Tui) Handle(key string, args []string) (newDef any, remainArgs []string, err error) {
	// -tui selects the TUI; -cli selects plain command-line output.
	// Either flag overrides the StdoutIsTerminal-derived default.
	ret := Tui(key != "-cli")
	return &ret, args, nil
}

func (t Tui) Keys() map[string]string {
	return map[string]string{
		"-tui": "Use the TUI interface (the default)",
		"-cli": "Use the plain command-line interface, disabling the TUI",
	}
}

// panelStyle styles the three tab panels: dark blue for unfocused tabs,
// dark gray for the focused tab, a highlight color for the active
// request label, and red for the fallback unseen-content background on
// a one-column strip, where the red-circle unseen emoji cannot fit. It
// is the single style definition shared by the panel rendering and the
// tests.
var panelStyle = taiui.PanelStyle{
	BaseBG:        taiui.HexColor(tabUnfocusBG),
	FocusBG:       taiui.HexColor(tabFocusBG),
	LabelFG:       color.PaletteColor(8),
	FocusLabelFG:  color.PaletteColor(15),
	ActiveLabelFG: color.PaletteColor(int(tabActiveLabelFg)),
	UnseenDotBG:   taiui.HexColor(0xd23b3b),
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
// calls and results render as markers, and errors are shown inline.
// Finish reasons are not captured here: they arrive as EventFinish on
// the event stream that withTUIOutputObserver taps, and the finish event
// clears the Output tab's "generating..." hint. See TheoryOfTUI and
// pipeline.TheoryOfLoopEvents.
type tuiOutputState struct {
	upstream generators.State
	tui      *TUI
}

// chatInputResult carries one chat input outcome to the blocked
// ChatInput call: the submitted line when ok is true, or a
// cancellation when ok is false. See TheoryOfTUIChatInput.
type chatInputResult struct {
	line string
	ok   bool
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
	// the completion signal of one generation, so the output of
	// consecutive generations is separated as well. See TheoryOfTUI.
	s.tui.ensureOutputNewline()
	// The attempt has ended: a pending section owner whose output never
	// appeared must not open a section at the next, unrelated content.
	// See TheoryOfTUIOutputSections.
	s.tui.clearPendingOutputOwner()
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
	t.output.Append(color, string(p))
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
	mu      sync.Mutex
	output  *taiui.LineBuffer
	logs    *taiui.StringBuffer
	tabs    *taiui.Tabs
	scrolls [3]taiui.ScrollState

	// events is the Events tab's event forest: pipeline events are
	// filed into it by their (loop, sequence) identity and rendered
	// depth-first with cached wrapping, elapsed timers, and alternating
	// shades. The tree mechanism lives in taiui; see
	// taiui.TheoryOfEventTree and TheoryOfEventTree. Guarded by mu.
	events taiui.EventTree

	// startTime anchors the Events tab's elapsed-time timer: every
	// event records the duration from startTime to its arrival, shown
	// at the right edge of its first display line. See TheoryOfEventTree.
	startTime time.Time

	outputCache taiui.WrapCache
	logsCache   taiui.WrapCache

	// finished reports whether the generation session has ended. It
	// clears the Output tab's "generating..." hint.
	finished bool
	// quit is the two-press quit confirmation state: the first press of
	// a quit key (q, Q, or Ctrl-C) arms it and shows a confirmation bar
	// (taiui.QuitConfirmBar) at the bottom of the screen; a second quit
	// key press quits, and any other key cancels the confirmation before
	// its normal processing. See TheoryOfTUI and
	// taiui.TheoryOfSessionChrome.
	quit taiui.QuitConfirm
	// generating reports whether a generation request is in flight. It
	// is set when the generator's "generating" log record is observed
	// and cleared by an EventFinish or by the session
	// ending. While a request is in flight the Output tab title keeps
	// the "generating..." hint regardless of how long the model is
	// silent (e.g., long thinking phases without streamed output).
	// See TheoryOfTUI.
	generating bool
	// handoff reports whether a handoff generation request is in flight.
	// It is set by HandoffStart and cleared by HandoffEnd, and takes
	// precedence over the "generating..." hint in the Output tab title.
	// See pipeline.TheoryOfHandoff and TheoryOfTUIHandoff.
	handoff bool
	// showHelp reports whether the operation help overlay is visible.
	// The ? key toggles it. The overlay is derived from state like the
	// quit confirmation bar: toggling showHelp re-renders the overlay.
	// See TheoryOfTUI.
	showHelp bool

	// interactive reports whether this session's app supports
	// multi-turn conversation (see apps.Interactive): only
	// interactive sessions render the chat input bar and reserve its
	// row; in the others the bar is not drawn, the Output pane keeps
	// its full height, and clicks on the bottom row drive ordinary tab
	// interaction. runWithTUI sets it. See TheoryOfTUIChatInput.
	interactive bool

	// Chat input bar state, guarded by mu. In interactive sessions the
	// bar is rendered as the bottom row of the Output tab; inputFocused
	// reports whether it holds the keyboard, and inputBar carries the
	// reusable editing state (prompt, line, cursor) of taiui.InputBar —
	// the line editing and bar rendering live in the library, while
	// focus, delivery, and the blocked-wait protocol stay here.
	// inputResult is non-nil while a ChatInput call is blocked on the
	// generation goroutine (the model is idle); Enter delivers the
	// typed line only then, and the pending line survives across calls
	// so text typed while the model generated is sent with the next
	// submit. See TheoryOfTUIChatInput and taiui.TheoryOfInputBar.
	inputFocused bool
	inputBar     taiui.InputBar
	inputResult  chan chatInputResult
	// wasInputFocused tracks the input focus across renders to write
	// the terminal cursor visibility sequence exactly on the
	// transition. See TheoryOfTUIChatInput.
	wasInputFocused bool

	// showThoughts reports whether raw reasoning thoughts are displayed
	// in the Output tab. It is false only when -no-thoughts is set;
	// -summarize-thoughts never suppresses the raw stream in the TUI —
	// the periodic summaries render in the Events tab concurrently, so
	// the focused Output tab keeps live feedback during long thinking
	// phases. runWithTUI sets it before the generation goroutine starts;
	// it is read only by captureContent on the generation goroutine. See
	// TheoryOfTUI.
	showThoughts bool

	// lastOutputRole is the role of the last content written to the
	// Output tab. It is used with lastWasThought to insert a blank line
	// separator when the output switches roles or switches between
	// thinking and non-thinking content. It is written by the generation
	// goroutine via captureContent; displayChatInput initializes it on
	// the main goroutine before the generation goroutine starts, so it
	// is never accessed concurrently. See TheoryOfTUI.
	lastOutputRole generators.Role
	// lastWasThought reports whether the last content written to the
	// Output tab was a thought. See lastOutputRole.
	lastWasThought bool
	// hasOutput reports whether any content has been written to the
	// Output tab. It is false until the first part is written, so the
	// first output never gets a leading blank line separator.
	hasOutput bool

	// outputSections organizes the Output tab's stream into sections:
	// each records the source-line index in the output buffer where the
	// section begins, so navigation can scroll the pane to a section's
	// first display line. eventSections binds a pipeline event's
	// (run, sequence) identity to the section the event's attempt
	// wrote, and pendingOwner carries an attempt-start event's identity
	// to the next visible content part, which then opens the event's
	// section. All three are guarded by mu. See
	// TheoryOfTUIOutputSections.
	outputSections []outputSection
	eventSections  map[outputSectionOwner]int
	pendingOwner   *outputSectionOwner

	// mouse is the pointer interaction state over the tab layout: wheel
	// scrolling, press-driven tab switching, and drag-scrolling anchored
	// to the press origin. Its zero value is inert. See
	// taiui.TheoryOfMouseInteraction.
	mouse taiui.TabMouse

	// session is the running taiui.Session, set by Run. The mouse key
	// uses it to switch mouse reporting at runtime (see toggleMouse); it
	// is nil until Run starts — tests construct a TUI without a session —
	// so consumers treat nil as inert.
	session *taiui.Session
	// mouseReporting records whether terminal mouse reporting is
	// currently enabled. While it is disabled, the terminal performs its
	// own text selection and copy; see toggleMouse and
	// TheoryOfMouseSupport.
	mouseReporting bool

	tty      tty.Tty
	screen   *taiui.TerminalScreen
	updateCh chan struct{}
	width    int
	height   int
}

// ttyOpener opens one tty backend for the TUI.
type ttyOpener func() (tty.Tty, error)

func newTUI() (*TUI, error) {
	t, err := tryOpenTty([]ttyOpener{tty.NewStdIoTty, tty.NewDevTty})
	if err != nil {
		return nil, err
	}
	width, height := 80, 25
	if ws, err := t.WindowSize(); err == nil && ws.Width > 0 && ws.Height > 0 {
		width, height = ws.Width, ws.Height
	}
	tabs := taiui.NewTabs(3)
	// The Logs tab caps its box height while expanded but not focused;
	// see logsMaxBoxHeight.
	tabs.MaxSizes = []int{0, 0, logsMaxBoxHeight}
	// The Output tab starts expanded and focused: the model's stream is
	// the pane the user watches, so it is open from the first frame.
	// See TheoryOfTUI.
	tabs.FocusTab(0)
	return &TUI{
		// Every display buffer is unbounded: the Output tab retains each
		// streamed line, the Logs tab each log record, and the Events
		// tab each event line. Truncation would silently discard
		// information, leaving the TUI's record of the session
		// incomplete. See TheoryOfTUINoTruncation.
		output: taiui.NewLineBuffer(0),
		logs:   taiui.NewStringBuffer(0),
		tabs:   tabs,
		// The Output tab starts expanded, focused, and following the
		// tail; the other tabs stay collapsed and expand automatically
		// the first time content for them arrives. The scroll offsets
		// start at the tail sentinel so the first render sticks to the
		// latest content. See TheoryOfTUI.
		scrolls: [3]taiui.ScrollState{
			{Offset: 1 << 30, Follow: true},
			{Offset: 1 << 30},
			{Offset: 1 << 30},
		},
		// The Events tab's elapsed-time timer counts from the session's
		// start. See TheoryOfEventTree.
		startTime: time.Now(),
		// The Events tab's event forest; the expand-hint color matches
		// the log color of event lines. See TheoryOfEventTree.
		events:   taiui.EventTree{HintColor: outputColorLogLine},
		tty:      t,
		screen:   taiui.NewTerminalScreen(t, width, height),
		updateCh: make(chan struct{}, 1),
		width:    width,
		height:   height,
	}, nil
}

// tryOpenTty acquires a terminal for the TUI: stdin/stdout first, then
// /dev/tty, each backend retried once. The raw-mode ioctls of a session
// start can fail transiently — an interleaved signal delivering EINTR, or
// a previous session's teardown racing this one's setup — and a stdin that
// happens not to be a terminal must still fall through to /dev/tty. A
// single failure therefore never disables the TUI for the whole
// invocation; only when every attempt fails does the caller fall back to
// the command line. See TheoryOfDisplayMode.
func tryOpenTty(openers []ttyOpener) (tty.Tty, error) {
	for _, open := range openers {
		for retry := 0; retry < 2; retry++ {
			t, err := open()
			if err != nil {
				continue
			}
			if err := t.Start(); err != nil {
				continue
			}
			return t, nil
		}
	}
	return nil, fmt.Errorf("no usable terminal to start TUI")
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

func (t *TUI) ChatInput(prompt string) (string, error) {
	ch := make(chan chatInputResult, 1)
	t.mu.Lock()
	// A waiting call does not take the keyboard: focus is gained only
	// by a click on the bar's row, so the keys keep driving navigation
	// and the terminal cursor stays hidden until the click. The pending
	// typed line is preserved, so text typed before the click is
	// submitted with the first Enter. See TheoryOfTUIChatInput.
	t.inputBar.Prompt = prompt
	t.inputResult = ch
	t.mu.Unlock()
	t.notify()
	res := <-ch
	if !res.ok {
		return "", io.EOF
	}
	return res.line, nil
}

func (t *TUI) handleChatInputKey(key string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.inputFocused {
		return false
	}
	switch {
	case key == "enter":
		// Submit only while a ChatInput call waits on the bar (the
		// model is idle); during generation the key is a no-op that
		// keeps the typed line for the next submit. The key is always
		// consumed so it never triggers the unfocused Enter binding.
		// See TheoryOfTUIChatInput.
		if t.inputResult != nil {
			t.deliverInputLocked(chatInputResult{line: t.inputBar.Line(), ok: true})
		}
	case key == "esc", key == "ctrl-c":
		// Release the keyboard back to navigation; the blocked
		// ChatInput keeps waiting and the typed line is kept. io.EOF
		// reaches a waiting input only through the quit path. See
		// TheoryOfTUIChatInput.
		t.inputFocused = false
	default:
		// Line-editing keys edit the pending line through the reusable
		// taiui.InputBar; keys that are not line editing (arrows, page
		// keys, tab, function keys) return false and fall through to
		// the normal dispatch, so the TUI stays operable while typing.
		// See TheoryOfTUIChatInput and taiui.TheoryOfInputBar.
		return t.inputBar.HandleKey(key)
	}
	return true
}

func (t *TUI) deliverInputLocked(res chatInputResult) {
	ch := t.inputResult
	t.inputBar.Reset()
	t.inputResult = nil
	// The bar keeps its focus after the delivery, so typing continues
	// right into the next round. See TheoryOfTUIChatInput.
	if ch != nil {
		ch <- res
	}
}

func (t *TUI) cancelChatInput() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.inputFocused = false
	if t.inputResult != nil {
		t.deliverInputLocked(chatInputResult{})
	}
}

// focusInputLocked focuses the chat input bar: typed keys edit the
// line, and the Output tab — the pane the bar belongs to — takes the
// keyboard focus with it so the scroll keys act on that pane. An armed
// quit confirmation is cancelled because q types text while the bar is
// focused. The caller holds t.mu. See TheoryOfTUIChatInput.
func (t *TUI) focusInputLocked() {
	t.inputFocused = true
	t.quit.Cancel()
	if t.tabs.Focus != 0 {
		t.tabs.FocusTab(0)
	}
}

// chatInputViewSnapshot captures the view state a fall-through key may
// change while the chat input bar is focused: the pane scroll states,
// the tab focus, the tab expansion, and the split axis. handleKey
// compares the snapshots taken before and after a fall-through key's
// dispatch to detect whether the key changed other elements — the
// condition that releases the input focus. See TheoryOfTUIChatInput.
type chatInputViewSnapshot struct {
	scrolls  [3]taiui.ScrollState
	expanded [3]bool
	focus    int
	split    bool
}

// inputRowHit reports whether the given cell lies on the chat input
// bar's row: the bottom row of the expanded Output tab's box, the same
// row buildRoot renders the bar on. The row exists only in interactive
// sessions; in the others the bar is not rendered and the press falls
// through to ordinary tab interaction. The caller holds t.mu. See
// TheoryOfTUIChatInput.
func (t *TUI) inputRowHit(x, y int) bool {
	if !t.interactive || !t.tabs.Expanded[0] {
		return false
	}
	box := t.tabs.Boxes(t.width, t.height)[0]
	if box.Height() <= 1 || box.Width() <= 0 {
		return false
	}
	return y == box.Bottom-1 && x >= box.Left && x < box.Right
}

// chatInputViewSnapshotLocked returns the current view snapshot for the
// input-focus release detection. The caller holds t.mu. See
// TheoryOfTUIChatInput.
func (t *TUI) chatInputViewSnapshotLocked() chatInputViewSnapshot {
	var snap chatInputViewSnapshot
	snap.scrolls = t.scrolls
	snap.focus = t.tabs.Focus
	copy(snap.expanded[:], t.tabs.Expanded)
	snap.split = t.tabs.SplitVertical
	return snap
}

func (t *TUI) captureContent(content *generators.Content) {
	role := content.Role
	for _, part := range content.Parts {
		switch p := part.(type) {
		case generators.Text:
			if len(p) > 0 {
				t.writeOutputPart(role, roleColor(role), false, string(p))
			}
		case generators.Thought:
			if !t.showThoughts {
				// Raw thoughts are suppressed when -no-thoughts is set.
				// With -summarize-thoughts they keep streaming here while
				// the periodic summaries render in the Events tab. See
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

// writeOutputPart writes one output part to the Output tab, starting a
// new output section and inserting a blank line separator when the
// output switches roles or switches between thinking and non-thinking
// content, and also when a pending attempt-start event marks the start
// of a new attempt's output. The section state (hasOutput,
// lastOutputRole, lastWasThought) is only accessed by the generation
// goroutine via captureContent, so it is read and written without a
// lock; the section bookkeeping also read by the pointer handlers is
// guarded by mu. See TheoryOfTUIOutputSections.
func (t *TUI) writeOutputPart(role generators.Role, color taiui.Color, isThought bool, text string) {
	// Consume a pending attempt-start event even when the role does
	// not switch: consecutive attempts sharing one role still open
	// separate sections, so each attempt's output is addressable.
	var owner *outputSectionOwner
	t.mu.Lock()
	if t.pendingOwner != nil {
		owner = t.pendingOwner
		t.pendingOwner = nil
	}
	t.mu.Unlock()

	newSection := false
	if t.hasOutput && (role != t.lastOutputRole || isThought != t.lastWasThought) {
		t.separateOutput()
		newSection = true
	}
	if owner != nil && !newSection {
		if t.hasOutput {
			t.separateOutput()
		}
		newSection = true
	}
	if !t.hasOutput {
		newSection = true
	}
	if newSection {
		t.beginOutputSection(owner)
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

func (t *TUI) ensureOutputNewline() {
	t.mu.Lock()
	if t.output.HasPartial() {
		t.output.Append(taiui.NoColor, "\n")
	}
	t.mu.Unlock()
	t.notify()
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

// HandoffStart marks the beginning of a handoff generation request. It
// is called by the handoff process (see pipeline.TheoryOfHandoff) and sets
// the handoff flag so the Output tab title shows "Output (handoff...)".
func (t *TUI) HandoffStart() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.handoff = true
}

// HandoffEnd marks the end of a handoff generation request. It is called
// by the handoff process after the last attempt, success or failure, and
// clears the handoff flag so the Output tab title returns to its normal
// state.
func (t *TUI) HandoffEnd() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.handoff = false
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

func (t *TUI) handleQuitKey() bool {
	return t.quit.QuitKeyPressed()
}

func (t *TUI) cancelConfirmQuit() {
	t.quit.Cancel()
}

func (t *TUI) Stop() error {
	return t.tty.Stop()
}

func (t *TUI) handleKey(key string) bool {
	// Pointer events route through the ordinary mouse path whether or
	// not the chat input bar is focused: wheel and drag scrolling and
	// press-driven tab switching keep working while typing, and the
	// press target decides the input focus (a press on the bar's row
	// focuses it; a press elsewhere releases it). See
	// TheoryOfTUIChatInput and TheoryOfMouseSupport.
	if strings.HasPrefix(key, taiui.MouseKeyPrefix) {
		t.handleMouseKey(key)
		return false
	}
	// While the chat input bar is focused, editing keys edit the
	// pending line; every other key falls through to the dispatch below
	// so the TUI stays operable while typing. A fall-through key that
	// changes other elements — scrolling the focused pane, cycling the
	// tab focus — releases the input focus so the cursor hides, and
	// only a click on the bar's row regains it; the view snapshot taken
	// here is compared after the dispatch to detect the change. See
	// TheoryOfTUIChatInput.
	t.mu.Lock()
	inputFocused := t.inputFocused
	var viewBefore chatInputViewSnapshot
	if inputFocused {
		viewBefore = t.chatInputViewSnapshotLocked()
	}
	t.mu.Unlock()
	if t.handleChatInputKey(key) {
		return false
	}
	// taiui.ReadKeys returns generic key names ("q", "s", "?", "[", "]");
	// mapTUIKey translates the TUI's key bindings to semantic names so
	// the dispatch below reads as a table of actions, not a table of
	// characters. Generic key names the TUI does not bind (arrows,
	// function keys) pass through unchanged. See TheoryOfTUIKeyMapping.
	key = mapTUIKey(key)
	// Any key other than a quit key cancels a pending quit
	// confirmation before its normal processing, so an accidental q
	// press never loses the session. See TheoryOfTUI.
	if key != "quit" {
		t.cancelConfirmQuit()
	}
	switch {
	case key == "tab":
		t.cycleFocus()
	case key == "1":
		t.toggleTab(0)
	case key == "2":
		t.toggleTab(1)
	case key == "3":
		t.toggleTab(2)
	case key == "split":
		t.toggleSplit()
	case key == "mouse":
		t.toggleMouse()
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
	case key == "enter":
		t.toggleLastHandoff()
	case key == "help":
		t.toggleHelp()
	case key == "quit":
		// The first quit key press shows a confirmation bar; a second
		// press confirms the quit. See TheoryOfTUI.
		if t.handleQuitKey() {
			// Release any chat input waiting on the input bar so the
			// blocked generation loop can wind down as the session
			// ends. See TheoryOfTUIChatInput.
			t.cancelChatInput()
			t.mu.Lock()
			height := t.height
			t.mu.Unlock()
			fmt.Fprintf(t.tty, "\x1b[%d;1H", height)
			return true
		}
	}
	// A fall-through key that changed other elements while the input
	// was focused hands the keyboard back to navigation; a key that
	// changed nothing (a page-up already at the top) keeps the bar
	// focused. See TheoryOfTUIChatInput.
	if inputFocused {
		t.mu.Lock()
		if viewBefore != t.chatInputViewSnapshotLocked() {
			t.inputFocused = false
		}
		t.mu.Unlock()
	}
	return false
}

const TheoryOfTUIKeyMapping = `
taiui.ReadKeys returns generic, application-independent key names ("q",
"s", "?", "[", "]"); the TUI maps its key bindings to semantic names
("quit", "split", "help", "prev-transition", "next-transition") so the
key dispatch in Run reads as a table of actions rather than a table of
characters. The mapping lives in cmd/tai, not in taiui, preserving
taiui's reusability: key names stay generic, and each application defines
its own binding. Unmapped keys (arrows, function keys, mouse events) pass
through unchanged.
`

// mapTUIKey maps a generic key name from taiui.ReadKeys to the semantic
// name the TUI uses for its key bindings. See TheoryOfTUIKeyMapping.
func mapTUIKey(key string) string {
	switch key {
	case "q", "Q", "ctrl-c":
		return "quit"
	case "s", "S":
		return "split"
	case "m", "M":
		return "mouse"
	case "?":
		return "help"
	case "[":
		return "prev-transition"
	case "]":
		return "next-transition"
	default:
		return key
	}
}

// toggleSplit switches between vertical and horizontal tab splitting.
func (t *TUI) toggleSplit() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.tabs.SplitVertical = !t.tabs.SplitVertical
}

// toggleMouse switches terminal mouse reporting at runtime. While
// reporting is off, the terminal performs its own text selection and
// copy (most terminals also offer Shift+drag as a bypass while
// reporting is on) and wheel events feed the terminal scrollback;
// pressing the key again restores the TUI's pointer interaction. The
// new state is recorded as a log line in the Logs tab. The session
// is nil until Run starts — tests construct a TUI without one —
// in which case only the recorded state flips. The write and notify
// happen after the lock is released; both take the TUI lock internally.
// See TheoryOfMouseSupport and taiui.TheoryOfMouseInteraction.
func (t *TUI) toggleMouse() {
	t.mu.Lock()
	session := t.session
	enabled := !t.mouseReporting
	t.mouseReporting = enabled
	t.mu.Unlock()
	if session != nil {
		session.SetMouse(enabled)
	}
	state := "off"
	if enabled {
		state = "on"
	}
	t.writeLogs([]byte("mouse reporting " + state + "\n"))
	t.notify()
}

func (t *TUI) Run(gen func()) error {
	// The taiui.Session owns the terminal lifecycle — cursor hiding,
	// mouse reporting, key decoding, resize notification, and the
	// coalesced update channel — and recovers a panic in gen into the
	// returned error. The TUI supplies only its behavior: rendering,
	// key dispatch, resize bookkeeping, and the generation function.
	// See taiui.TheoryOfSession and TheoryOfTUI.
	sess := &taiui.Session{
		Tty:    t.tty,
		Screen: t.screen,
		Update: t.updateCh,
		Mouse:  true,
		Render: t.render,
		Key:    t.handleKey,
		OnResize: func(width, height int) {
			t.mu.Lock()
			t.width, t.height = width, height
			t.mu.Unlock()
		},
		Gen: gen,
		GenEnd: func(err error) {
			if err != nil {
				t.write([]byte(err.Error() + "\n"))
			}
			t.mu.Lock()
			// The session has ended: clear the in-flight hint with the
			// finished state. A request that returned without a finish
			// line (e.g., an error path) must not leave the hint stuck
			// on. See TheoryOfTUI.
			t.finished = true
			t.generating = false
			t.handoff = false
			t.mu.Unlock()
			t.notify()
		},
	}
	// The session reference lets the mouse key switch mouse reporting
	// at runtime (see toggleMouse); mouseReporting mirrors the Mouse
	// flag above, which starts enabled. Both are written before
	// sess.Run starts the loop, so handleKey reads them ordered.
	t.session = sess
	t.mouseReporting = true
	return sess.Run()
}

func (t *TUI) handleMouseKey(key string) {
	event, x, y, ok := taiui.ParseMouseKey(key)
	if !ok {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	switch event {
	case "wheel-up":
		t.mouse.Wheel(t.tabs, t.scrolls[:], t.width, t.height, x, y, -1)
	case "wheel-down":
		t.mouse.Wheel(t.tabs, t.scrolls[:], t.width, t.height, x, y, 1)
	case "left":
		if t.inputRowHit(x, y) {
			// A press on the chat input bar's row focuses the bar
			// instead of driving tab interaction, so the user can click
			// the input and type. See TheoryOfTUIChatInput.
			t.focusInputLocked()
			return
		}
		// A press anywhere else releases the input focus before the
		// ordinary press handling runs, so clicking a pane hands the
		// keyboard back to navigation. See TheoryOfTUIChatInput.
		t.inputFocused = false
		t.mouse.Press(t.tabs, t.scrolls[:], t.width, t.height, x, y)
		// A press on an event's display rows jumps the Output tab to
		// the section the event's attempt wrote. The jump runs before
		// the handoff toggle so it maps rows of the last-rendered
		// tree, the same ranges the toggle consumes. See
		// TheoryOfTUIOutputSections.
		t.jumpToEventAtClick(x, y)
		// A press on a handoff node's display rows toggles its
		// expansion. See TheoryOfEventTree.
		t.toggleHandoffAtClick(x, y)
	case "release":
		t.mouse.Release()
	case "leftdrag":
		t.mouse.Drag(t.tabs, t.scrolls[:], y)
	}
}

// toggleLastHandoff expands or collapses the handoff node displayed
// last in the Events tab, so Enter works on the most recent handoff
// summary without a cursor. See TheoryOfEventTree.
func (t *TUI) toggleLastHandoff() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.events.ToggleLastExpanded()
}

// toggleHandoffAtClick toggles a handoff node when a left press lands
// on its display rows in the Events tab: any row of a collapsed node
// expands it, and the header row of an expanded node collapses it, so
// clicking inside a long expanded summary never collapses it by
// accident. Presses outside the events pane's content area are no-ops.
// See TheoryOfEventTree.
func (t *TUI) toggleHandoffAtClick(x, y int) {
	if !t.tabs.Expanded[1] {
		return
	}
	box := t.tabs.Boxes(t.width, t.height)[1]
	if x < box.Left || x >= box.Right || y <= box.Top || y >= box.Bottom {
		return
	}
	// The press's screen row maps onto the tab's content row by
	// dropping the label strip and re-adding the scroll offset; the
	// tree owns the row-range matching. See taiui.TheoryOfEventTree.
	t.events.ToggleAtRow(t.scrolls[1].Offset + (y - box.Top - 1))
}

// handleEvent renders one pipeline.Run event into the Events tab. It is
// the Events tab's only content source: withTUIOutputObserver taps the
// run's event iterator and forwards every event here, so every
// Events-tab line originates from a pipeline event. Each event is filed
// into the tab's event tree by its sequence and parent numbers, so the
// tab renders the stream in tree order however the events arrive. See
// TheoryOfTUI, TheoryOfEventTree and pipeline.TheoryOfLoopEvents.
func (t *TUI) handleEvent(ev pipeline.Event) {
	lines := eventLines(ev)
	if len(lines) == 0 {
		return
	}
	t.mu.Lock()
	// The finish reason marks the end of the generation request: the
	// Output tab's "generating..." hint clears once the request has
	// returned. A new request's "generating" log re-sets it.
	if ev.Kind == pipeline.EventFinish {
		t.generating = false
	}
	// An attempt start opens the output section the attempt's streamed
	// content will fill: the next visible content part begins a section
	// owned by this event, so the Events tab can jump to the attempt's
	// output. See TheoryOfTUIOutputSections.
	if ev.Kind == pipeline.EventAttemptStart {
		t.pendingOwner = &outputSectionOwner{run: ev.Loop, seq: ev.Seq}
	}
	if t.tabs.AutoExpand(1) {
		t.scrolls[1].Follow = true
	}
	// A handoff summary with a body is the expandable node; the node's
	// elapsed time anchors on the session's start. See
	// taiui.TheoryOfEventTree.
	t.events.Add(taiui.EventNode{
		Run:        ev.Loop,
		Seq:        ev.Seq,
		ParentSeq:  ev.Parent,
		Lines:      lines,
		Expandable: ev.Kind == pipeline.EventHandoff && len(lines) > 1,
		Elapsed:    time.Since(t.startTime),
	})
	t.mu.Unlock()
	t.notify()
}

var eventEmoji = map[pipeline.EventKind]string{
	pipeline.EventAttemptStart:        "🚀",
	pipeline.EventAttemptCompleted:    "✅",
	pipeline.EventRequest:             "📡",
	pipeline.EventTruncated:           "✂️",
	pipeline.EventRetry:               "🔁",
	pipeline.EventHandoffStart:        "🤝",
	pipeline.EventHandoff:             "📝",
	pipeline.EventSynthesizedSummary:  "🧩",
	pipeline.EventUsage:               "📊",
	pipeline.EventFinish:              "🏁",
	pipeline.EventThoughtSummary:      "💭",
	pipeline.EventComponentsTriggered: "⚙️",
	pipeline.EventIdle:                "💤",
	pipeline.EventRunError:            "❌",
	pipeline.EventGoal:                "🎯",
	pipeline.EventLoopStart:           "🌳",
}

// eventLog renders one event's display line in the log color, prefixed
// by the kind's emoji; a kind missing from eventEmoji gets the question
// mark. See eventLines and TheoryOfTUI.
func eventLog(kind pipeline.EventKind, text string) []taiui.Line {
	emoji, ok := eventEmoji[kind]
	if !ok {
		emoji = "❓"
	}
	return logLines(emoji + " " + text)
}

// loopPrefix renders the attribution of a per-attempt event: "loop N
// attempt M" when the event carries a goal loop number; the given
// attempt label unchanged otherwise, so non-goal runs keep their
// display bytes unchanged — start and completion lines use the
// capitalized "Attempt M" label and usage lines the lowercase one,
// exactly as before. See TheoryOfTUI and pipeline.TheoryOfLoopEvents.
func loopPrefix(loop int, attemptLabel string) string {
	if loop != 0 {
		return fmt.Sprintf("loop %d %s", loop, strings.ToLower(attemptLabel))
	}
	return attemptLabel
}

// eventLines renders one pipeline event as Events-tab lines. The first
// line of every event starts with the kind's emoji (eventEmoji) followed
// by a bracketed label — one display style shared by all kinds, so event
// types are recognized at a glance and no style mixes brackets with
// banner equals. A completed attempt with no summary (single-shot
// commands like ai produce empty summaries) shows a completion line; a
// completed attempt with a summary shows the same header followed by the
// summary body. An unknown kind shows a generic event line, so no
// pipeline event type is silently dropped. Log-style events use the log
// color; the thought summary header uses the thought color; summary
// bodies stay plain. The attempt start, request, completion, and usage
// lines attribute the attempt to its goal loop via loopPrefix; non-goal
// runs keep the bare attempt labels. The loop-start event renders the
// line that roots its goal loop's branch. The goal event renders its
// message lines in the log color, the emoji on the first line.
func eventLines(ev pipeline.Event) []taiui.Line {
	// The "attempt x/y" budget display uses the in-generation
	// position, pairing with MaxAttempts; hand-constructed events
	// may carry only the session-wide number, which then stands in.
	inGeneration := ev.AttemptInGeneration
	if inGeneration == 0 {
		inGeneration = ev.Attempt
	}
	switch ev.Kind {
	case pipeline.EventAttemptStart:
		return eventLog(ev.Kind, fmt.Sprintf("[%s start]",
			loopPrefix(ev.Loop, fmt.Sprintf("Attempt %d", ev.Attempt))))
	case pipeline.EventRequest:
		return eventLog(ev.Kind, fmt.Sprintf("[%s request] %s",
			loopPrefix(ev.Loop, fmt.Sprintf("Attempt %d", ev.Attempt)), ev.Detail))
	case pipeline.EventAttemptCompleted:
		header := eventLog(ev.Kind, fmt.Sprintf("[%s complete]",
			loopPrefix(ev.Loop, fmt.Sprintf("Attempt %d", ev.Attempt))))
		if strings.TrimSpace(ev.Summary) == "" {
			return header
		}
		return append(header, summaryLines(ev.Summary)...)
	case pipeline.EventTruncated:
		return eventLog(ev.Kind, fmt.Sprintf("[Attempt %d truncated (attempt %d/%d): %s]",
			ev.Attempt, inGeneration, ev.MaxAttempts, ev.Detail))
	case pipeline.EventRetry:
		return eventLog(ev.Kind, fmt.Sprintf("[Retry attempt %d/%d] %v",
			inGeneration, ev.MaxAttempts, ev.Err))
	case pipeline.EventRunError:
		return eventLog(ev.Kind, fmt.Sprintf("[Run error] %v", ev.Err))
	case pipeline.EventHandoffStart:
		// Handoff events carry no budget figures: handoff generation
		// itself retries without an attempt limit, so the header
		// shows no "attempt x/y" suffix. See pipeline.TheoryOfHandoff.
		return eventLog(ev.Kind, "[Handoff started]")
	case pipeline.EventHandoff:
		return append(eventLog(ev.Kind, "[Handoff summary]"), summaryLines(ev.Summary)...)
	case pipeline.EventSynthesizedSummary:
		return append(eventLog(ev.Kind, "[Synthesized completion summary]"), summaryLines(ev.Summary)...)
	case pipeline.EventUsage:
		outcome := ""
		if ev.Detail != "" {
			outcome = " (" + ev.Detail + ")"
		}
		// SpeedSuffix carries the streaming ttft and average generation
		// speed when measured, staying empty for unmeasured usages.
		// See TheoryOfUsageTiming.
		return eventLog(ev.Kind, fmt.Sprintf("[Usage] %s%s: prompt %d, cached %d, completion %d, thoughts %d",
			loopPrefix(ev.Loop, fmt.Sprintf("attempt %d", ev.Attempt)), outcome,
			ev.Usage.Prompt.TokenCount,
			ev.Usage.Prompt.TokenCountCached,
			ev.Usage.Candidates.TokenCount,
			ev.Usage.Thoughts.TokenCount,
		)+ev.Usage.SpeedSuffix())
	case pipeline.EventFinish:
		return eventLog(ev.Kind, "[Finish: "+ev.Detail+"]")
	case pipeline.EventThoughtSummary:
		return append(
			[]taiui.Line{{Text: eventEmoji[ev.Kind] + " [Thought Summary]", Color: outputColorThoughtLine}},
			summaryLines(ev.Summary)...,
		)
	case pipeline.EventComponentsTriggered:
		return eventLog(ev.Kind, fmt.Sprintf("[Attempt %d continues] %s", ev.Attempt, ev.Detail))
	case pipeline.EventIdle:
		return eventLog(ev.Kind, "[Idle input received; starting the next generation]")
	case pipeline.EventLoopStart:
		return eventLog(ev.Kind, fmt.Sprintf("[Loop %d start]", ev.Loop))
	case pipeline.EventGoal:
		// A goal message may span lines (multi-line verdicts); every
		// line renders in the log color, the first line carrying the
		// kind's emoji. See pipeline.TheoryOfGoalMode.
		var lines []taiui.Line
		for i, line := range strings.Split(ev.Detail, "\n") {
			if i == 0 {
				line = eventEmoji[ev.Kind] + " " + line
			}
			lines = append(lines, taiui.Line{Text: line, Color: outputColorLogLine})
		}
		return lines
	default:
		if ev.Detail == "" {
			return eventLog(ev.Kind, fmt.Sprintf("[Event %s]", ev.Kind))
		}
		return eventLog(ev.Kind, fmt.Sprintf("[Event %s] %s", ev.Kind, ev.Detail))
	}
}

// logLines renders one single-line log-style event line in the log color.
func logLines(text string) []taiui.Line {
	return []taiui.Line{{Text: text, Color: outputColorLogLine}}
}

// summaryLines renders a multi-line summary body as plain lines.
func summaryLines(s string) []taiui.Line {
	var lines []taiui.Line
	for _, line := range strings.Split(s, "\n") {
		lines = append(lines, taiui.Line{Text: line})
	}
	return lines
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
	// The scroll view is the panel box minus the one-row label strip,
	// and the interactive Output tab's input bar row on top of it;
	// tuiPaneHeight applies both so the page size matches the rendered
	// pane. See TheoryOfTUIChatInput.
	paneHeight := t.tuiPaneHeight(idx, box)
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
	// minus its one-row label strip and the interactive Output tab's
	// input bar row, matching render's scroll updates. See
	// TheoryOfTUIChatInput.
	paneHeight := t.tuiPaneHeight(0, box)
	offset := taiui.ClampOffset(t.scrolls[0].Offset, len(display), paneHeight)
	// The stops come from taiui.TransitionJumpStops (each transition
	// contributes the exit stop and the entry stop) and the selection —
	// including the backward fallback to the very beginning of the
	// content — from taiui.JumpStopOffset. See
	// taiui.TheoryOfSectionNavigation.
	stops := taiui.TransitionJumpStops(display, paneHeight)
	target, ok := taiui.JumpStopOffset(stops, offset, direction)
	if !ok {
		if direction < 0 {
			return
		}
		// No stop lies ahead: the view sits at or past the last section
		// transition, or the output has a single uniform section. The
		// forward key falls back to the live tail, mirroring the
		// backward fallback to the content start, so it always moves
		// the view to a well-defined endpoint instead of silently doing
		// nothing. The tail sentinel is clamped to the content extent
		// below; landing on the last row lets the next render's Update
		// resume following the tail.
		target = 1 << 30
	}
	// The target is clamped to the content extent so the offset is valid
	// immediately; a transition beyond the last view position shows at
	// the bottom of the final view. The jump is a deliberate navigation
	// away from the live tail: following resumes only when the view
	// reaches the latest row (see ScrollState.Update).
	t.scrolls[0].Offset = taiui.ClampOffset(target, len(display), paneHeight)
	t.scrolls[0].Follow = false
}

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
		// tuiPaneHeight reserves every panel's one-row label strip plus
		// the interactive Output tab's input bar row, matching the boxes
		// buildRoot renders. See TheoryOfTUIChatInput.
		t.scrolls[idx].Update(len(displays[idx]), t.tuiPaneHeight(idx, boxes[idx]))
	}

	taiui.Render(buildRoot(t, width, height, displays), t.screen)

	// The chat input bar carries the terminal cursor while it is
	// focused: the cursor is shown on the focus gain and hidden on the
	// loss, written after the frame is presented so the sequence stays
	// serial with the screen's output. Between transitions the Input
	// element's CursorAt records the editing position in the frame and
	// the screen repositions the cursor on its own. See
	// TheoryOfTUIChatInput.
	if t.inputFocused != t.wasInputFocused {
		if t.inputFocused {
			io.WriteString(t.tty, taiui.CursorRestoreSequence)
		} else {
			io.WriteString(t.tty, taiui.CursorHideSequence)
		}
		t.wasInputFocused = t.inputFocused
	}
}

var (
	tabNames = [...]string{"Output", "Events", "Logs"}
	// tabUnfocusBG is the dark blue background of every unfocused tab.
	tabUnfocusBG int32 = 0x0a1428
	// tabFocusBG is the dark gray background of the focused tab.
	tabFocusBG       int32 = 0x2e2e2e
	tabActiveLabelFg int32 = 10
)

// withTUIOutputObserver connects a pipeline.Run to the TUI: it wraps the
// state with the tuiOutputState decorator (streaming output content to
// the Output tab) and taps the run's event iterator, forwarding every
// event to handleEvent before the command's own consumer sees it. The
// tap is the Events tab's only content source, so every Events-tab line
// originates from a pipeline.Run event. See TheoryOfTUI and
// pipeline.TheoryOfLoopEvents.
func withTUIOutputObserver(run pipeline.Run, tui *TUI) pipeline.Run {
	return func(ctx context.Context, opts pipeline.RunOptions, result *pipeline.Result) iter.Seq2[pipeline.Event, error] {
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
		inner := run(ctx, opts, result)
		return func(yield func(pipeline.Event, error) bool) {
			inner(func(ev pipeline.Event, err error) bool {
				// Tap unconditionally: the terminal EventRunError
				// arrives with a non-nil error and must render too.
				tui.handleEvent(ev)
				return yield(ev, err)
			})
		}
	}
}

// tuiShowThoughts returns whether the TUI's Output tab displays raw reasoning
// thoughts. Only the -thoughts flag governs it: -summarize-thoughts adds
// periodic summaries in the Events tab but never suppresses the raw stream,
// because blanking the focused Output tab during long thinking phases leaves
// no live feedback and makes the session look stalled. See TheoryOfTUI.
func tuiShowThoughts(thoughts flags.Thoughts) bool {
	if thoughts.Value != nil {
		return *thoughts.Value
	}
	return true
}

func forkTUIDisplay(scope dscope.Scope, tui *TUI) dscope.Scope {
	scope = scope.Fork(
		func() logs.Writer { return logs.Writer(tui.LogsWriter()) },
		// Command-level output (ping verdicts, applied notices) goes
		// to the Output tab via the dscope-resolved Output writer.
		// Generation output is captured separately and never routed
		// here; goal-mode verdicts are pipeline events rendered in the
		// Events tab through the goal event observer below. See
		// TheoryOfCommandOutput and TheoryOfTUI.
		func() Output { return Output(tui.Writer()) },
		// The TUI owns the terminal's single key reader, so interactive
		// chat reads lines through the TUI's input bar instead of a
		// liner prompt racing for the same tty. See TheoryOfTUIChatInput
		// and pipeline.TheoryOfChatInput.
		func() pipeline.ChatInput { return pipeline.ChatInput(tui.ChatInput) },
		// Handoff generation reaches the Output tab through the
		// tuiOutputState decorator, so each part is displayed with its
		// role color and thought coloring — the same path as regular
		// generation output — and the lifecycle is reported through the
		// HandoffObserver so the title shows "Output (handoff...)" while
		// a handoff request is in flight. See pipeline.TheoryOfHandoff
		// and TheoryOfTUIHandoff.
		func() pipeline.HandoffStateDecorator {
			return func(state generators.State) generators.State {
				return tuiOutputState{
					upstream: state,
					tui:      tui,
				}
			}
		},
		func() pipeline.HandoffObserver { return tui },
		// The goal runner's verdicts and failure notes — EventGoal —
		// forward to the Events tab through the same handleEvent path
		// as the run's events. See pipeline.TheoryOfGoalMode.
		func() pipeline.GoalEventObserver { return tui.handleEvent },
	)
	// Resolve the loop from the display scope so Module.Run binds the
	// Logs-pane Logger at provider-resolution time. See
	// TheoryOfTUIDisplayFork.
	loopRun := scope.Get[pipeline.Run]()
	return scope.Fork(func() pipeline.Run {
		return withTUIOutputObserver(loopRun, tui)
	})
}

func runWithTUI(app apps.App, scope dscope.Scope) {
	tui, err := newTUI()
	if err != nil {
		fmt.Fprintf(os.Stderr, "cannot start TUI: %v; continuing without TUI\n", err)
		app.Call(app.Scope(scope))
		return
	}
	// Layer the app's definitions onto the scope exactly once, before
	// anything reads from it: each fork branch evaluates providers
	// independently, so forking the same defs again would evaluate
	// side-effecting providers twice. See apps.TheoryOfApps.
	scope = app.Scope(scope)
	// The chat input bar renders only in interactive sessions: apps
	// that never call pipeline.ChatInput have no use for the bar, so the
	// Output pane keeps its full height. Interactive apps fork
	// apps.Interactive(true) into their Defs. See TheoryOfTUIChatInput.
	tui.interactive = bool(scope.Get[apps.Interactive]())
	oldOut, oldErr := os.Stdout, os.Stderr

	// The TUI is the display. Model output is captured from the
	// generation state by the tuiOutputState decorator, pipeline events
	// reach the Events tab through the iterator tap layered by
	// forkTUIDisplay, and log records
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
		app.Call(scope)
		return
	}
	os.Stdout = devNull
	pr, pw, err := os.Pipe()
	if err != nil {
		_ = devNull.Close()
		_ = tui.Stop()
		fmt.Fprintf(os.Stderr, "cannot create stderr pipe: %v; continuing without TUI\n", err)
		app.Call(scope)
		return
	}
	os.Stderr = pw
	stderrDone := make(chan struct{})
	go func() {
		defer close(stderrDone)
		_, _ = io.Copy(tui.Writer(), pr)
	}()

	// The TUI's raw-thought display is governed by -no-thoughts alone:
	// -summarize-thoughts adds periodic summaries in the Events tab but
	// never suppresses the raw stream, because blanking the focused
	// Output tab during long thinking phases leaves no live feedback and
	// makes the session look stalled. The flag is resolved from the
	// scope before the generation goroutine starts, so the policy is
	// fixed for the session. See TheoryOfTUI.
	tui.showThoughts = tuiShowThoughts(scope.Get[flags.Thoughts]())
	// Fork the display writers (Logs pane, Output tab) and rebind the
	// generation loop to them; the loop's state-decorator and event-tap
	// wrapper is layered on top so model output is captured from the
	// generation state and events reach the TUI. The loop must be
	// resolved after the writer forks — see TheoryOfTUIDisplayFork.
	scope = forkTUIDisplay(scope, tui)
	// Display the user's chat input (flags.Chats) at the top of the
	// Output tab before the command starts generating. The chat lives in
	// the initial generation state, which the tuiOutputState decorator
	// does not display; writing it here gives the user a clear view of
	// what the model was asked. See TheoryOfTUI.
	displayChatInput(tui, scope.Get[flags.Chats]())
	runErr := tui.Run(func() {
		app.Call(scope)
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

func displayChatInput(tui *TUI, chats flags.Chats) {
	if len(chats) == 0 {
		return
	}
	tui.mu.Lock()
	tui.lastOutputRole = generators.RoleUser
	tui.lastWasThought = false
	tui.hasOutput = true
	tui.mu.Unlock()
	tui.writeColored(outputColorUserLine, []byte(strings.Join(chats, "\n")+"\n"))
}
