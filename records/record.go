package records

import (
	"database/sql"
	"encoding/base64"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"cuelang.org/go/cue"

	"github.com/reusee/tai/blocks"
	"github.com/reusee/tai/configs"
	"github.com/reusee/tai/flags"
	"github.com/reusee/tai/generators"
	_ "modernc.org/sqlite"
)

const TheoryOfInteractionRecording = `
The records package implements the self-improvement mechanism: every
interaction of the tai command can be recorded in detail into a single
sqlite database file, and recorded sessions can be fed back to the model
for analysis and improvement.

Recording is centralized in the unified generation loop (pipeline.Run), which is
used by every generation command (ai, next, and the auto-detected default), so one
instrumentation point covers them all. The loop implements the
pipeline.InteractionRecorder contract: it reports the session's system prompt
and initial contents, wraps the state so every content append is captured
(user input, model output, reasoning thoughts, tool calls, retry feedback),
reports every structured block parsed from the model output (change, shell,
go-test, continue, summary, done), and reports malformed blocks via
ParseError. Attempt lifecycle events — AttemptStart, AttemptCompleted,
AttemptTruncated, AttemptError — delimit one pass through the phase chain:
an attempt opens with AttemptStart and closes with AttemptCompleted,
AttemptTruncated, or AttemptError; a generation spans all of its attempts.

Model-produced streaming contents are merged at record time: consecutive
contents with the model or assistant role are buffered and coalesced into a
single event, with adjacent Text parts (and adjacent Thought parts)
concatenated verbatim — the same semantics as generators.Content.Merge. A
streamed response therefore produces one readable content event per
contiguous run instead of one event per streaming chunk. The buffer flushes
before every other event write and before the attempt counter advances, so a
merged content never crosses an attempt boundary, a non-model content, or any
other event in the transcript.

Recording is enabled by the -record flag (or the "record" config path) and
disabled by -no-record. When disabled, the Recorder still opens the database
so the record subcommand can query sessions, but no events are written.

Storage layout: a single sqlite file (tai-interactions.db) in the user config
directory, using WAL journal mode and a busy timeout so multiple tai
processes can append concurrently. Two tables: sessions (one row per tai
invocation) and events (the chronological event stream). Each event row
carries a type tag, the attempt sequence number (stored in the round
column, whose name is kept because CREATE TABLE IF NOT EXISTS cannot
migrate existing databases), a timestamp, and a text detail. A session is
reconstructed as a readable transcript by ordering its events. The
default database path is overridable via the DBPath provider (tests use
a temporary directory).

The analysis pass (records.RunAnalysis) renders a session transcript and
sends it to the configured model with a purpose-built system prompt asking
for an assessment of what went well, what went wrong, root causes, and
concrete improvements. This closes the self-improvement loop: interactions
are recorded, analyzed, and the findings inform prompt and tool changes.

Recording is best-effort: database errors are ignored so recording never
interferes with the generation pipeline. Event details are recorded in full:
nothing is omitted or truncated. The transcript therefore faithfully reflects
the entire interaction — system prompts, user input, model output, reasoning
thoughts, tool calls, file attachments (binary content encoded as base64),
structured blocks, parse errors, and retry feedback.
`

const TheoryOfEventRecording = `
The event stream is open-ended: Recorder.Event captures arbitrary typed
events (type + detail) alongside the structured lifecycle events. The
generation loop records flow decisions — retries with attempt counts,
parse-error corrections, component-triggered generations, generator
selection, and the command line — and generator implementations record
API-level events (api_call, api_error) through the dscope-injected
generators.EventRecorder (see generators.TheoryOfEventRecorder). Together
these capture errors returned by model APIs and important pipeline
decisions that would otherwise be visible only in logs, giving the
analysis pass a complete view of what happened during the interaction.
`

// DBPath is the path of the interaction sqlite database file. The default
// provider places it in the user config directory so it persists across
// sessions; tests override it with a temporary directory.
// See TheoryOfInteractionRecording.
type DBPath string

func (Module) DBPath() DBPath {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return DBPath(filepath.Join(dir, "tai-interactions.db"))
}

// Enabled controls whether interaction recording is active. Recording is
// enabled by the -record flag or the "record" config path.
// See TheoryOfInteractionRecording.
type Enabled bool

