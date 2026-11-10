package records

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/reusee/tai/tree"
)

// TheoryOfRecordQuery documents the query model over the recorded sessions:
// its filters, its replay rule, and its result shape. See
// TheoryOfGlobalRecordSearch for the keyword scope the query shares.
const TheoryOfRecordQuery = `
Record query theory:
- A query selects nodes from the recorded sessions; it is not a second
  search mechanism. The session id selects one record, the keyword selects
  the nodes whose content or metadata carries it, and the type, author, and
  name lists select nodes by structured identity. Every filter is
  conjunctive, and an omitted filter matches everything.
- The database narrows the candidate sessions with the shared keyword
  predicate over the operation stream, and tree.Replay rebuilds each
  candidate's applied history before the node filter decides: a node
  written and later deleted does not match, and a rewritten node matches
  its final content. The scope and the ordering — every recorded session,
  most recent first — are TheoryOfGlobalRecordSearch's.
- A type filter matches four ways: the full structured type
  (event::run-error), its name segment (run-error), its prefix (event,
  block, message), and the single-colon spelling (event:run-error). One
  filter therefore selects one kind, a family of kinds, or a named kind,
  whatever form the caller wrote. Author and name filters are exact,
  case-insensitive comparisons.
- The result carries the session metadata once per matched session and one
  record per node: name, structured type, author, parent, insert time, and
  the content when the query asks for it. Content is opt-in because one
  node can hold a whole file; the caller bounds the node count with the
  limit, and the model-facing renderer bounds what it prints.
- QueryRecord is a dscope-bound function type: the recorder is captured at
  provider resolution, so callers pass only the query (see
  pipeline.TheoryOfDscopeBoundFunctions).
- The query-record block kind delivers the same data inside a generation.
  QueryRecordBlockPrompt lists the parameters ParseRecordQuery accepts; the
  prompt and the parser are maintained together.
`

// QueryRecordBlockPrompt teaches the query-record block kind: its header
// parameters, the next-round arrival of the results, and the summary-first
// stop discipline every stop-and-wait kind follows. The parameters are the
// keys ParseRecordQuery accepts. See TheoryOfRecordQuery and
// blocks.TheoryOfSummaryBlocks.
const QueryRecordBlockPrompt = `
Query-Record Block Kind:

Use the "query-record" kind to inspect the recorded interaction sessions. The opening header carries the query parameters as key=value pairs joined by &; the block body is empty. The parameters combine conjunctively, and an omitted parameter matches everything.

**Parameters:**
- session (alias record): one recorded session selected by its record id. Omitted, the query spans every recorded session, most recent first.
- keyword: keep the nodes whose content or metadata — name, type, author — contains the keyword, case-insensitively. Percent-encode every value character outside A-Z a-z 0-9 - . _ ~ (a space is %20).
- types: keep the nodes of the listed node types, comma-separated. An entry is a full structured type (event::run-error), a type name segment (run-error), a type prefix (event, block, message), or the single-colon spelling (event:run-error).
- authors: keep the nodes written by the listed authors, comma-separated: user, model, program.
- names: keep the nodes with the listed names, comma-separated. A name matches exactly.
- limit: cap the number of returned nodes. Omitted, every match is returned.
- content: include each returned node's content when its value is true or empty.

**Rules:**
- The query reads the recorded sessions. Use it to learn what earlier sessions did — the errors they hit, the summaries they wrote, the changes they applied — and to read one session's details by its record id.
- Keep the result small: name the session, the node types, and a limit whenever you can.
- Query results are NOT available in the current response: they arrive as user content at the start of the NEXT round.
- Do NOT emit change blocks whose content depends on the query result: the results have not arrived yet.
- After the last query-record block's closing line, emit the summary block IMMEDIATELY, then end the response and wait for the results.
- Never end a response on a query-record block, and never stop at its closing line: stopping there omits the mandatory summary block, the response is treated as incomplete, and it is discarded and retried — its blocks are discarded, so the query never runs unless it is re-emitted with the summary.
- The query-record block is NOT a completion signal. MUST still emit a summary block in the same round, after the query-record block. Every round must end with a summary block.
`

// QueryRecord answers one query over the recorded sessions, most recent
// session first. The recorder is bound from the dscope scope, so callers pass
// only the query. See TheoryOfRecordQuery.
type QueryRecord func(query RecordQuery) (*RecordQueryResult, error)

