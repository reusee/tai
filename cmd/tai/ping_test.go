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
	"github.com/reusee/tai/flags"
	"github.com/reusee/tai/generators"
	"github.com/reusee/tai/phases"
	"github.com/reusee/tai/pipeline"
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

func TestRandomPingBlocks(t *testing.T) {
	var provider RandomPingBlocks
	dscope.New(
		new(Module),
	).Call(func(
		randomPingBlocks RandomPingBlocks,
	) {
		provider = randomPingBlocks
	})
	checkLowercase := func(s string) {
		if s == "" {
			t.Fatal("expected a non-empty string")
		}
		for _, ch := range s {
			if ch < 'a' || ch > 'z' {
				t.Fatalf("expected lowercase letters only, got %q", s)
			}
		}
	}
	tricky := make(map[string]bool)
	for _, value := range pingTrickyValues {
		tricky[value] = true
	}
	for i := 0; i < 100; i++ {
		specs := provider()
		if len(specs) != 3 {
			t.Fatalf("expected 3 block specs, got %d", len(specs))
		}
		kinds := make(map[string]bool)
		foundTricky := false
		for _, spec := range specs {
			checkLowercase(spec.Kind)
			if kinds[spec.Kind] {
				t.Fatalf("expected distinct kind names, got duplicate %q", spec.Kind)
			}
			kinds[spec.Kind] = true
			if len(spec.Attributes) < 1 || len(spec.Attributes) > 3 {
				t.Fatalf("expected 1..3 parameter pairs, got %d: %v", len(spec.Attributes), spec.Attributes)
			}
			for name, value := range spec.Attributes {
				checkLowercase(name)
				if tricky[value] {
					foundTricky = true
				} else {
					checkLowercase(value)
				}
			}
			if spec.Body == "" {
				t.Fatal("expected a non-empty body")
			}
			for _, word := range strings.Fields(spec.Body) {
				checkLowercase(word)
			}
		}
		if !foundTricky {
			t.Fatalf("expected at least one tricky parameter value per run, got %+v", specs)
		}
	}
}

func TestPingBlockPrompt(t *testing.T) {
	specs := []PingBlockSpec{
		{Kind: "abc", Attributes: map[string]string{"foo": "bar", "id": "qux"}, Body: "first body"},
		{Kind: "xyz", Attributes: map[string]string{"tag": `say "hi"`}, Body: "second body"},
	}
	prompt := pingBlockPrompt(specs)
	// The prompt carries the kinds, their exact parameter pairs — sorted
	// by name, with tricky values shown in one valid escaped form — the
	// exact bodies, and the ordering requirement, so the model can
	// reproduce them exactly.
	for _, s := range []string{
		"abc",
		"xyz",
		`foo="bar", id="qux"`,
		"tag=\"say \\\"hi\\\"\"",
		"first body",
		"second body",
		"in this exact order",
	} {
		if !strings.Contains(prompt, s) {
			t.Fatalf("prompt must contain %q, got: %s", s, prompt)
		}
	}
	// The block-format description lives in the system prompt
	// (blocks.BlockFormatSystemPrompt); the user prompt must not repeat
	// it. See TheoryOfPingCommand.
	if strings.Contains(prompt, "uncommon Chinese two-character word") {
		t.Fatal("user prompt must not repeat the delimiter policy; it lives in the system prompt")
	}
	if strings.Contains(prompt, "heredoc-delimited format") {
		t.Fatal("user prompt must not repeat the block format description; it lives in the system prompt")
	}
}

