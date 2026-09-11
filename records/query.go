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
// one event block per applied operation in application order, each
// carrying its metadata in the opening header's URI query and the node
// content it wrote as the block body. Used for display and as the input
// to the analysis pass. See TheoryOfInteractionRecording.
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
		if err := writeEventBlock(&b, op); err != nil {
			return "", err
		}
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

// eventBlockDelimiters lists the preset delimiters of transcript event
// blocks, in selection order: the first delimiter an event's content
// does not contain is chosen, so a body never collides with its own
// opening or closing marker. The names are uncommon Chinese era names,
// each exactly two Han characters, so the standard block parser accepts
// them. See TheoryOfInteractionRecording.
var eventBlockDelimiters = [...]string{
	"貞觀", "開元", "洪武", "永樂", "弘治", "嘉靖",
	"萬曆", "順治", "康熙", "雍正", "乾隆", "嘉慶",
	"道光", "咸豐", "同治", "宣統",
}

// selectEventDelimiter returns the first preset delimiter the content
// does not contain. When every preset collides, the event cannot be
// rendered unambiguously and an error is returned instead of a corrupt
// block. See TheoryOfInteractionRecording.
func selectEventDelimiter(content string) (string, error) {
	for _, delimiter := range eventBlockDelimiters {
		if !strings.Contains(content, delimiter) {
			return delimiter, nil
		}
	}
	return "", fmt.Errorf("no preset delimiter is absent from the event content")
}

// percentEncodeEventValue encodes one URI query value: every byte
// outside the RFC 3986 unreserved set (A-Z a-z 0-9 - . _ ~) becomes
// %XX, so spaces, newlines, and non-ASCII bytes never break the header.
// See TheoryOfInteractionRecording.
func percentEncodeEventValue(value string) string {
	var b strings.Builder
	for i := 0; i < len(value); i++ {
		c := value[i]
		switch {
		case c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z', c >= '0' && c <= '9',
			c == '-', c == '.', c == '_', c == '~':
			b.WriteByte(c)
		default:
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String()
}

// eventMetadataQuery renders one operation's metadata as the URI query
// of its event block header: key=value pairs joined by '&', in the fixed
// order name, kind, type, author, parent, time, with empty fields
// omitted and every value percent-encoded, so the standard header parser
// decodes them. See TheoryOfInteractionRecording.
func eventMetadataQuery(op tree.Op) string {
	pairs := make([]string, 0, 6)
	add := func(key, value string) {
		if value == "" {
			return
		}
		pairs = append(pairs, key+"="+percentEncodeEventValue(value))
	}
	add("name", op.Name)
	add("kind", string(op.Kind))
	add("type", string(op.Type))
	add("author", string(op.Author))
	add("parent", op.Parent)
	if !op.Time.IsZero() {
		add("time", op.Time.Format(time.RFC3339Nano))
	}
	return strings.Join(pairs, "&")
}

// writeEventBlock renders one recorded operation as a boundary-delimited
// event block: the block kind is "event", the operation's metadata is
// percent-encoded into the opening header's URI query, and the content
// the operation wrote is the block body. The delimiter is the first
// preset the content does not contain, so the body never collides with
// its own closing marker. See TheoryOfInteractionRecording.
func writeEventBlock(b *strings.Builder, op tree.Op) error {
	delimiter, err := selectEventDelimiter(op.Content)
	if err != nil {
		return err
	}
	fmt.Fprintf(b, "<<%s event:?%s\n", delimiter, eventMetadataQuery(op))
	b.WriteString(op.Content)
	if op.Content != "" && !strings.HasSuffix(op.Content, "\n") {
		b.WriteString("\n")
	}
	b.WriteString(delimiter)
	b.WriteString("\n")
	return nil
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
