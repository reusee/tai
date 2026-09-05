package pipeline

import (
	"context"
	"testing"

	"github.com/reusee/dscope"
	"github.com/reusee/tai/generators"
	"github.com/reusee/tai/modes"
)

// TestRunDecoratorsAppliedByModuleRun verifies that Module.Run applies
// the scope's RunDecorators inside its provider, in list order, after
// binding the per-run continuation: a decorator forked into the scope
// wraps the resolved Run, and the decorated run still executes the
// pipeline. See TheoryOfRunDecorators.
func TestRunDecoratorsAppliedByModuleRun(t *testing.T) {
	scope := dscope.New(modes.ForTest(t), new(Module))
	var applied []string
	scope = scope.Fork(func() RunDecorators {
		return RunDecorators{
			func(run Run) Run {
				applied = append(applied, "first")
				return run
			},
			func(run Run) Run {
				applied = append(applied, "second")
				return run
			},
		}
	})
	scope.Call(func(run Run) {
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
		if result.SessionTree == nil {
			t.Fatal("expected the decorated run to complete with a session tree")
		}
	})
	if len(applied) != 2 || applied[0] != "first" || applied[1] != "second" {
		t.Fatalf("expected both decorators applied in order, got %v", applied)
	}
}
