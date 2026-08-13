package main

import (
	"context"
	"iter"
	"strings"
	"testing"

	"github.com/reusee/dscope"
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

func TestPingCommandUsesRunLoop(t *testing.T) {
	// Ping must run through the unified generation loop so it
	// participates in the TUI mechanism (finish-reason observer,
	// generating hint) and interaction recording. See TheoryOfPingCommand.
	var gotOpts loops.RunOptions
	fakeRun := func(ctx context.Context, opts loops.RunOptions, result *loops.Result) iter.Seq[error] {
		gotOpts = opts
		return func(yield func(error) bool) {}
	}

	mainFn, ok := PingCommand.Main.(func(*records.Recorder, generators.Generator, phases.BuildGenerate, loops.Run))
	if !ok {
		t.Fatalf("unexpected Main type: %T", PingCommand.Main)
	}
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
	)

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
	var foundHello bool
	for c := range gotOpts.InitialState.Contents() {
		for _, p := range c.Parts {
			if text, ok := p.(generators.Text); ok && strings.Contains(string(text), "hello") {
				foundHello = true
			}
		}
	}
	if !foundHello {
		t.Fatal("expected hello message in initial state")
	}
}
