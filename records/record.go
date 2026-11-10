package records

import (
	"database/sql"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"cuelang.org/go/cue"

	"github.com/reusee/tai/configs"
	"github.com/reusee/tai/flags"
	"github.com/reusee/tai/tree"
	_ "modernc.org/sqlite"
)

const TheoryOfInteractionRecording = `
The records package implements the self-improvement mechanism: every
interaction of the tai command is persisted as the session's tree
operation stream, and recorded sessions are fed back to the model for
analysis and improvement.

Recording is the tree itself. Every mutation of the session tree — every
node the generation loop, the goal runner, and the model's block
components write — is one operation (tree.TheoryOfOperationLog), and the
recorder persists the operations in application order into one sqlite
database file. A transcript renders the recorded operation stream as an
event stream: one boundary-delimited block per applied operation in
application order, block kind "event", with the operation's metadata —
name, kind, type, author, parent, and time, empty fields omitted —
percent-encoded into the opening header's URI query, and the content the
operation wrote as the block body, so a program parses the transcript
with the standard block parser. Deletes appear as their own events
(kind=delete), so the transcript shows the applied history instead of a
tree rebuilt from it (no tree.Replay rendering). The block delimiter is
drawn at random per event — two characters from the Han range — and
redrawn while the event's content contains it, so the body never
collides with its own opening or closing marker. Randomness replaces a
fixed preset because recorded content is arbitrary: program code and
model-emitted blocks may contain any given pair, so no fixed list can be
guaranteed absent, while two independently drawn Han characters make a
coincidental match vanishingly rare. When a bounded number of draws all
collide, the event cannot be rendered unambiguously and the transcript
fails instead of emitting a corrupt block. The whole session
is recoverable from what was recorded — system prompt, user inputs,
model output and reasoning thoughts, generated blocks, their processing
results, loop and attempt structure, event nodes, and errors — with no
second, parallel transcript. The session listing and the transcript
header render metadata as key=value pairs.

A session is one recording lifetime: one generation run, or one goal run
spanning its loops. The recorder is attached to the tree with
tree.Tree.WithOpSink; the sink is shared by every version derived from
the tree, so one attachment covers the session's writes, including the
versions a continued run receives through its continuation.

Storage layout: a single sqlite file (tai-tree.db) in the user config
directory, using WAL journal mode and a busy timeout so multiple tai
processes can append concurrently. Two tables: sessions (one row per
recording lifetime) and tree_ops (the ordered operation stream: kind,
parent, name, type, author, content, time). The default database path is
overridable via the DBPath provider (tests use a temporary directory).

Recording is enabled by the -record flag (or the "record" config path)
and disabled by -no-record. When disabled, the recorder still opens the
database so the record subcommand can query sessions, but no operations
are written.

The analysis pass (records.RunAnalysis) renders a session's transcript —
the event stream of its recorded operations — and sends it to the
configured model with a purpose-built system prompt asking for an
assessment of what went well, what went wrong, root causes, and concrete
improvements. This closes the self-improvement loop: interactions are
recorded, analyzed, and the findings inform prompt and tool changes.

Recording is best-effort: database errors are ignored so recording never
interferes with the generation pipeline. Node content is recorded in
full: nothing is omitted or truncated.
`

const TheoryOfRecordedSessionLazyStart = `
A session row is written with the first recorded operation: a run that
records nothing leaves no row, so the record list never shows a record
without content, and the row's start time is the run's start rather than
its first operation's. A row is the promise that a record can be opened;
an empty row breaks that promise, and the browser can only report the
broken promise after the user pressed a record that cannot answer.

A failed operation write stops the session at the last recorded
operation. A hole in the middle — an operation skipped while later ones
land — would leave every later operation that references the missing
node unapplied, so the record would open with less of the session than
it holds; the viewer shows what applies and states what it skipped (see
tree.TheoryOfReplayTolerance). A truncated prefix is still an applied
history, so the session ends there with the failure recorded in the row.

The promise survives in the form the record can still keep: every press
answers. A truncated prefix reconstructs whole; a stream that diverged
opens to the operations that applied, with the reason at the screen's
bottom row (see cmd/tai.TheoryOfRecordOpenFailure).
`

