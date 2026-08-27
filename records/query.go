package records

import (
	"database/sql"
	"fmt"
	"io"
	"strings"
)

// SessionInfo describes one recorded session for listing.
type SessionInfo struct {
	ID         int64
	Command    string
	StartTime  string
	EndTime    string
	Status     string
	Error      string
	EventCount int
}

// listSessions writes a table of recent sessions, most recent first, to
// output. See TheoryOfInteractionRecording.
func listSessions(recorder *Recorder, limit int, output io.Writer) error {
	if recorder == nil || recorder.db == nil {
		return fmt.Errorf("interaction database not available")
	}
	rows, err := recorder.db.Query(`
SELECT s.id, s.command, s.start_time, COALESCE(s.end_time, ''), s.status, COALESCE(s.error, ''), COUNT(e.id)
FROM sessions s
LEFT JOIN events e ON e.session_id = s.id
GROUP BY s.id
ORDER BY s.id DESC
LIMIT ?`, limit)
	if err != nil {
		return err
	}
	defer rows.Close()
	fmt.Fprintf(output, "%-6s %-10s %-20s %-8s %6s\n", "ID", "Command", "Start", "Status", "Events")
	for rows.Next() {
		var info SessionInfo
		if err := rows.Scan(&info.ID, &info.Command, &info.StartTime, &info.EndTime, &info.Status, &info.Error, &info.EventCount); err != nil {
			return err
		}
		fmt.Fprintf(output, "%-6d %-10s %-20s %-8s %6d\n", info.ID, info.Command, info.StartTime, info.Status, info.EventCount)
	}
	return rows.Err()
}

// Transcript renders a session as readable text: the session header,
// followed by each event in chronological order. Events recorded before
// the first attempt (attempt 0: system prompt and initial contents) are
// rendered as session context. Used for display and as the input to the
// analysis pass. See TheoryOfInteractionRecording.
func Transcript(recorder *Recorder, sessionID int64) (string, error) {
	if recorder == nil || recorder.db == nil {
		return "", fmt.Errorf("interaction database not available")
	}
	var command, startTime, endTime, status, errMsg string
	err := recorder.db.QueryRow(
		`SELECT command, start_time, COALESCE(end_time, ''), status, COALESCE(error, '') FROM sessions WHERE id = ?`,
		sessionID,
	).Scan(&command, &startTime, &endTime, &status, &errMsg)
	if err != nil {
		return "", err
	}
	rows, err := recorder.db.Query(
		`SELECT round, time, type, detail FROM events WHERE session_id = ? ORDER BY id`,
		sessionID,
	)
	if err != nil {
		return "", err
	}
	defer rows.Close()

	var b strings.Builder
	fmt.Fprintf(&b, "=== Session %d: %s ===\n", sessionID, command)
	fmt.Fprintf(&b, "start: %s\n", startTime)
	if endTime != "" {
		fmt.Fprintf(&b, "end: %s\n", endTime)
	}
	fmt.Fprintf(&b, "status: %s\n", status)
	if errMsg != "" {
		fmt.Fprintf(&b, "error: %s\n", errMsg)
	}

	for rows.Next() {
		var attempt int
		var t, typ, detail string
		if err := rows.Scan(&attempt, &t, &typ, &detail); err != nil {
			return "", err
		}
		if attempt > 0 {
			fmt.Fprintf(&b, "\n--- attempt %d [%s] %s ---\n", attempt, typ, t)
		} else {
			fmt.Fprintf(&b, "\n--- context [%s] %s ---\n", typ, t)
		}
		if detail != "" {
			fmt.Fprintf(&b, "%s\n", detail)
		}
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	return b.String(), nil
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

// latestSessionID returns the id of the most recent session, or 0 when no
// session has been recorded.
func latestSessionID(recorder *Recorder) (int64, error) {
	if recorder == nil || recorder.db == nil {
		return 0, fmt.Errorf("interaction database not available")
	}
	var id int64
	err := recorder.db.QueryRow(`SELECT id FROM sessions ORDER BY id DESC LIMIT 1`).Scan(&id)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	return id, err
}

// ShowSession writes the transcript of a session to output. The recorder
// is bound from the dscope scope, so callers pass only the runtime values
// (the session id and the output writer). See
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
