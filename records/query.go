package records

import (
	"database/sql"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/reusee/tai/tree"
)

// SessionInfo describes one recorded session for listing.
type SessionInfo struct {
	ID          int64
	Command     string
	CommandLine string
	StartTime   string
	EndTime     string
	Status      string
	Error       string
	OpCount     int
}

// listSessions writes one key=value metadata line per session, most
// recent first, to output. See TheoryOfInteractionRecording.
func listSessions(recorder *Recorder, limit int, output io.Writer) error {
	if recorder == nil || recorder.db == nil {
		return fmt.Errorf("session database not available")
	}
	rows, err := recorder.db.Query(`
SELECT s.id, s.command, s.start_time, COALESCE(s.end_time, ''), s.status, COALESCE(s.error, ''), COUNT(o.id)
FROM sessions s
LEFT JOIN tree_ops o ON o.session_id = s.id
GROUP BY s.id
ORDER BY s.id DESC
LIMIT ?`, limit)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var info SessionInfo
		if err := rows.Scan(&info.ID, &info.Command, &info.StartTime, &info.EndTime, &info.Status, &info.Error, &info.OpCount); err != nil {
			return err
		}
		fmt.Fprintf(output, "id=%d command=%s start=%s status=%s operations=%d\n",
			info.ID, info.Command, info.StartTime, info.Status, info.OpCount)
	}
	return rows.Err()
}

// Transcript renders a session as readable text: the session metadata
// followed by the recorded operation stream rendered as an event stream —
// one event per applied operation in application order, each carrying
// its metadata and the node content it wrote. Used for display and as
// the input to the analysis pass. See TheoryOfInteractionRecording.
func Transcript(recorder *Recorder, sessionID int64) (string, error) {
	if recorder == nil || recorder.db == nil {
		return "", fmt.Errorf("session database not available")
	}
	var command, commandLine, startTime, endTime, status, errMsg string
	err := recorder.db.QueryRow(
		`SELECT command, command_line, start_time, COALESCE(end_time, ''), status, COALESCE(error, '')
		 FROM sessions WHERE id = ?`,
		sessionID,
	).Scan(&command, &commandLine, &startTime, &endTime, &status, &errMsg)
	if err != nil {
		return "", err
	}
	ops, err := loadOps(recorder, sessionID)
	if err != nil {
		return "", err
	}

	var b strings.Builder
	fmt.Fprintf(&b, "=== Session %d ===\n", sessionID)
	fmt.Fprintf(&b, "command=%s\n", command)
	if commandLine != "" {
		fmt.Fprintf(&b, "command_line=%s\n", commandLine)
	}
	fmt.Fprintf(&b, "start=%s\n", startTime)
	if endTime != "" {
		fmt.Fprintf(&b, "end=%s\n", endTime)
	}
	fmt.Fprintf(&b, "status=%s\n", status)
	if errMsg != "" {
		fmt.Fprintf(&b, "error=%s\n", errMsg)
	}
	fmt.Fprintf(&b, "operations=%d\n", len(ops))

	b.WriteString("\nevents:\n")
	for _, op := range ops {
		writeEventText(&b, op)
	}
	return b.String(), nil
}

// loadOps reads a session's operation stream in application order.
func loadOps(recorder *Recorder, sessionID int64) ([]tree.Op, error) {
	rows, err := recorder.db.Query(
		`SELECT time, kind, parent, name, type, author, content
		 FROM tree_ops WHERE session_id = ? ORDER BY seq`,
		sessionID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ops []tree.Op
	for rows.Next() {
		var op tree.Op
		var timeText, kind, typ, author string
		if err := rows.Scan(&timeText, &kind, &op.Parent, &op.Name, &typ, &author, &op.Content); err != nil {
			return nil, err
		}
		if parsed, perr := time.Parse(time.RFC3339Nano, timeText); perr == nil {
			op.Time = parsed
		}
		op.Kind = tree.OpKind(kind)
		op.Type = tree.Type(typ)
		op.Author = tree.Author(author)
		ops = append(ops, op)
	}
	return ops, rows.Err()
}

// writeEventText renders one recorded operation as an event line: the
// operation's target node name followed by its metadata as key=value
// pairs — kind, type, author, parent, and time, empty fields omitted —
// with the content the operation wrote following as indented lines, so
// the transcript keeps the applied history with every node's content.
func writeEventText(b *strings.Builder, op tree.Op) {
	fmt.Fprintf(b, "%s kind=%s", op.Name, op.Kind)
	if op.Type != "" {
		fmt.Fprintf(b, " type=%s", op.Type)
	}
	if op.Author != "" {
		fmt.Fprintf(b, " author=%s", op.Author)
	}
	if op.Parent != "" {
		fmt.Fprintf(b, " parent=%s", op.Parent)
	}
	fmt.Fprintf(b, " time=%s\n", op.Time.Format(time.RFC3339Nano))
	if op.Content != "" {
		for _, line := range strings.Split(op.Content, "\n") {
			fmt.Fprintf(b, "| %s\n", line)
		}
	}
}

// showSession writes the transcript of a session to output.
func showSession(recorder *Recorder, sessionID int64, output io.Writer) error {
	text, err := Transcript(recorder, sessionID)
	if err != nil {
		if err == sql.ErrNoRows {
			return fmt.Errorf("session %d not found", sessionID)
		}
		return err
	}
	_, err = io.WriteString(output, text)
	return err
}

// latestSessionID returns the id of the most recent session, or 0 when
// no session has been recorded.
func latestSessionID(recorder *Recorder) (int64, error) {
	if recorder == nil || recorder.db == nil {
		return 0, fmt.Errorf("session database not available")
	}
	var id int64
	err := recorder.db.QueryRow(`SELECT id FROM sessions ORDER BY id DESC LIMIT 1`).Scan(&id)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	return id, err
}

// ShowSession writes the transcript of a session to output. The recorder
// is bound from the dscope scope, so callers pass only the runtime
// values (the session id and the output writer). See
// TheoryOfInteractionRecording.
type ShowSession func(sessionID int64, output io.Writer) error

func (Module) ShowSession(recorder *Recorder) ShowSession {
	return func(sessionID int64, output io.Writer) error {
		return showSession(recorder, sessionID, output)
	}
}

// ListSessions writes a table of recent sessions, most recent first, to
// output. The recorder is bound from the dscope scope, so callers pass
// only the runtime values (the limit and the output writer). See
// TheoryOfInteractionRecording.
type ListSessions func(limit int, output io.Writer) error

func (Module) ListSessions(recorder *Recorder) ListSessions {
	return func(limit int, output io.Writer) error {
		return listSessions(recorder, limit, output)
	}
}