const TheoryOfEventRecording = `
Generator-level events are tree nodes. The recording mechanism itself — the
generator-held EventRecorder, the per-scope EventSink, and the loop's drain
after every round — lives in generators.TheoryOfEventRecorder. The recorder
therefore needs no separate event path: it records the tree, and every
event is a tree node.
`

// DBPath is the path of the session tree sqlite database file. The
// default provider places it in the user config directory so it persists
// across sessions; tests override it with a temporary directory.
// See TheoryOfInteractionRecording.
type DBPath string

func (Module) DBPath() DBPath {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return DBPath(filepath.Join(dir, "tai-tree.db"))
}

// Enabled controls whether session recording is active. Recording is
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
		"-record":    "Record session trees for self-improvement analysis",
		"-no-record": "Disable session recording",
	}
}

func (e Enabled) ConfigPaths() []string {
	return []string{"record"}
}

func (e Enabled) HandleConfig(path string, values []*cue.Value) (any, error) {
	return configs.DecodeConfig[Enabled](values)
}

// Recorder writes the session tree's operation stream into the sqlite
// database. A session spans one recording lifetime — one generation run,
// or one goal run spanning its loops — and the tree's operations are
// written in application order; the tree the analysis sees is
// reconstructed with tree.Replay. The session's row is written with the
// first operation, so a run without operations leaves no row, and a write
// outside StartSession and EndSession records nothing. See
// TheoryOfInteractionRecording and TheoryOfRecordedSessionLazyStart.
type Recorder struct {
	db          *sql.DB
	commandLine string

	mu        sync.Mutex
	enabled   bool
	sessionID int64
	seq       int64
	// pending reports that StartSession registered a session: only a
	// registered session writes its row and its operations. started
	// reports that the row exists; command and startedAt carry the
	// metadata until then. See TheoryOfRecordedSessionLazyStart.
	pending   bool
	started   bool
	command   string
	startedAt time.Time
}

const schema = `
CREATE TABLE IF NOT EXISTS sessions (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    command TEXT NOT NULL,
    command_line TEXT NOT NULL DEFAULT '',
    start_time TEXT NOT NULL,
    end_time TEXT,
    status TEXT NOT NULL DEFAULT 'running',
    error TEXT
);
CREATE TABLE IF NOT EXISTS tree_ops (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id INTEGER NOT NULL,
    seq INTEGER NOT NULL,
    time TEXT NOT NULL,
    kind TEXT NOT NULL,
    parent TEXT NOT NULL DEFAULT '',
    name TEXT NOT NULL,
    type TEXT NOT NULL,
    author TEXT NOT NULL,
    content TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_tree_ops_session ON tree_ops(session_id, seq);
`

// Recorder provider: opens the session tree database and returns a
// recorder. A nil recorder is returned when the database cannot be
// opened (unknown path, unwritable directory, unavailable driver), so
// callers can treat a nil recorder as "recording unavailable". The
// database is opened even when recording is disabled so the record
// subcommand can query sessions. See TheoryOfInteractionRecording.
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
		db:          db,
		enabled:     bool(flagEnabled),
		commandLine: strings.Join(os.Args, " "),
	}
}

// Enabled reports whether the recorder writes operations. A nil recorder
// (database unavailable) or a disabled one is not enabled.
func (r *Recorder) Enabled() bool {
	return r != nil && r.enabled
}

