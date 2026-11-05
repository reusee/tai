package records

import (
	"fmt"
	"strings"

	"github.com/reusee/tai/tree"
)

const TheoryOfGlobalRecordSearch = `
Global record search theory:
- The search spans every recorded session, not the opened record: the
  user remembers a phrase, not the session that wrote it, so a search
  limited to one record would force a walk through every record before
  the phrase turns up. The keyword is matched against a node's content
  and its metadata — name, type, author — the fields the Tree tab's own
  search reads (cmd/tai.TheoryOfTreeSearch), with case folding this
  search adds because a hunted phrase is not remembered by its
  capitalization.
- The match unit is the node, not the substring: a list entry names one
  node of one session and jumps there. A node with five matching lines
  lists once; the entry's preview carries the first matching content
  line, and a metadata-only match previews the node's first content
  line.
- The database narrows the sessions and the replay decides. A LIKE over
  the operation stream says only which sessions ever wrote the text, so
  it selects the candidates — one query instead of a replay of every
  record — while tree.Replay rebuilds each candidate's applied history
  and the walk reports what the session holds now: a node written and
  later deleted matches the query and not the search, and a node whose
  content changed matches the content it ended with.
- A session whose operations cannot be replayed is skipped: one corrupt
  record must not hide every other match. Matches are ordered most
  recent session first and, within a session, in tree order, so the
  newest record reads at the top of the list.
- An empty keyword searches nothing: the answer is an empty list, not
  every node of every session.
`

// RecordSearchMatch is one recorded node the global record search matched:
// the session that wrote it, the node's identity, and where the keyword was
// found. ContentLine is the 0-based matched content line, or -1 for a
// metadata-only match; Preview carries the matched line, or the node's
// first content line for a metadata-only match.
// See TheoryOfGlobalRecordSearch.
type RecordSearchMatch struct {
	SessionID   int64
	Command     string
	StartTime   string
	NodeName    string
	NodeType    tree.Type
	NodeAuthor  tree.Author
	ContentLine int
	Preview     string
}

// SearchRecordNodes searches every recorded session's tree for nodes whose
// content or metadata contains the keyword, most recent session first.
// The recorder is bound from the dscope scope, so callers pass only the
// keyword. See TheoryOfGlobalRecordSearch.
type SearchRecordNodes func(keyword string) ([]RecordSearchMatch, error)

func (Module) SearchRecordNodes(recorder *Recorder) SearchRecordNodes {
	return func(keyword string) ([]RecordSearchMatch, error) {
		return searchRecordNodes(recorder, keyword)
	}
}

// searchRecordNodes is the package-level implementation behind the
// SearchRecordNodes dscope function type, so a test calls it directly.
// See TheoryOfGlobalRecordSearch.
func searchRecordNodes(recorder *Recorder, keyword string) ([]RecordSearchMatch, error) {
	if recorder == nil || recorder.db == nil {
		return nil, fmt.Errorf("session database not available")
	}
	keyword = strings.TrimSpace(keyword)
	if keyword == "" {
		return nil, nil
	}
	pattern := "%" + escapeLikePattern(strings.ToLower(keyword)) + "%"
	rows, err := recorder.db.Query(`
SELECT s.id, s.command, s.start_time
FROM sessions s
WHERE EXISTS (
    SELECT 1 FROM tree_ops o
    WHERE o.session_id = s.id
      AND (LOWER(o.name) LIKE ? ESCAPE '\'
        OR LOWER(o.type) LIKE ? ESCAPE '\'
        OR LOWER(o.author) LIKE ? ESCAPE '\'
        OR LOWER(o.content) LIKE ? ESCAPE '\')
)
ORDER BY s.id DESC`, pattern, pattern, pattern, pattern)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	type candidate struct {
		id      int64
		command string
		start   string
	}
	var candidates []candidate
	for rows.Next() {
		var c candidate
		if err := rows.Scan(&c.id, &c.command, &c.start); err != nil {
			return nil, err
		}
		candidates = append(candidates, c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	folded := strings.ToLower(keyword)
	var matches []RecordSearchMatch
	for _, c := range candidates {
		ops, err := loadOps(recorder, c.id)
		if err != nil {
			continue
		}
		tr, err := tree.Replay(ops)
		if err != nil {
			continue
		}
		for _, n := range tr.Filter(func(n *tree.Node) bool { return n.Type != tree.TypeRoot }) {
			line, preview, ok := recordNodeMatch(n, folded)
			if !ok {
				continue
			}
			matches = append(matches, RecordSearchMatch{
				SessionID:   c.id,
				Command:     c.command,
				StartTime:   c.start,
				NodeName:    n.Name,
				NodeType:    n.Type,
				NodeAuthor:  n.Author,
				ContentLine: line,
				Preview:     preview,
			})
		}
	}
	return matches, nil
}

// escapeLikePattern escapes the LIKE metacharacters of one keyword, so a
// "%" or "_" the user typed matches itself instead of every node.
func escapeLikePattern(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r == '\\' || r == '%' || r == '_' {
			b.WriteRune('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}

// recordNodeMatch reports whether one recorded node matches the folded
// keyword: its content line by line first, then its metadata. line is the
// 0-based matched content line, or -1 for a metadata-only match; preview is
// the matched line, or the node's first content line.
// See TheoryOfGlobalRecordSearch.
func recordNodeMatch(n *tree.Node, foldedKeyword string) (line int, preview string, ok bool) {
	lines := recordContentLines(n.Content)
	for i, text := range lines {
		if strings.Contains(strings.ToLower(text), foldedKeyword) {
			return i, text, true
		}
	}
	for _, field := range []string{n.Name, string(n.Type), string(n.Author)} {
		if !strings.Contains(strings.ToLower(field), foldedKeyword) {
			continue
		}
		if len(lines) > 0 {
			return -1, lines[0], true
		}
		return -1, "", true
	}
	return 0, "", false
}

// recordContentLines returns a node's content as lines with leading blank
// lines dropped, the normalization the Tree tab's preview applies
// (cmd/tai.treeContentLines): a prompt constant often begins with a newline
// after the Go raw-string backtick, so a blank first line must not hide the
// content from the preview.
func recordContentLines(content string) []string {
	lines := strings.Split(strings.TrimRight(content, "\n"), "\n")
	i := 0
	for i < len(lines) && strings.TrimSpace(lines[i]) == "" {
		i++
	}
	return lines[i:]
}
