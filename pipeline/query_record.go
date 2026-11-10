package pipeline

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/reusee/tai/blocks"
	"github.com/reusee/tai/components"
	"github.com/reusee/tai/generators"
	"github.com/reusee/tai/nets"
	"github.com/reusee/tai/records"
)

const (
	// maxRecordQueryRenderedNodes bounds the nodes one query result
	// prints, so a broad query cannot flood the context; a leading note
	// names the cut. See records.TheoryOfRecordQuery.
	maxRecordQueryRenderedNodes = 200
	// maxRecordQueryContentBytes bounds one node's printed content: a
	// recorded node can hold a whole file, and a trailing note names the
	// bytes left out. See records.TheoryOfRecordQuery.
	maxRecordQueryContentBytes = 4000
)

// NewQueryRecordComponent returns the query-record block component: the model
// queries the recorded interaction sessions through header parameters, the
// scope's records.QueryRecord answers the query, and the result is fed back as
// user content for the next generation. The component is read-only context
// fetching, so it declares a per-block Compute and the generation loop
// prefetches the query at parse time; its Process consumes the prefetched
// outcomes in block order. A parameter fault or a query fault is fed back as a
// text part, so the model can correct the block in the next round. See
// records.TheoryOfRecordQuery and components.TheoryOfReadOnlyPrefetch.
func NewQueryRecordComponent(queryRecord records.QueryRecord) components.Component {
	computeOne := func(
		ctx context.Context,
		block blocks.Block,
		root *os.Root,
		httpClient nets.HTTPClient,
	) ([]generators.Part, error) {
		return computeRecordQueryBlock(ctx, block, queryRecord)
	}
	return components.Component{
		Kind:          "query-record",
		PromptSection: records.QueryRecordBlockPrompt,
		Compute:       computeOne,
		Process: func(ctx context.Context, pctx *components.ProcessContext) components.ProcessResult {
			parts, err := components.ConsumePrefetchedBlockParts(ctx, pctx,
				func(ctx context.Context, block blocks.Block) ([]generators.Part, error) {
					return computeRecordQueryBlock(ctx, block, queryRecord)
				})
			return components.ProcessResult{Parts: parts, Err: err}
		},
	}
}

// computeRecordQueryBlock computes the user-content parts of one query-record
// block without side effects: the header parameters become a
// records.RecordQuery, the scope's query answers it, and the result is rendered
// for the model. A parameter or query fault yields a text part rather than an
// error, so the model corrects the block in the next round. The per-block shape
// is what makes the query prefetchable at parse time. See
// records.TheoryOfRecordQuery and components.TheoryOfReadOnlyPrefetch.
func computeRecordQueryBlock(
	ctx context.Context,
	block blocks.Block,
	queryRecord records.QueryRecord,
) ([]generators.Part, error) {
	query, err := records.ParseRecordQuery(block.Attributes)
	if err != nil {
		return []generators.Part{generators.Text(fmt.Sprintf(
			"[query-record: %v]\n\n", err))}, nil
	}
	result, err := queryRecord(query)
	if err != nil {
		return []generators.Part{generators.Text(fmt.Sprintf(
			"[query-record: %v]\n\n", err))}, nil
	}
	return []generators.Part{generators.Text(renderRecordQueryResult(result))}, nil
}

// renderRecordQueryResult renders one query result for the model: a header
// with the session and node counts, one session header per matched session,
// and one line per node — name, structured type, author, and insert time —
// with the node's content indented below when the query asked for it.
// Rendering is bounded in both directions, so one broad query cannot flood the
// context. See records.TheoryOfRecordQuery.
func renderRecordQueryResult(result *records.RecordQueryResult) string {
	var b strings.Builder
	fmt.Fprintf(&b, "[record query result: %d session(s), %d node(s)]\n",
		len(result.Sessions), len(result.Nodes))
	if result.Truncated {
		b.WriteString("the limit stopped the query; raise the limit or narrow the query\n")
	}
	nodes := result.Nodes
	if len(nodes) > maxRecordQueryRenderedNodes {
		fmt.Fprintf(&b, "rendering the first %d nodes of %d; narrow the query\n",
			maxRecordQueryRenderedNodes, len(nodes))
		nodes = nodes[:maxRecordQueryRenderedNodes]
	}
	if len(nodes) == 0 {
		b.WriteString("no recorded node matched the query; check the parameters and the record id\n\n")
		return b.String()
	}
	header := make(map[int64]bool, len(result.Sessions))
	for _, n := range nodes {
		if !header[n.SessionID] {
			header[n.SessionID] = true
			fmt.Fprintf(&b, "session %d: command=%s start=%s\n",
				n.SessionID, n.Command, n.StartTime)
		}
		fmt.Fprintf(&b, "- %s %s %s %s\n", n.Name, n.Type, n.Author, n.Time.Format(time.RFC3339))
		if n.Content == "" {
			continue
		}
		for _, line := range recordQueryContentLines(n.Content) {
			fmt.Fprintf(&b, "    %s\n", line)
		}
	}
	b.WriteString("\n")
	return b.String()
}

// truncateRecordQueryContent cuts one node's printed content at
// maxRecordQueryContentBytes on a rune boundary and notes the bytes left out.
// See records.TheoryOfRecordQuery.
func truncateRecordQueryContent(content string) string {
	if len(content) <= maxRecordQueryContentBytes {
		return content
	}
	cut := maxRecordQueryContentBytes
	for cut > 0 && !utf8.RuneStart(content[cut]) {
		cut--
	}
	return content[:cut] + fmt.Sprintf("\n[... %d more bytes]", len(content)-cut)
}

// recordQueryContentLines returns one printed node's content lines: the
// content cut at the print bound, with leading blank lines dropped so a prompt
// constant's opening newline does not hide the text.
// See records.TheoryOfRecordQuery.
func recordQueryContentLines(content string) []string {
	lines := strings.Split(strings.TrimRight(truncateRecordQueryContent(content), "\n"), "\n")
	i := 0
	for i < len(lines) && strings.TrimSpace(lines[i]) == "" {
		i++
	}
	return lines[i:]
}
