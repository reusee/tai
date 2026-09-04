package main

import (
	"context"
	"testing"

	"github.com/reusee/dscope"
	"github.com/reusee/tai/components"
	"github.com/reusee/tai/generators"
	"github.com/reusee/tai/modes"
)

// TestMemoryComponentProcessIsInert verifies that the ai command's
// memory component carries an inert Process function: the actual
// profile update runs post-loop in ai.go's OnAttemptSuccess hook, and
// the Process exists only so the generation loop records a
// ComponentOutput for memory blocks, which attaches a block-result
// child to each memory block node in the session tree. A nil Process
// would leave the block childless, reading as unprocessed. See
// TheoryOfAIComponents and pipeline.TheoryOfSessionTree.
func TestMemoryComponentProcessIsInert(t *testing.T) {
	dscope.New(
		new(Module),
	).Fork(
		modes.ForTest(t),
		func() generators.GetDefaultGenerator {
			return func() (generators.Generator, error) {
				return aiMockGenerator{}, nil
			}
		},
	).Call(func(comps AIComponents) {
		found := false
		for _, comp := range comps.Processable() {
			if comp.Kind != "memory" {
				continue
			}
			found = true
			result := comp.Process(context.Background(), &components.ProcessContext{})
			if len(result.Parts) != 0 || result.State != nil || result.Tree != nil || result.Err != nil {
				t.Fatalf("memory component's Process must stay inert, got %+v", result)
			}
		}
		if !found {
			t.Fatal("expected a processable memory component")
		}
	})
}
