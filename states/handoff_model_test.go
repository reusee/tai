package states

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

func TestGetHandoffGenerators(t *testing.T) {
	m := new(Module)

	var resolved []string
	get := func(name string) (generators.Generator, error) {
		resolved = append(resolved, name)
		return &mockSummarizerGenerator{}, nil
	}
	defaultGen := func() (generators.Generator, error) {
		resolved = append(resolved, "default")
		return &mockSummarizerGenerator{}, nil
	}

	t.Run("SingleModel", func(t *testing.T) {
		resolved = nil
		fn := m.GetHandoffGenerators(
			flags.HandoffModel("model-a"),
			flags.FastModelName(""),
			defaultGen,
			get,
		)
		gens, err := fn()
		if err != nil {
			t.Fatal(err)
		}
		if len(gens) != 1 {
			t.Fatalf("expected 1 generator, got %d", len(gens))
		}
		if len(resolved) != 1 || resolved[0] != "model-a" {
			t.Fatalf("expected model-a resolved, got %v", resolved)
		}
	})

	t.Run("SingleModelFromFast", func(t *testing.T) {
		resolved = nil
		fn := m.GetHandoffGenerators(
			flags.HandoffModel(""),
			flags.FastModelName("fast-model"),
			defaultGen,
			get,
		)
		gens, err := fn()
		if err != nil {
			t.Fatal(err)
		}
		if len(gens) != 1 {
			t.Fatalf("expected 1 generator, got %d", len(gens))
		}
	})

	t.Run("DefaultFallback", func(t *testing.T) {
		resolved = nil
		fn := m.GetHandoffGenerators(
			flags.HandoffModel(""),
			flags.FastModelName(""),
			defaultGen,
			get,
		)
		gens, err := fn()
		if err != nil {
			t.Fatal(err)
		}
		if len(gens) != 1 {
			t.Fatalf("expected 1 generator from default, got %d", len(gens))
		}
	})

	t.Run("ResolutionError", func(t *testing.T) {
		getErr := func(name string) (generators.Generator, error) {
			return nil, errors.New("bad")
		}
		fn := m.GetHandoffGenerators(
			flags.HandoffModel("bad"),
			flags.FastModelName(""),
			defaultGen,
			getErr,
		)
		if _, err := fn(); err == nil {
			t.Fatal("expected error for unresolvable model")
		}
	})
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
