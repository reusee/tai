package pipeline

import (
	"context"
	"strings"
	"testing"

	"github.com/reusee/dscope"
	"github.com/reusee/tai/generators"
	"github.com/reusee/tai/gotools"
	"github.com/reusee/tai/modes"
	"github.com/reusee/tai/tree"
)

// TestRunDrainsTreeEventSink verifies that Module.Run drains the
// per-scope TreeEventSink at startup and replays each buffered record
// as a context event node under the session root, before the first
// attempt, and that the sink is emptied so records never replay across
// runs. See TheoryOfLoopEvents and gotools.TheoryOfTokenComposition.
func TestRunDrainsTreeEventSink(t *testing.T) {
	scope := dscope.New(modes.ForTest(t), new(Module))
	scope.Call(func(run Run, sink *gotools.TreeEventSink) {
		sink.Record("assembled tokens: focus 100, context 200, total 300")
		var result Result
		for _, err := range run(context.Background(), RunOptions{
			InitialState: generators.NewPrompts("", nil),
			PhaseBuilder: func(_ generators.Generator) generators.Phase {
				return func(_ context.Context, state generators.State) (generators.Phase, generators.State, error) {
					state, err := state.AppendContent(&generators.Content{
						Role: generators.RoleModel,
						Parts: []generators.Part{
							generators.Text("ok\n"),
						},
					})
					if err != nil {
						return nil, nil, err
					}
					return nil, state, nil
				}
			},
		}, &result) {
			if err != nil {
				t.Fatal(err)
			}
		}
		if drained := sink.Drain(); len(drained) != 0 {
			t.Fatalf("expected the sink drained by the run, got %d records", len(drained))
		}
		nodes := result.SessionTree.ByType(tree.TypeContext)
		if len(nodes) != 1 {
			t.Fatalf("expected 1 context event node, got %d", len(nodes))
		}
		if !strings.Contains(nodes[0].Content, "focus 100") {
			t.Fatalf("expected the composition detail in the node, got: %s", nodes[0].Content)
		}
		if nodes[0].Parent != "root" {
			t.Fatalf("expected the context node under the session root, got parent %q", nodes[0].Parent)
		}
		if nodes[0].Type.Prefix() != "event" {
			t.Fatalf("expected the context node in the event family, got %v", nodes[0].Type)
		}
	})
}