// RecordQuery selects recorded nodes. Every field is optional: an omitted
// filter matches every node. See TheoryOfRecordQuery.
type RecordQuery struct {
	// SessionID selects one recorded session by its record id; zero
	// selects every recorded session.
	SessionID int64
	// Keyword selects the nodes whose content or metadata — name, type,
	// author — contains it, case-insensitively.
	Keyword string
	// Types selects the nodes whose structured type matches one of the
	// entries as a full type, a type name segment, a type prefix, or the
	// single-colon spelling of prefix and name.
	Types []string
	// Authors selects the nodes written by one of the authors.
	Authors []string
	// Names selects the nodes whose name equals one of the entries.
	Names []string
	// Limit caps the returned node count; zero returns every match.
	Limit int
	// Content includes each returned node's content.
	Content bool
}

// RecordQueryNode is one node a query returned: its session, its structured
// identity, its insert time, and its content when the query asked for it.
// See TheoryOfRecordQuery.
type RecordQueryNode struct {
	SessionID int64
	Command   string
	StartTime string
	Name      string
	Type      tree.Type
	Author    tree.Author
	Parent    string
	Time      time.Time
	Content   string
}

// RecordQueryResult is one query's outcome: the matched sessions, the matched
// nodes in session order and tree order, and whether the limit stopped the
// query. See TheoryOfRecordQuery.
type RecordQueryResult struct {
	Query     RecordQuery
	Sessions  []SessionInfo
	Nodes     []RecordQueryNode
	Truncated bool
}

// ParseRecordQuery interprets a query-record block's header parameters. The
// accepted keys are session (alias record), keyword, types, authors, names,
// limit, and content; the session-tree parameter parent is ignored because
// the pipeline owns it. Parameter names are matched case-insensitively, and
// keys are processed in sorted order, so a batch of invalid parameters reports
// the same first error every time. An unknown key is an error naming the
// accepted keys, so a model that guessed a parameter name can correct it.
// See TheoryOfRecordQuery.
func ParseRecordQuery(attrs map[string]string) (RecordQuery, error) {
	var query RecordQuery
	keys := make([]string, 0, len(attrs))
	for key := range attrs {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		value := attrs[key]
		switch strings.ToLower(key) {
		case "parent":
			// The session tree owns this parameter; the query does not.
		case "session", "record":
			id, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
			if err != nil || id <= 0 {
				return RecordQuery{}, fmt.Errorf("session %q is not a positive record id", value)
			}
			query.SessionID = id
		case "keyword":
			query.Keyword = strings.TrimSpace(value)
		case "types":
			query.Types = splitRecordQueryList(value)
		case "authors":
			query.Authors = splitRecordQueryList(value)
		case "names":
			query.Names = splitRecordQueryList(value)
		case "limit":
			limit, err := strconv.Atoi(strings.TrimSpace(value))
			if err != nil || limit < 0 {
				return RecordQuery{}, fmt.Errorf("limit %q is not a non-negative integer", value)
			}
			query.Limit = limit
		case "content":
			if strings.TrimSpace(value) == "" {
				query.Content = true
				continue
			}
			content, err := strconv.ParseBool(strings.TrimSpace(value))
			if err != nil {
				return RecordQuery{}, fmt.Errorf("content %q is not a boolean", value)
			}
			query.Content = content
		default:
			return RecordQuery{}, fmt.Errorf(
				"unknown parameter %q; accepted parameters are session, keyword, types, authors, names, limit, and content",
				key,
			)
		}
	}
	return query, nil
}