// StartSession begins a recording session for the given command. The
// recorded command line is the process invocation, so the analysis sees
// exactly how the session was launched. The session's row is written by
// the first recorded operation, so a run without operations leaves no
// row. See TheoryOfRecordedSessionLazyStart.
func (r *Recorder) StartSession(command string) {
	if !r.Enabled() {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sessionID = 0
	r.seq = 0
	r.pending = true
	r.started = false
	r.command = command
	r.startedAt = time.Now()
}

// EndSession closes the current session with the given outcome. A
// non-nil error marks the session as failed. A session that recorded no
// operation has no row, so there is nothing to close, and the session
// closes to no pending state so a later stray write records nothing.
// See TheoryOfRecordedSessionLazyStart.
func (r *Recorder) EndSession(err error) {
	if !r.Enabled() {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.pending {
		return
	}
	if r.started && r.sessionID != 0 {
		status := "success"
		var errMsg string
		if err != nil {
			status = "error"
			errMsg = err.Error()
		}
		_, _ = r.db.Exec(
			`UPDATE sessions SET end_time = ?, status = ?, error = ? WHERE id = ?`,
			time.Now().Format(time.RFC3339Nano), status, errMsg, r.sessionID,
		)
	}
	r.pending = false
	r.started = false
	r.sessionID = 0
}

// Sink returns the tree operation sink bound to this recorder: attach it
// with tree.Tree.WithOpSink and every applied operation — and therefore
// the whole session — is written to the session's operation stream. A
// nil recorder yields a nil sink, leaving the tree without an
// attachment. See TheoryOfInteractionRecording.
func (r *Recorder) Sink() tree.OpSink {
	if r == nil {
		return nil
	}
	return r.record
}

// record writes one operation. Operations that carry no time — a delete,
// whose mutation moment is not part of the node identity — fall back to
// the write moment. The first recorded operation writes the session's row,
// so a session without operations leaves none, and a write outside a
// registered session records nothing. A failed write stops the session at
// the last recorded operation, so the stream stays an applied history.
// See TheoryOfRecordedSessionLazyStart and TheoryOfInteractionRecording.
func (r *Recorder) record(op tree.Op) {
	if r == nil || !r.enabled {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.db == nil || !r.pending {
		return
	}
	if !r.started {
		startedAt := r.startedAt
		if startedAt.IsZero() {
			startedAt = time.Now()
		}
		res, err := r.db.Exec(
			`INSERT INTO sessions (command, command_line, start_time) VALUES (?, ?, ?)`,
			r.command, r.commandLine, startedAt.Format(time.RFC3339Nano),
		)
		if err != nil {
			r.pending = false
			return
		}
		id, err := res.LastInsertId()
		if err != nil {
			r.pending = false
			return
		}
		r.sessionID = id
		r.started = true
	}
	if r.sessionID == 0 {
		return
	}
	t := op.Time
	if t.IsZero() {
		t = time.Now()
	}
	seq := r.seq + 1
	_, err := r.db.Exec(
		`INSERT INTO tree_ops (session_id, seq, time, kind, parent, name, type, author, content)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.sessionID, seq, t.Format(time.RFC3339Nano),
		string(op.Kind), op.Parent, op.Name,
		string(op.Type), string(op.Author), op.Content,
	)
	if err != nil {
		r.stopOnFailure(err)
		return
	}
	r.seq = seq
}

// stopOnFailure ends the session at the last recorded operation after a
// write failure: the recorded prefix is the applied history, so the record
// still replays whole, while a hole would leave every later operation that
// references the missing node unapplied and the record would open with less
// of the session than it holds. The caller holds the lock. See
// TheoryOfRecordedSessionLazyStart.
func (r *Recorder) stopOnFailure(err error) {
	r.pending = false
	r.started = false
	if r.sessionID != 0 && r.db != nil {
		_, _ = r.db.Exec(
			`UPDATE sessions SET end_time = ?, status = ?, error = ? WHERE id = ?`,
			time.Now().Format(time.RFC3339Nano), "error", err.Error(), r.sessionID,
		)
	}
	r.sessionID = 0
}
