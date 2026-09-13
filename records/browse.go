package records

import (
	"fmt"

	"github.com/reusee/tai/tree"
)

const TheoryOfSessionBrowsing = `
Session browsing theory:
- The recorded sessions and their trees are read through two dscope
  functions, so a browser consumes them without a text rendering in
  between: ListSessionInfos returns the session list as data — every
  session, most recent first — and LoadSessionTree reconstructs one
  session's tree from its recorded operation stream with tree.Replay,
  the same tree a live session's Tree tab renders.
- The command-line listing keeps its own rendering; it renders the
  same data the browser reads, so the two views cannot disagree.
- A session that recorded no operations has no tree: LoadSessionTree
  reports it as an error instead of returning an empty tree, because
  an empty tree would hide the reason from the caller.
`

// sessionInfos reads the recorded sessions, most recent first; a
// non-positive limit reads every session. See TheoryOfSessionBrowsing.
func sessionInfos(recorder *Recorder, limit int) ([]SessionInfo, error) {
	if recorder == nil || recorder.db == nil {
		return nil, fmt.Errorf("session database not available")
	}
	if limit <= 0 {
		limit = -1
	}
	rows, err := recorder.db.Query(`
SELECT s.id, s.command, s.command_line, s.start_time, COALESCE(s.end_time, ''), s.status, COALESCE(s.error, ''), COUNT(o.id)
FROM sessions s
LEFT JOIN tree_ops o ON o.session_id = s.id
GROUP BY s.id
ORDER BY s.id DESC
LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var infos []SessionInfo
	for rows.Next() {
		var info SessionInfo
		if err := rows.Scan(
			&info.ID, &info.Command, &info.CommandLine, &info.StartTime,
			&info.EndTime, &info.Status, &info.Error, &info.OpCount,
		); err != nil {
			return nil, err
		}
		infos = append(infos, info)
	}
	return infos, rows.Err()
}

// ListSessionInfos returns the recorded sessions as data, most recent
// first. The recorder is bound from the dscope scope, so callers pass no
// arguments. See TheoryOfSessionBrowsing.
type ListSessionInfos func() ([]SessionInfo, error)

func (Module) ListSessionInfos(recorder *Recorder) ListSessionInfos {
	return func() ([]SessionInfo, error) {
		return sessionInfos(recorder, 0)
	}
}

// LoadSessionTree reconstructs a recorded session's tree from its
// operation stream, so a browser renders the same tree the live Tree tab
// renders. The recorder is bound from the dscope scope.
// See TheoryOfSessionBrowsing and tree.TheoryOfOperationLog.
type LoadSessionTree func(sessionID int64) (*tree.Tree, error)

func (Module) LoadSessionTree(recorder *Recorder) LoadSessionTree {
	return func(sessionID int64) (*tree.Tree, error) {
		if recorder == nil || recorder.db == nil {
			return nil, fmt.Errorf("session database not available")
		}
		ops, err := loadOps(recorder, sessionID)
		if err != nil {
			return nil, err
		}
		if len(ops) == 0 {
			return nil, fmt.Errorf("session %d has no recorded operations", sessionID)
		}
		return tree.Replay(ops)
	}
}
