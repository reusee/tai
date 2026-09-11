package main

import (
	"os"
	"testing"

	"github.com/reusee/tai/generators"
)

func TestTuiOutputStateNeverReturnsNilOnUpstreamError(t *testing.T) {
	// A closed file makes the upstream output layer fail. The returned
	// state must still carry its chain — never nil — because the
	// generation loop's retry gate reads the content increase from it.
	// See generators.TheoryOfStateImmutability.
	f, err := os.CreateTemp(t.TempDir(), "output")
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	upstream := generators.NewOutput(generators.NewPrompts("", nil), f, true)
	var state generators.State = tuiOutputState{upstream: upstream, tui: newTUIForTest()}

	newState, err := state.AppendContent(&generators.Content{
		Role:  generators.RoleModel,
		Parts: []generators.Part{generators.Text("hello")},
	})
	if err == nil {
		t.Fatal("expected the upstream write to fail")
	}
	if newState == nil {
		t.Fatal("AppendContent must never return a nil state on error")
	}

	newState, err = newState.Flush()
	if err == nil {
		t.Fatal("expected the upstream write to fail")
	}
	if newState == nil {
		t.Fatal("Flush must never return a nil state on error")
	}
}