func (Module) Enabled() Enabled {
	return false
}

var _ configs.Config = Enabled(false)

var _ flags.Flag = Enabled(false)

func (e Enabled) Handle(key string, args []string) (newDef any, remainArgs []string, err error) {
	switch key {
	case "-record":
		ret := Enabled(true)
		return &ret, args, nil
	case "-no-record":
		ret := Enabled(false)
		return &ret, args, nil
	}
	panic("key not handle: " + key)
}

func (e Enabled) Keys() map[string]string {
	return map[string]string{
		"-record":    "Record interaction sessions for self-improvement analysis",
		"-no-record": "Disable interaction recording",
	}
}

func (e Enabled) ConfigPaths() []string {
	return []string{"record"}
}

func (e Enabled) HandleConfig(path string, values []*cue.Value) (any, error) {
	var b bool
	if err := values[0].Decode(&b); err != nil {
		return nil, err
	}
	ret := Enabled(b)
	return &ret, nil
}

type Recorder struct {
	db *sql.DB

	mu        sync.Mutex
	enabled   bool
	sessionID int64
	attempt   int

	// pendingModel buffers model-produced streaming contents (model or
	// assistant role) until the next event write or attempt boundary, so
	// consecutive streaming chunks are recorded as one merged event
	// instead of one event per chunk. Guarded by mu.
	// See TheoryOfInteractionRecording.
	pendingModel *generators.Content
}

const schema = `
CREATE TABLE IF NOT EXISTS sessions (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    command TEXT NOT NULL,
    start_time TEXT NOT NULL,
    end_time TEXT,
    status TEXT NOT NULL DEFAULT 'running',
    error TEXT
);
CREATE TABLE IF NOT EXISTS events (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id INTEGER NOT NULL,
    round INTEGER NOT NULL DEFAULT 0,
    time TEXT NOT NULL,
    type TEXT NOT NULL,
    detail TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_events_session ON events(session_id);
`

// Recorder provider: opens the interaction database and returns a recorder.
// A nil recorder is returned when the database cannot be opened (unknown
// path, unwritable directory, unavailable driver) so callers can treat a
// nil recorder as "recording unavailable". The database is opened even when
// recording is disabled so the record subcommand can query sessions.
// See TheoryOfInteractionRecording.
func (Module) Recorder(
	dbPath DBPath,
	flagEnabled Enabled,
) *Recorder {
	if dbPath == "" {
		return nil
	}
	dsn := "file:" + url.PathEscape(string(dbPath)) +
		"?_pragma=busy_timeout(10000)&_pragma=journal_mode(WAL)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil
	}
	return &Recorder{
		db:      db,
		enabled: bool(flagEnabled),
	}
}

// Enabled reports whether the recorder writes events. A nil recorder
// (database unavailable) or a disabled one is not enabled.
func (r *Recorder) Enabled() bool {
	return r != nil && r.enabled
}

