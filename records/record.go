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
database file. A transcript is the reconstruction of the tree from the
operation stream (tree.Replay), rendered node by node with its full
content: the whole session is recoverable from what was recorded —
system prompt, user inputs, model output and reasoning thoughts,
generated blocks, their processing results, loop and attempt structure,
event nodes, and errors — with no second, parallel transcript. The
session listing, the transcript header, and every tree node line render
metadata uniformly as key=value pairs.

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

The analysis pass (records.RunAnalysis) renders a session's tree and
sends it to the configured model with a purpose-built system prompt
asking for an assessment of what went well, what went wrong, root
causes, and concrete improvements. This closes the self-improvement
loop: interactions are recorded, analyzed, and the findings inform
prompt and tool changes.

Recording is best-effort: database errors are ignored so recording never
interferes with the generation pipeline. Node content is recorded in
full: nothing is omitted or truncated.
`

const TheoryOfEventRecording = `
Generator-level events are tree nodes. Generator implementations hold
the dscope-injected generators.EventRecorder and write events such as
api_call and api_error into the scope's generators.EventSink; the
generation loop drains the sink after every round and records each
buffered event as a session-tree event node, so API-level occurrences
join the same operation stream as the rest of the session
(generators.TheoryOfEventRecorder). The recorder therefore needs no
separate event path: it records the tree, and every event is a tree
node.
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
// reconstructed with tree.Replay. See TheoryOfInteractionRecording.
type Recorder struct {
	db          *sql.DB
	commandLine string

	mu        sync.Mutex
	enabled   bool
	sessionID int64
	seq       int64
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
// exactly how the session was launched.
func (r *Recorder) StartSession(command string) {
	if !r.Enabled() {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sessionID = 0
	r.seq = 0
	res, err := r.db.Exec(
		`INSERT INTO sessions (command, command_line, start_time) VALUES (?, ?, ?)`,
		command, r.commandLine, time.Now().Format(time.RFC3339Nano),
	)
	if err != nil {
		return
	}
	r.sessionID, _ = res.LastInsertId()
}

// EndSession closes the current session with the given outcome. A
// non-nil error marks the session as failed.
func (r *Recorder) EndSession(err error) {
	if !r.Enabled() || r.sessionID == 0 {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
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
// the write moment. See TheoryOfInteractionRecording.
func (r *Recorder) record(op tree.Op) {
	if r == nil || !r.enabled {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.db == nil || r.sessionID == 0 {
		return
	}
	r.seq++
	t := op.Time
	if t.IsZero() {
		t = time.Now()
	}
	_, _ = r.db.Exec(
		`INSERT INTO tree_ops (session_id, seq, time, kind, parent, name, type, author, content)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.sessionID, r.seq, t.Format(time.RFC3339Nano),
		string(op.Kind), op.Parent, op.Name,
		string(op.Type), string(op.Author), op.Content,
	)
}
