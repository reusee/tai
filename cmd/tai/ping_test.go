package main

import (
	"context"
	"io"
	"iter"
	"os"
	"strings"
	"testing"

	"github.com/reusee/dscope"
	"github.com/reusee/tai/blocks"
	"github.com/reusee/tai/generators"
	"github.com/reusee/tai/loops"
	"github.com/reusee/tai/phases"
	"github.com/reusee/tai/records"
)

func TestPingCommandRegistered(t *testing.T) {
	dscope.New(
		new(Module),
	).Call(func(
		cmd Command,
	) {
		keys := cmd.Keys()
		if _, ok := keys["ping"]; !ok {
			t.Fatal("ping command not registered in Keys()")
		}

		newValue, _, err := cmd.Handle("ping", nil)
		if err != nil {
			t.Fatalf("Handle ping failed: %v", err)
		}
		// Handle returns *Command (a pointer), matching the flags.Flag convention
		// where Handle returns a pointer to a typed value for scope.Fork.
		// See flags.Flag.Handle documentation.
		pingCmd, ok := newValue.(*Command)
		if !ok {
			t.Fatal("Handle ping did not return a *Command")
		}
		if pingCmd.Main == nil {
			t.Fatal("PingCommand has no Main")
		}
	})
}

func TestRandomBlockKinds(t *testing.T) {
	var provider RandomBlockKinds
	dscope.New(
		new(Module),
	).Call(func(
		randomBlockKinds RandomBlockKinds,
	) {
		provider = randomBlockKinds
	})
	for i := 0; i < 100; i++ {
		a, b := provider()
		if a == "" || b == "" {
			t.Fatal("expected non-empty kind names")
		}
		if a == b {
			t.Fatal("expected two distinct kind names")
		}
		for _, k := range []string{a, b} {
			for _, ch := range k {
				if ch < 'a' || ch > 'z' {
					t.Fatalf("expected lowercase letters in kind name, got %q", k)
				}
			}
		}
	}
}

func TestPingBlockPrompt(t *testing.T) {
	prompt := pingBlockPrompt("abc", "xyz")
	for _, kind := range []string{"abc", "xyz"} {
		if !strings.Contains(prompt, kind) {
			t.Fatalf("prompt must contain required kind %q", kind)
		}
	}
	if !strings.Contains(prompt, "uncommon Chinese characters") {
		t.Fatal("prompt must mandate the three-uncommon-Chinese-characters delimiter policy")
	}
	if !strings.Contains(prompt, "different delimiter") {
		t.Fatal("prompt must require a distinct delimiter per block")
	}
}

func TestValidatePingBlocks(t *testing.T) {
	kindA, kindB := "abc", "xyz"

	t.Run("MissingBlocks", func(t *testing.T) {
		if err := validatePingBlocks(loops.Result{}, kindA, kindB); err == nil {
			t.Fatal("expected an error when no blocks are emitted")
		}
	})

	t.Run("ExactMatch", func(t *testing.T) {
		result := loops.Result{RemainingBlocks: []blocks.Block{{Kind: kindA}, {Kind: kindB}}}
		if err := validatePingBlocks(result, kindA, kindB); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("OrderIndependent", func(t *testing.T) {
		result := loops.Result{RemainingBlocks: []blocks.Block{{Kind: kindB}, {Kind: kindA}}}
		if err := validatePingBlocks(result, kindA, kindB); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("DuplicateKindFails", func(t *testing.T) {
		result := loops.Result{RemainingBlocks: []blocks.Block{{Kind: kindA}, {Kind: kindA}}}
		if err := validatePingBlocks(result, kindA, kindB); err == nil {
			t.Fatal("expected an error when one kind is duplicated")
		}
	})

	t.Run("WrongKindFails", func(t *testing.T) {
		result := loops.Result{RemainingBlocks: []blocks.Block{{Kind: kindA}, {Kind: "wrong"}}}
		if err := validatePingBlocks(result, kindA, kindB); err == nil {
			t.Fatal("expected an error when a block has the wrong kind")
		}
	})

	t.Run("ExtraBlockFails", func(t *testing.T) {
		result := loops.Result{RemainingBlocks: []blocks.Block{{Kind: kindA}, {Kind: kindB}, {Kind: "other"}}}
		if err := validatePingBlocks(result, kindA, kindB); err == nil {
			t.Fatal("expected an error when an extra block is emitted")
		}
	})
}

func TestPingCommandUsesRunLoop(t *testing.T) {
	// Ping must run through the unified generation loop so it
	// participates in the TUI mechanism (finish-reason observer,
	// generating hint) and interaction recording. The fake run simulates
	// the model emitting exactly the two required blocks so the validation
	// passes. See TheoryOfPingCommand.
	const kindA = "abc"
	const kindB = "xyz"
	var gotOpts loops.RunOptions
	fakeRun := func(ctx context.Context, opts loops.RunOptions, result *loops.Result) iter.Seq[error] {
		gotOpts = opts
		result.RemainingBlocks = []blocks.Block{{Kind: kindA}, {Kind: kindB}}
		return func(yield func(error) bool) {}
	}

	mainFn, ok := PingCommand.Main.(func(*records.Recorder, generators.Generator, phases.BuildGenerate, loops.Run, RandomBlockKinds))
	if !ok {
		t.Fatalf("unexpected Main type: %T", PingCommand.Main)
	}

	// Capture stdout so the success verdict is asserted and does not
	// pollute the test output.
	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	mainFn(
		nil,
		aiMockGenerator{},
		func(generator generators.Generator, options *generators.GenerateOptions) phases.PhaseBuilder {
			return func(cont phases.Phase) phases.Phase {
				return func(ctx context.Context, state generators.State) (phases.Phase, generators.State, error) {
					return nil, state, nil
				}
			}
		},
		fakeRun,
		func() (string, string) { return kindA, kindB },
	)
	w.Close()
	os.Stdout = oldStdout
	output, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	r.Close()

	if gotOpts.Command != "ping" {
		t.Fatalf("expected command ping, got %q", gotOpts.Command)
	}
	if gotOpts.Components != nil {
		t.Fatalf("expected no components, got %v", gotOpts.Components)
	}
	if gotOpts.PhaseBuilder == nil {
		t.Fatal("expected non-nil phase builder")
	}
	if gotOpts.InitialState == nil {
		t.Fatal("expected non-nil initial state")
	}
	var prompt strings.Builder
	for c := range gotOpts.InitialState.Contents() {
		for _, p := range c.Parts {
			if text, ok := p.(generators.Text); ok {
				prompt.WriteString(string(text))
			}
		}
	}
	for _, kind := range []string{kindA, kindB} {
		if !strings.Contains(prompt.String(), kind) {
			t.Fatalf("expected required kind %q in the user prompt", kind)
		}
	}
	if !strings.Contains(string(output), "ping ok") {
		t.Fatalf("expected the success verdict on stdout, got %q", string(output))
	}
}
