package states

import (
	"errors"
	"testing"

	"github.com/reusee/tai/flags"
	"github.com/reusee/tai/generators"
)

func TestGetSummarizeGeneratorSelection(t *testing.T) {
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

	t.Run("SummarizeModelConfigured", func(t *testing.T) {
		names = nil
		fn := m.GetSummarizeGenerator(
			flags.SummarizeModel("sum-model"),
			flags.FastModelName(""),
			defaultGen,
			get,
		)
		if _, err := fn(); err != nil {
			t.Fatal(err)
		}
		if len(names) != 1 || names[0] != "sum-model" {
			t.Fatalf("expected sum-model, got %v", names)
		}
	})

	t.Run("FastModelConfigured", func(t *testing.T) {
		names = nil
		fn := m.GetSummarizeGenerator(
			flags.SummarizeModel(""),
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
		fn := m.GetSummarizeGenerator(
			flags.SummarizeModel(""),
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
		fn := m.GetSummarizeGenerator(
			flags.SummarizeModel("bad"),
			flags.FastModelName(""),
			defaultGen,
			get,
		)
		if _, err := fn(); err == nil {
			t.Fatal("expected resolution error for configured model")
		}
	})
}

func TestGetDefaultSummarizerUsesSummarizeGenerator(t *testing.T) {
	m := new(Module)
	called := false
	getSummarize := func() (generators.Generator, error) {
		called = true
		return &mockSummarizerGenerator{}, nil
	}

	fn := m.GetDefaultSummarizer(getSummarize, true, "")
	s, err := fn()
	if err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("expected the summarize generator to be used")
	}
	if s == nil {
		t.Fatal("expected a Summarizer")
	}

	called = false
	fn = m.GetDefaultSummarizer(getSummarize, false, "")
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
