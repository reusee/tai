package records

import (
	"strings"

	"github.com/reusee/tai/tree"
)

// chatInputFence opens the fence the ai command wraps the typed chat in
// inside the session's initial user node (cmd/tai/ai.go). See
// TheoryOfSessionBrowsing.
const chatInputFence = "``` begin of user input"

const (
	// sessionChatPreviewInputs bounds the inputs one preview shows: a
	// record list row is one line, so a session with many turns lists its
	// first inputs and an ellipsis states the rest.
	sessionChatPreviewInputs = 4
	// sessionChatPreviewRunes bounds one input's contribution, so a long
	// pasted input never crowds the browsing signal of the other records.
	sessionChatPreviewRunes = 40
)

// attachSessionChats fills the sessions' chat previews from their recorded
// user inputs: the user-authored message::user nodes of the same session
// page the list carries. See TheoryOfSessionBrowsing.
func attachSessionChats(recorder *Recorder, infos []SessionInfo, limit int) error {
	if len(infos) == 0 {
		return nil
	}
	rows, err := recorder.db.Query(`
SELECT o.session_id, o.content
FROM tree_ops o
JOIN (SELECT id FROM sessions ORDER BY id DESC LIMIT ?) s ON s.id = o.session_id
WHERE o.type = ? AND o.author = ?
ORDER BY o.session_id, o.seq`,
		limit, string(tree.TypeUser), string(tree.AuthorUser),
	)
	if err != nil {
		return err
	}
	defer rows.Close()
	contents := make(map[int64][]string, len(infos))
	for rows.Next() {
		var id int64
		var content string
		if err := rows.Scan(&id, &content); err != nil {
			return err
		}
		contents[id] = append(contents[id], content)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for i := range infos {
		infos[i].Chat = sessionChatPreview(contents[infos[i].ID])
	}
	return nil
}

// sessionChatPreview renders a session's user inputs as one list line: each
// input's own line, bounded, in order, with an ellipsis for the inputs and
// the text the preview leaves out. See TheoryOfSessionBrowsing.
func sessionChatPreview(contents []string) string {
	lines := make([]string, 0, len(contents))
	for _, content := range contents {
		if line := chatInputLine(content); line != "" {
			lines = append(lines, line)
		}
	}
	more := len(lines) > sessionChatPreviewInputs
	if more {
		lines = lines[:sessionChatPreviewInputs]
	}
	parts := make([]string, len(lines))
	for i, line := range lines {
		parts[i] = truncateRunes(line, sessionChatPreviewRunes)
	}
	preview := strings.Join(parts, " | ")
	if more {
		preview += " | …"
	}
	return preview
}

// chatInputLine extracts the user's own words from one recorded user input:
// the fenced chat the ai command writes, or the input's first non-blank
// line. See TheoryOfSessionBrowsing.
func chatInputLine(content string) string {
	lines := recordContentLines(content)
	for i, line := range lines {
		if strings.TrimSpace(line) != chatInputFence || i+1 >= len(lines) {
			continue
		}
		if text := strings.TrimSpace(lines[i+1]); text != "" {
			return text
		}
	}
	for _, line := range lines {
		if text := strings.TrimSpace(line); text != "" {
			return text
		}
	}
	return ""
}

// truncateRunes cuts s to at most n runes, marking the cut with an
// ellipsis.
func truncateRunes(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n]) + "…"
}