func (r *Recorder) StartSession(command string) {
	if !r.Enabled() {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.attempt = 0
	r.pendingModel = nil
	res, err := r.db.Exec(
		`INSERT INTO sessions (command, start_time) VALUES (?, ?)`,
		command, time.Now().Format(time.RFC3339),
	)
	if err != nil {
		return
	}
	r.sessionID, _ = res.LastInsertId()
}

// EndSession closes the current session with the given outcome. A non-nil
// error marks the session as failed.
func (r *Recorder) EndSession(err error) {
	if !r.Enabled() || r.sessionID == 0 {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.flushPendingModelLocked()
	status := "success"
	var errMsg string
	if err != nil {
		status = "error"
		errMsg = err.Error()
	}
	_, _ = r.db.Exec(
		`UPDATE sessions SET end_time = ?, status = ?, error = ? WHERE id = ?`,
		time.Now().Format(time.RFC3339), status, errMsg, r.sessionID,
	)
	r.sessionID = 0
}

// SystemPrompt records the session's system prompt.
func (r *Recorder) SystemPrompt(prompt string) {
	if !r.Enabled() {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.flushPendingModelLocked()
	r.insertEventLocked(0, "system_prompt", prompt)
}

// AttemptStart marks the beginning of a generation attempt. Buffered
// model contents are flushed into the previous attempt before the
// counter advances, so a merged content never crosses an attempt
// boundary.
func (r *Recorder) AttemptStart() {
	if !r.Enabled() {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.flushPendingModelLocked()
	r.attempt++
	r.insertEventLocked(r.attempt, "attempt_start", "")
}

// AttemptCompleted marks an attempt that completed normally, carrying
// the summary block bodies as detail.
func (r *Recorder) AttemptCompleted(summaries []string) {
	if !r.Enabled() {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.flushPendingModelLocked()
	detail := "success"
	if len(summaries) > 0 {
		detail += "\n" + strings.Join(summaries, "\n")
	}
	r.insertEventLocked(r.attempt, "attempt_end", detail)
}

// AttemptTruncated marks an attempt that ended without a completion
// signal (no summary block or abnormal finish reason) and was retried.
func (r *Recorder) AttemptTruncated() {
	if !r.Enabled() {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.flushPendingModelLocked()
	r.insertEventLocked(r.attempt, "attempt_end", "truncated")
}

// AttemptError marks an attempt that failed with an error.
func (r *Recorder) AttemptError(err error) {
	if !r.Enabled() {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.flushPendingModelLocked()
	detail := "error"
	if err != nil {
		detail += "\n" + err.Error()
	}
	r.insertEventLocked(r.attempt, "attempt_end", detail)
}

func (r *Recorder) Content(content *generators.Content) {
	if !r.Enabled() {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if content == nil {
		return
	}
	if isModelRole(content.Role) {
		if r.pendingModel == nil {
			r.pendingModel = content
			return
		}
		if merged, ok := r.pendingModel.Merge(content); ok {
			r.pendingModel = merged
			return
		}
		// The roles differ (model vs assistant): close the current
		// merged run and start a new one.
		r.flushPendingModelLocked()
		r.pendingModel = content
		return
	}
	r.flushPendingModelLocked()
	detail := contentDetail(content)
	if detail == "" {
		return
	}
	r.insertEventLocked(r.attempt, "content_"+string(content.Role), detail)
}

// isModelRole reports whether the role carries model-produced streaming
// output. Generators emit either RoleModel (Gemini) or RoleAssistant
// (OpenAI-compatible); both are merged at record time. See
// TheoryOfInteractionRecording.
func isModelRole(role generators.Role) bool {
	return role == generators.RoleModel || role == generators.RoleAssistant
}

func (r *Recorder) Block(block blocks.Block) {
	if !r.Enabled() {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.flushPendingModelLocked()
	kind := block.Kind
	if kind == "" {
		kind = "unknown"
	}
	r.insertEventLocked(r.attempt, "block_"+kind, blockDetail(block))
}

func (r *Recorder) ParseError(parseErr *blocks.BlockParseError) {
	if !r.Enabled() {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.flushPendingModelLocked()
	var b strings.Builder
	fmt.Fprintf(&b, "kind=%s boundary=%s", parseErr.BlockKind, parseErr.Boundary)
	if parseErr.Line > 0 {
		fmt.Fprintf(&b, " line=%d", parseErr.Line)
	}
	if parseErr.Reason != "" {
		fmt.Fprintf(&b, "\nreason: %s", parseErr.Reason)
	}
	if len(parseErr.Hints) > 0 {
		b.WriteString("\nhints:\n" + strings.Join(parseErr.Hints, "\n"))
	}
	if parseErr.Content != "" {
		b.WriteString("\ncontent:\n" + parseErr.Content)
	}
	r.insertEventLocked(r.attempt, "parse_error", b.String())
}

// Event records an arbitrary session event with the current attempt
// number. The generation loop uses it for flow decisions (retries,
// parse-error corrections, component-triggered generations, session
// metadata), and generator implementations use it through the
// dscope-injected EventRecorder for API-level events (api_call,
// api_error). The event carries a type and a free-form detail; the
// transcript renders each event by its type. An empty detail is skipped.
// See TheoryOfEventRecording.
func (r *Recorder) Event(typ string, detail string) {
	if !r.Enabled() {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.flushPendingModelLocked()
	r.insertEventLocked(r.attempt, typ, detail)
}

// flushPendingModelLocked writes the buffered model-produced contents as a
// single merged event and clears the buffer. It is called before every
// other event write and — in AttemptStart — before the attempt counter
// advances, so a merged content never crosses an attempt boundary or
// another event in the transcript. See TheoryOfInteractionRecording.
func (r *Recorder) flushPendingModelLocked() {
	pending := r.pendingModel
	r.pendingModel = nil
	if pending == nil {
		return
	}
	detail := contentDetail(pending)
	if detail == "" {
		return
	}
	r.insertEventLocked(r.attempt, "content_"+string(pending.Role), detail)
}

// insertEventLocked writes one event row. The column is named round for
// schema compatibility (CREATE TABLE IF NOT EXISTS cannot migrate
// existing databases); it stores the session-wide monotonic attempt
// sequence. See TheoryOfInteractionRecording.
func (r *Recorder) insertEventLocked(attempt int, typ, detail string) {
	if r.db == nil || r.sessionID == 0 {
		return
	}
	if detail == "" && typ != "attempt_start" {
		return
	}
	_, _ = r.db.Exec(
		`INSERT INTO events (session_id, round, time, type, detail) VALUES (?, ?, ?, ?, ?)`,
		r.sessionID, attempt, time.Now().Format(time.RFC3339Nano), typ, detail,
	)
}

func contentDetail(content *generators.Content) string {
	if content == nil {
		return ""
	}
	var parts []string
	for _, part := range content.Parts {
		switch p := part.(type) {
		case generators.Text:
			if len(p) > 0 {
				parts = append(parts, string(p))
			}
		case generators.Thought:
			if len(p) > 0 {
				parts = append(parts, "[thought]\n"+string(p))
			}
		case generators.FuncCall:
			parts = append(parts, fmt.Sprintf("[function call] %s(%v)", p.Name, p.Arguments))
		case generators.CallResult:
			parts = append(parts, fmt.Sprintf("[call result] %s(%v)", p.Name, p.Results))
		case generators.FinishReason:
			parts = append(parts, "[finish] "+string(p))
		case generators.Usage:
			parts = append(parts, fmt.Sprintf("[usage] prompt=%d cached=%d completion=%d thoughts=%d",
				p.Prompt.TokenCount, p.Prompt.TokenCountCached, p.Candidates.TokenCount, p.Thoughts.TokenCount))
		case generators.Error:
			if p.Error != nil {
				parts = append(parts, "[error] "+p.Error.Error())
			}
		case generators.FileURL:
			parts = append(parts, "[file] "+string(p))
		case generators.FileContent:
			// The full attachment content is recorded so no information
			// from the interaction is lost. Binary content is encoded as
			// base64 to keep the text transcript parseable.
			parts = append(parts, fmt.Sprintf("[file content: %s, base64]\n%s",
				p.MimeType, base64.StdEncoding.EncodeToString(p.Content)))
		}
	}
	return strings.Join(parts, "\n")
}

func blockDetail(block blocks.Block) string {
	var b strings.Builder
	kind := block.Kind
	if kind == "" {
		kind = "unknown"
	}
	fmt.Fprintf(&b, "kind=%s boundary=%s", kind, block.Boundary)
	keys := make([]string, 0, len(block.Attributes))
	for k := range block.Attributes {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Fprintf(&b, " %s=%q", k, block.Attributes[k])
	}
	if block.Body != "" {
		b.WriteString("\n")
		b.WriteString(block.Body)
	}
	return b.String()
}

// RecordSession starts recording a session for the given command and returns
// a cleanup function that ends the session. When the recorder is nil or
// disabled, the returned function is a no-op. A panic after session start is
// recorded as a failed session and re-panicked. Usage:
//
//	defer records.RecordSession(recorder, "ai")()
//
// See TheoryOfInteractionRecording.
func RecordSession(recorder *Recorder, command string) func() {
	if recorder == nil || !recorder.Enabled() {
		return func() {}
	}
	recorder.StartSession(command)
	return func() {
		if r := recover(); r != nil {
			recorder.EndSession(fmt.Errorf("panic: %v", r))
			panic(r)
		}
		recorder.EndSession(nil)
	}
}