// splitRecordQueryList splits a comma-separated filter list, trimming each
// entry and dropping the empty ones, so a list written with spaces parses.
// See TheoryOfRecordQuery.
func splitRecordQueryList(value string) []string {
	parts := strings.Split(value, ",")
	var out []string
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

// queryRecord is the package-level implementation behind the QueryRecord
// dscope function type, so a test calls it directly. See TheoryOfRecordQuery.
func queryRecord(recorder *Recorder, query RecordQuery) (*RecordQueryResult, error) {
	if recorder == nil || recorder.db == nil {
		return nil, fmt.Errorf("session database not available")
	}
	result := &RecordQueryResult{Query: query}

	infos, err := sessionInfos(recorder, 0)
	if err != nil {
		return nil, err
	}
	if query.SessionID > 0 {
		found := false
		for _, info := range infos {
			if info.ID == query.SessionID {
				infos = []SessionInfo{info}
				found = true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("session %d not found", query.SessionID)
		}
	}
	if query.Keyword != "" {
		ids, err := keywordCandidateSessionIDs(recorder, query.Keyword)
		if err != nil {
			return nil, err
		}
		keep := make(map[int64]bool, len(ids))
		for _, id := range ids {
			keep[id] = true
		}
		narrowed := infos[:0]
		for _, info := range infos {
			if keep[info.ID] {
				narrowed = append(narrowed, info)
			}
		}
		infos = narrowed
	}

	folded := strings.ToLower(query.Keyword)
loop:
	for _, info := range infos {
		ops, err := loadOps(recorder, info.ID)
		if err != nil {
			continue
		}
		tr, err := tree.Replay(ops)
		if err != nil {
			continue
		}
		sessionRecorded := false
		for _, n := range tr.Filter(func(n *tree.Node) bool { return n.Type != tree.TypeRoot }) {
			if !recordQueryMatches(query, n, folded) {
				continue
			}
			if query.Limit > 0 && len(result.Nodes) == query.Limit {
				result.Truncated = true
				break loop
			}
			if !sessionRecorded {
				result.Sessions = append(result.Sessions, info)
				sessionRecorded = true
			}
			node := RecordQueryNode{
				SessionID: info.ID,
				Command:   info.Command,
				StartTime: info.StartTime,
				Name:      n.Name,
				Type:      n.Type,
				Author:    n.Author,
				Parent:    n.Parent,
				Time:      n.InsertTime,
			}
			if query.Content {
				node.Content = n.Content
			}
			result.Nodes = append(result.Nodes, node)
		}
	}
	return result, nil
}

// keywordCandidateSessionIDs returns the ids of the sessions whose operation
// stream carries the keyword: the SQL narrowing every keyword query shares, so
// the replay runs only on the sessions that can hold a match.
// See TheoryOfRecordQuery.
func keywordCandidateSessionIDs(recorder *Recorder, keyword string) ([]int64, error) {
	pattern := "%" + escapeLikePattern(strings.ToLower(keyword)) + "%"
	rows, err := recorder.db.Query(`
SELECT DISTINCT o.session_id
FROM tree_ops o
WHERE `+keywordFilterSQL+`
ORDER BY o.session_id DESC`, pattern, pattern, pattern, pattern)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// recordQueryMatches reports whether one replayed node satisfies every filter
// of the query. folded is the lowercased keyword, folded once per query.
// See TheoryOfRecordQuery.
func recordQueryMatches(query RecordQuery, n *tree.Node, folded string) bool {
	if len(query.Types) > 0 && !matchRecordQueryType(n.Type, query.Types) {
		return false
	}
	if len(query.Authors) > 0 && !matchRecordQueryValue(string(n.Author), query.Authors) {
		return false
	}
	if len(query.Names) > 0 && !matchRecordQueryValue(n.Name, query.Names) {
		return false
	}
	if folded != "" {
		if _, _, ok := recordNodeMatch(n, folded); !ok {
			return false
		}
	}
	return true
}

// matchRecordQueryType reports whether one structured type matches one of the
// filter entries: the full type, its name segment, its prefix, or the
// single-colon spelling of prefix and name. The comparison is
// case-insensitive. See TheoryOfRecordQuery.
func matchRecordQueryType(typ tree.Type, want []string) bool {
	full := strings.ToLower(string(typ))
	name := strings.ToLower(typ.Name())
	prefix := strings.ToLower(typ.Prefix())
	single := prefix + ":" + name
	for _, entry := range want {
		entry = strings.ToLower(strings.TrimSpace(entry))
		if entry == "" {
			continue
		}
		if entry == full || entry == name || entry == prefix || entry == single {
			return true
		}
	}
	return false
}

// matchRecordQueryValue reports whether one node field equals one of the
// filter entries, case-insensitively. See TheoryOfRecordQuery.
func matchRecordQueryValue(value string, want []string) bool {
	value = strings.ToLower(value)
	for _, entry := range want {
		if value == strings.ToLower(strings.TrimSpace(entry)) {
			return true
		}
	}
	return false
}

// QueryRecord provider: the query answers from the session database the
// Recorder opened. See TheoryOfRecordQuery.
func (Module) QueryRecord(recorder *Recorder) QueryRecord {
	return func(query RecordQuery) (*RecordQueryResult, error) {
		return queryRecord(recorder, query)
	}
}
