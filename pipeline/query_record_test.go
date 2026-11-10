package pipeline

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/reusee/dscope"
	"github.com/reusee/tai/blocks"
	"github.com/reusee/tai/components"
	"github.com/reusee/tai/generators"
	"github.com/reusee/tai/modes"
	"github.com/reusee/tai/pipeline/codetypes"
	"github.com/reusee/tai/records"
	"github.com/reusee/tai/tree"
)

// TestQueryRecordComponent verifies the query-record component: the header
// parameters become a records.RecordQuery, the scope's query answers it, and
// the rendered result names the matched session and node; a parameter fault is
// fed back as text, so the model can correct the block without a failed round.
// See records.TheoryOfRecordQuery.
func TestQueryRecordComponent(t *testing.T) {
	var got records.RecordQuery
	comps := dscope.New(
		modes.ForTest(t),
		new(Module),
	).Fork(
		func() codetypes.PartsProvider { return mockPartsProvider{} },
		func() records.QueryRecord {
			return func(query records.RecordQuery) (*records.RecordQueryResult, error) {
				got = query
				return &records.RecordQueryResult{
					Query:    query,
					Sessions: []records.SessionInfo{{ID: 3, Command: "go_module"}},
					Nodes: []records.RecordQueryNode{{
						SessionID: 3,
						Command:   "go_module",
						Name:      "run-error-1",
						Type:      tree.TypeRunError,
						Author:    tree.AuthorProgram,
						Time:      mustParseRecordTime(t, "2026-01-01T00:00:00Z"),
						Content:   "the build failed",
					}},
				}, nil
			}
		},
	).Get[CodesComponents]()

	comp := queryRecordComponent(t, comps)
	result := comp.Process(context.Background(), &components.ProcessContext{
		Blocks: []blocks.Block{{
			Kind:       "query-record",
			Attributes: map[string]string{"session": "3", "types": "event", "content": "true"},
		}},
	})
	if result.Err != nil {
		t.Fatal(result.Err)
	}
	if got.SessionID != 3 || len(got.Types) != 1 || !got.Content {
		t.Fatalf("the header did not reach the query: %+v", got)
	}
	if len(result.Parts) != 1 {
		t.Fatalf("expected one rendered part, got %d", len(result.Parts))
	}
	text := string(result.Parts[0].(generators.Text))
	for _, want := range []string{"session 3", "run-error-1", string(tree.TypeRunError), "the build failed"} {
		if !strings.Contains(text, want) {
			t.Fatalf("rendered result missing %q:\n%s", want, text)
		}
	}

	// An unknown parameter is fed back, and the scope query never runs.
	got = records.RecordQuery{}
	result = comp.Process(context.Background(), &components.ProcessContext{
		Blocks: []blocks.Block{{
			Kind:       "query-record",
			Attributes: map[string]string{"type": "event"},
		}},
	})
	if result.Err != nil {
		t.Fatal(result.Err)
	}
	text = string(result.Parts[0].(generators.Text))
	if !strings.Contains(text, "unknown parameter") || !strings.Contains(text, "keyword") {
		t.Fatalf("expected the parameter fault with the accepted keys, got %q", text)
	}
	if got.SessionID != 0 || len(got.Types) != 0 {
		t.Fatalf("a rejected query must not reach the scope query: %+v", got)
	}
}

// TestSystemPromptQueryRecordBlock verifies that the query-record kind is
// taught in the codes session and follows the stop-and-wait discipline.
// See records.TheoryOfRecordQuery and blocks.TheoryOfSummaryBlocks.
func TestSystemPromptQueryRecordBlock(t *testing.T) {
	prompt := dscope.New(
		modes.ForTest(t),
		new(Module),
	).Fork(
		func() codetypes.PartsProvider { return mockPartsProvider{} },
	).Get[SystemPrompt]()
	s := string(prompt)
	if !strings.Contains(s, "Query-Record Block Kind") {
		t.Fatal("system prompt must include the query-record block section")
	}
	if !strings.Contains(s, "query-record block is NOT a completion signal") {
		t.Fatal("system prompt must state that a query-record block is not a completion signal")
	}
}

func queryRecordComponent(t *testing.T, comps CodesComponents) components.Component {
	t.Helper()
	for _, comp := range comps.Processable() {
		if comp.Kind == "query-record" {
			return comp
		}
	}
	t.Fatal("query-record component not found")
	return components.Component{}
}

func mustParseRecordTime(t *testing.T, text string) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, text)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}