func TestValidatePingBlocks(t *testing.T) {
	specs := []PingBlockSpec{
		{Kind: "abc", Attributes: map[string]string{"foo": "bar"}, Body: "body one"},
		{Kind: "xyz", Attributes: map[string]string{"tag": "qux"}, Body: "body two"},
	}

	t.Run("MissingBlocks", func(t *testing.T) {
		if err := validatePingBlocks(pipeline.Result{}, specs); err == nil {
			t.Fatal("expected an error when no blocks are emitted")
		}
	})

	t.Run("ExactMatch", func(t *testing.T) {
		result := pipeline.Result{RemainingBlocks: pingResultBlocks(specs)}
		if err := validatePingBlocks(result, specs); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("TrickyValueDecoding", func(t *testing.T) {
		// The prompt shows key="say \"hi\""; the header parser decodes
		// the escape, so validation compares against the decoded value
		// and any equivalent escaping passes.
		// See TheoryOfPingCommand.
		tricky := []PingBlockSpec{
			{Kind: "abc", Attributes: map[string]string{"key": `say "hi"`}, Body: "body one"},
		}
		result := pipeline.Result{RemainingBlocks: pingResultBlocks(tricky)}
		if err := validatePingBlocks(result, tricky); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("WrongOrderFails", func(t *testing.T) {
		bs := pingResultBlocks(specs)
		bs[0], bs[1] = bs[1], bs[0]
		if err := validatePingBlocks(pipeline.Result{RemainingBlocks: bs}, specs); err == nil {
			t.Fatal("expected an error when the blocks are emitted in the wrong order")
		}
	})

	t.Run("DuplicateKindFails", func(t *testing.T) {
		bs := pingResultBlocks(specs)
		bs[1].Kind = specs[0].Kind
		if err := validatePingBlocks(pipeline.Result{RemainingBlocks: bs}, specs); err == nil {
			t.Fatal("expected an error when one kind is duplicated")
		}
	})

	t.Run("WrongBodyFails", func(t *testing.T) {
		bs := pingResultBlocks(specs)
		bs[0].Body = "wrong body"
		if err := validatePingBlocks(pipeline.Result{RemainingBlocks: bs}, specs); err == nil {
			t.Fatal("expected an error when a block body differs from the required body")
		}
	})

	t.Run("ExtraBlockFails", func(t *testing.T) {
		bs := append(pingResultBlocks(specs), blocks.Block{Kind: "other"})
		if err := validatePingBlocks(pipeline.Result{RemainingBlocks: bs}, specs); err == nil {
			t.Fatal("expected an error when an extra block is emitted")
		}
	})

	t.Run("WrongParameterValueFails", func(t *testing.T) {
		bs := pingResultBlocks(specs)
		bs[0].Attributes["foo"] = "wrong"
		if err := validatePingBlocks(pipeline.Result{RemainingBlocks: bs}, specs); err == nil {
			t.Fatal("expected an error when a parameter value differs from the required pair")
		}
	})

	t.Run("MissingParametersFails", func(t *testing.T) {
		bs := pingResultBlocks(specs)
		bs[0].Attributes = nil
		if err := validatePingBlocks(pipeline.Result{RemainingBlocks: bs}, specs); err == nil {
			t.Fatal("expected an error when a required parameter pair is missing")
		}
	})

	t.Run("ExtraParameterFails", func(t *testing.T) {
		bs := pingResultBlocks(specs)
		bs[0].Attributes["id"] = "extra"
		if err := validatePingBlocks(pipeline.Result{RemainingBlocks: bs}, specs); err == nil {
			t.Fatal("expected an error when an extra parameter pair is emitted")
		}
	})
}

// pingResultBlocks builds the RemainingBlocks of a passing run: one block
// per spec, in order, with copies of the exact attributes (decoded
// values) and the exact body. Tests mutate the returned blocks to build
// failing cases; the copied maps keep the specs intact.
func pingResultBlocks(specs []PingBlockSpec) []blocks.Block {
	ret := make([]blocks.Block, len(specs))
	for i, spec := range specs {
		attrs := make(map[string]string, len(spec.Attributes))
		for name, value := range spec.Attributes {
			attrs[name] = value
		}
		ret[i] = blocks.Block{Kind: spec.Kind, Attributes: attrs, Body: spec.Body}
	}
	return ret
}

func TestPingCommandUsesRunLoop(t *testing.T) {
	// Ping must run through the unified generation loop so it
	// participates in the TUI mechanism (finish-reason observer,
	// generating hint) and interaction recording. The fake run simulates
	// the model emitting exactly the required blocks, in order, with the
	// exact parameter values (decoded, including the tricky escaped
	// value) and exact bodies so the validation passes.
	// See TheoryOfPingCommand.
	specs := []PingBlockSpec{
		{Kind: "abc", Attributes: map[string]string{"foo": "bar"}, Body: "body one"},
		{Kind: "xyz", Attributes: map[string]string{"tag": "qux"}, Body: "body two"},
		{Kind: "zzz", Attributes: map[string]string{"key": `say "hi"`}, Body: "body three"},
	}
	var gotOpts pipeline.RunOptions
	fakeRun := func(ctx context.Context, opts pipeline.RunOptions, result *pipeline.Result) iter.Seq[error] {
		gotOpts = opts
		result.RemainingBlocks = pingResultBlocks(specs)
		return func(yield func(error) bool) {}
	}

	mainFn, ok := PingCommand.Main.(func(Output, *records.Recorder, generators.GetDefaultGenerator, phases.BuildGenerate, pipeline.Run, RandomPingBlocks, flags.ExtraSystemPrompt, flags.FamilyExtraSystemPrompt, generators.ModelFamily))
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
		Output(os.Stdout),
		nil,
		func() (generators.Generator, error) { return aiMockGenerator{}, nil },
		func(generator generators.Generator, options *generators.GenerateOptions) phases.PhaseBuilder {
			return func(cont phases.Phase) phases.Phase {
				return func(ctx context.Context, state generators.State) (phases.Phase, generators.State, error) {
					return nil, state, nil
				}
			}
		},
		fakeRun,
		func() []PingBlockSpec { return specs },
		nil,
		nil,
		"",
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
	if !strings.Contains(gotOpts.InitialState.SystemPrompt(), "uncommon Chinese two-character word") {
		t.Fatal("expected the block format prompt in the ping system prompt")
	}
	var prompt strings.Builder
	for c := range gotOpts.InitialState.Contents() {
		for _, p := range c.Parts {
			if text, ok := p.(generators.Text); ok {
				prompt.WriteString(string(text))
			}
		}
	}
	for _, kind := range []string{"abc", "xyz", "zzz"} {
		if !strings.Contains(prompt.String(), kind) {
			t.Fatalf("expected required kind %q in the user prompt", kind)
		}
	}
	for _, pair := range []string{`foo="bar"`, `tag="qux"`, "key=\"say \\\"hi\\\"\""} {
		if !strings.Contains(prompt.String(), pair) {
			t.Fatalf("expected required parameter pair %q in the user prompt", pair)
		}
	}
	for _, body := range []string{"body one", "body two", "body three"} {
		if !strings.Contains(prompt.String(), body) {
			t.Fatalf("expected required body %q in the user prompt", body)
		}
	}
	if !strings.Contains(string(output), "ping ok") {
		t.Fatalf("expected the success verdict on stdout, got %q", string(output))
	}
}

func TestPingCommandInjectsExtraSystemPrompt(t *testing.T) {
	// The ping command must inject the user-configured extra system
	// prompts (extra_system_prompt and family_extra_system_prompt) into
	// its system prompt, honoring the same configuration as the other
	// generation commands. See TheoryOfPingCommand.
	specs := []PingBlockSpec{
		{Kind: "abc", Attributes: map[string]string{"foo": "bar"}, Body: "body one"},
		{Kind: "xyz", Attributes: map[string]string{"tag": "qux"}, Body: "body two"},
	}
	var gotOpts pipeline.RunOptions
	fakeRun := func(ctx context.Context, opts pipeline.RunOptions, result *pipeline.Result) iter.Seq[error] {
		gotOpts = opts
		result.RemainingBlocks = pingResultBlocks(specs)
		return func(yield func(error) bool) {}
	}

	mainFn, ok := PingCommand.Main.(func(Output, *records.Recorder, generators.GetDefaultGenerator, phases.BuildGenerate, pipeline.Run, RandomPingBlocks, flags.ExtraSystemPrompt, flags.FamilyExtraSystemPrompt, generators.ModelFamily))
	if !ok {
		t.Fatalf("unexpected Main type: %T", PingCommand.Main)
	}

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	mainFn(
		Output(os.Stdout),
		nil,
		func() (generators.Generator, error) { return aiMockGenerator{}, nil },
		func(generator generators.Generator, options *generators.GenerateOptions) phases.PhaseBuilder {
			return func(cont phases.Phase) phases.Phase {
				return func(ctx context.Context, state generators.State) (phases.Phase, generators.State, error) {
					return nil, state, nil
				}
			}
		},
		fakeRun,
		func() []PingBlockSpec { return specs },
		flags.ExtraSystemPrompt{"generic extra prompt"},
		flags.FamilyExtraSystemPrompt{"test-family": {"family extra prompt"}},
		generators.ModelFamily("test-family"),
	)
	w.Close()
	os.Stdout = oldStdout
	output, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	r.Close()

	if gotOpts.InitialState == nil {
		t.Fatal("expected non-nil initial state")
	}
	systemPrompt := gotOpts.InitialState.SystemPrompt()
	if !strings.Contains(systemPrompt, "uncommon Chinese two-character word") {
		t.Fatalf("expected the block format prompt in the ping system prompt, got %q", systemPrompt)
	}
	if !strings.Contains(systemPrompt, "generic extra prompt") {
		t.Fatalf("expected the generic extra system prompt in the ping system prompt, got %q", systemPrompt)
	}
	if !strings.Contains(systemPrompt, "family extra prompt") {
		t.Fatalf("expected the family extra system prompt in the ping system prompt, got %q", systemPrompt)
	}
	if !strings.Contains(string(output), "ping ok") {
		t.Fatalf("expected the success verdict on stdout, got %q", string(output))
	}
}
