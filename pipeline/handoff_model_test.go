package pipeline

import (
	"errors"
	"testing"

	"github.com/reusee/tai/flags"
	"github.com/reusee/tai/generators"
)

func TestGetHandoffGeneratorSelection(t *testing.T) {
	m := new(Module)

	var names []string
	get := func(name string) (generators.Generator, error) {
		names = append(names, name)
		return &mockSummarizerGenerator{}, nil
	}
	defaultGen := func() (generators.Generator, error) {
		names = append(names, "default")
		return &mockSummarizerGenerator{}, nil
	}

	t.Run("HandoffModelConfigured", func(t *testing.T) {
		names = nil
		fn := m.GetHandoffGenerator(
			flags.HandoffModel("model-a"),
			flags.FastModelName(""),
			defaultGen,
			get,
		)
		if _, err := fn(); err != nil {
			t.Fatal(err)
		}
		if len(names) != 1 || names[0] != "model-a" {
			t.Fatalf("expected handoff model, got %v", names)
		}
	})

	t.Run("FastModelConfigured", func(t *testing.T) {
		names = nil
		fn := m.GetHandoffGenerator(
			flags.HandoffModel(""),
			flags.FastModelName("fast-model"),
			defaultGen,
			get,
		)
		if _, err := fn(); err != nil {
			t.Fatal(err)
		}
		if len(names) != 1 || names[0] != "fast-model" {
			t.Fatalf("expected fast-model, got %v", names)
		}
	})

	t.Run("DefaultModel", func(t *testing.T) {
		names = nil
		fn := m.GetHandoffGenerator(
			flags.HandoffModel(""),
			flags.FastModelName(""),
			defaultGen,
			get,
		)
		if _, err := fn(); err != nil {
			t.Fatal(err)
		}
		if len(names) != 1 || names[0] != "default" {
			t.Fatalf("expected default, got %v", names)
		}
	})

	t.Run("ResolutionErrorSurfaced", func(t *testing.T) {
		names = nil
		get := func(name string) (generators.Generator, error) {
			names = append(names, name)
			return nil, errors.New("bad model")
		}
		fn := m.GetHandoffGenerator(
			flags.HandoffModel("bad"),
			flags.FastModelName(""),
			defaultGen,
			get,
		)
		if _, err := fn(); err == nil {
			t.Fatal("expected resolution error for configured model")
		}
	})
}

// TestGetHandoffGenerators covers the wrapper's own contract: the resolved
// handoff generator is returned as a single-element slice and its resolution
// error is propagated. The model-selection chain (handoff model, fast model,
// default) is covered by TestGetHandoffGeneratorSelection.
func TestGetHandoffGenerators(t *testing.T) {
	m := new(Module)

	fn := m.GetHandoffGenerators(func() (generators.Generator, error) {
		return &mockSummarizerGenerator{}, nil
	})
	gens, err := fn()
	if err != nil {
		t.Fatal(err)
	}
	if len(gens) != 1 {
		t.Fatalf("expected 1 generator, got %d", len(gens))
	}

	fn = m.GetHandoffGenerators(func() (generators.Generator, error) {
		return nil, errors.New("bad")
	})
	if _, err := fn(); err == nil {
		t.Fatal("expected error propagation from the handoff generator")
	}
}

func TestGetDefaultSummarizerUsesHandoffGenerator(t *testing.T) {
	m := new(Module)
	called := false
	getHandoff := func() (generators.Generator, error) {
		called = true
		return &mockSummarizerGenerator{}, nil
	}

	fn := m.GetDefaultSummarizer(getHandoff, true, "")
	s, err := fn()
	if err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("expected the handoff generator to be used")
	}
	if s == nil {
		t.Fatal("expected a Summarizer")
	}

	called = false
	fn = m.GetDefaultSummarizer(getHandoff, false, "")
	s, err = fn()
	if err != nil {
		t.Fatal(err)
	}
	if called {
		t.Fatal("disabled summarization must not resolve a generator")
	}
	if s != nil {
		t.Fatal("disabled summarization must return nil")
	}
}
