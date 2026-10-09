package components

import (
	"context"
	"strings"
	"testing"

	"github.com/reusee/tai/blocks"
	"github.com/reusee/tai/generators"
)

func TestCommonComponentsSharesShellTasksAcrossRounds(t *testing.T) {
	// The shell component carries one task registry for the session: a
	// command started with op=background in one round is collectable in a
	// later round. See blocks.TheoryOfShellTasks.
	comps := CommonComponents(true, nil)
	var shellComp Component
	for _, comp := range comps {
		if comp.Kind == "shell" {
			shellComp = comp
		}
	}
	if shellComp.Process == nil {
		t.Fatal("the shell component must be present when shell is enabled")
	}

	ctx := context.Background()
	started := shellComp.Process(ctx, &ProcessContext{
		Blocks: []blocks.Block{{
			Kind:       "shell",
			Attributes: map[string]string{"op": "background"},
			Body:       "echo shared registry",
		}},
	})
	if started.Err != nil {
		t.Fatalf("starting a task failed: %v", started.Err)
	}
	if len(started.Parts) != 1 {
		t.Fatalf("expected 1 part, got %d", len(started.Parts))
	}
	if got := string(started.Parts[0].(generators.Text)); !strings.Contains(got, "Shell task 1 started") {
		t.Fatalf("expected the task number, got: %s", got)
	}

	collected := shellComp.Process(ctx, &ProcessContext{
		Blocks: []blocks.Block{{
			Kind:       "shell",
			Attributes: map[string]string{"op": "output", "task": "1"},
		}},
	})
	if collected.Err != nil {
		t.Fatalf("collecting a task failed: %v", collected.Err)
	}
	if len(collected.Parts) != 1 {
		t.Fatalf("expected 1 part, got %d", len(collected.Parts))
	}
	if got := string(collected.Parts[0].(generators.Text)); !strings.Contains(got, "shared registry") {
		t.Fatalf("the registry must be shared across rounds, got: %s", got)
	}
}
