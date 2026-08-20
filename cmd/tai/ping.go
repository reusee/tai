package main

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"strings"

	"github.com/reusee/tai/blocks"
	"github.com/reusee/tai/flags"
	"github.com/reusee/tai/generators"
	"github.com/reusee/tai/loops"
	"github.com/reusee/tai/modes"
	"github.com/reusee/tai/phases"
	"github.com/reusee/tai/records"
)

const TheoryOfPingCommand = `
The "ping" subcommand tests whether a model is reachable and responding, and
whether it can emit the heredoc blocks the tai tooling relies on. The command
randomly chooses two block kinds, asks the model to emit exactly one block of
each kind, and validates the parsed output after generation: each required
kind must appear exactly once, and no other block may appear. The random
kinds — short lowercase letter strings from RandomBlockKinds — are unknown
before each run, so a correct result demonstrates genuine instruction
following and format ability rather than pattern memory. The block-format
instructions live in the system prompt (blocks.BlockFormatSystemPrompt); the
user message (pingBlockPrompt) states only the test requirements without
repeating the format description. The system prompt also carries the
user-configured extra system prompts (extra_system_prompt and
family_extra_system_prompt), so ping honors the same configuration as the
other generation commands. The block format prompt requires a distinct
delimiter per block: two blocks sharing a delimiter would be mis-parsed as
nested (see blocks.TheoryOfNestedBlockParsing), so identical delimiters
cannot pass the test. Validation reads the blocks collected in
loops.Result.RemainingBlocks: ping runs without components, so no block kind
is consumed. A validation failure prints the observed outcome and exits with
status 1, making the command scriptable; on success the verdict is printed
to the command Output writer after the streamed output.

The command requires a model to be specified via -model; without it, resolving
the default generator fails, making the dependency on an explicit model
selection explicit. The command performs a single generation round with no
chat loop and no file context; its system prompt carries the block-format
prompt and the user-configured extra system prompts.
`

// RandomBlockKinds returns the two block kinds the ping command asks the
// model to emit, chosen at random on each run. The kinds are short lowercase
// letter strings so the model can reproduce them exactly, yet they vary per
// run, so a correct emission demonstrates genuine instruction-following and
// block-format ability rather than pattern memory. See TheoryOfPingCommand.
type RandomBlockKinds func() (kindA string, kindB string)

func (Module) RandomBlockKinds() RandomBlockKinds {
	return func() (kindA string, kindB string) {
		const letters = "abcdefghijklmnopqrstuvwxyz"
		kind := func() string {
			n := rand.IntN(6) + 3 // 3..8 letters, short enough to reproduce exactly
			b := make([]byte, n)
			for i := range b {
				b[i] = letters[rand.IntN(len(letters))]
			}
			return string(b)
		}
		kindA = kind()
		kindB = kind()
		for kindB == kindA {
			kindB = kind()
		}
		return
	}
}

func pingBlockPrompt(kindA, kindB string) string {
	return fmt.Sprintf(`This is a block-generation test. Emit exactly two blocks.

The two required block kinds (each used exactly once, in any order):
1. %s
2. %s

Rules:
- The kind must be one of the two required kinds listed above.
- The body may be any short text.
- Emit only the two blocks and nothing else: no prose, no explanations, no additional blocks.`, kindA, kindB)
}

// validatePingBlocks checks that the model emitted exactly one block of each
// required kind and no other blocks. Only parsed blocks are inspected: prose
// around the blocks is discarded by ParserState and never appears in
// loops.Result.RemainingBlocks. See TheoryOfPingCommand.
func validatePingBlocks(result loops.Result, kindA, kindB string) error {
	gotA := 0
	gotB := 0
	var extras []string
	for _, block := range result.RemainingBlocks {
		switch block.Kind {
		case kindA:
			gotA++
		case kindB:
			gotB++
		default:
			extras = append(extras, block.Kind)
		}
	}
	if gotA == 1 && gotB == 1 && len(extras) == 0 {
		return nil
	}
	var message strings.Builder
	fmt.Fprintf(&message, "expected exactly one block of kind %q and one block of kind %q; got %d block(s) of kind %q, %d block(s) of kind %q",
		kindA, kindB, gotA, kindA, gotB, kindB)
	if len(extras) > 0 {
		fmt.Fprintf(&message, ", and %d other block(s) (%s)",
			len(extras), strings.Join(extras, ", "))
	}
	return errors.New(message.String())
}

var PingCommand = Command{
	Defs: []any{
		modes.ForProduction(),
	},
	Main: func(
		output Output,
		recorder *records.Recorder,
		getDefaultGenerator generators.GetDefaultGenerator,
		buildGenerate phases.BuildGenerate,
		loopRun loops.Run,
		randomBlockKinds RandomBlockKinds,
		extra flags.ExtraSystemPrompt,
		familyExtra flags.FamilyExtraSystemPrompt,
		modelFamily generators.ModelFamily,
	) {
		ctx := context.Background()

		generator, err := getDefaultGenerator()
		ce(err)

		// Two block kinds are chosen at random on every run so the model
		// cannot match the request from pattern memory; a correct emission
		// demonstrates both availability and basic block-generation ability.
		// See TheoryOfPingCommand.
		kindA, kindB := randomBlockKinds()

		// The block-format instructions live in the system prompt
		// (blocks.BlockFormatSystemPrompt), so the user message
		// (pingBlockPrompt) states only the test requirements without
		// repeating the format description. The system prompt also
		// carries the user-configured extra system prompts
		// (extra_system_prompt and family_extra_system_prompt), so ping
		// honors the same configuration as the other generation
		// commands. Each prompt section is separated by a blank line so
		// adjacent sections never stick together. See TheoryOfPingCommand.
		systemPrompt := strings.TrimRight(blocks.BlockFormatSystemPrompt, " \t\n\r")
		for _, e := range extra {
			if e != "" {
				systemPrompt += "\n\n" + strings.TrimRight(e, " \t\n\r")
			}
		}
		for _, prompt := range familyExtra[string(modelFamily)] {
			if prompt != "" {
				systemPrompt += "\n\n" + strings.TrimRight(prompt, " \t\n\r")
			}
		}
		systemPrompt += "\n"

		var state generators.State
		state = generators.NewPrompts(
			systemPrompt,
			[]*generators.Content{
				{
					Role: generators.RoleUser,
					Parts: []generators.Part{
						generators.Text(pingBlockPrompt(kindA, kindB)),
					},
				},
			},
		)
		state = generators.NewOutput(state, os.Stdout, true)

		// Run the unified generation loop in single-shot mode (no
		// components). The loop handles ParserState wrapping, phase
		// execution, and interaction recording; the TUI's finish-reason
		// observer is applied via RunOptions.StateDecorators when -tui
		// is enabled. Parsed blocks are collected in result.RemainingBlocks
		// because no component consumes them. The result is filled into
		// result as the run progresses; the iterator yields the terminal
		// error, if any. See loops.TheoryOfLoops and TheoryOfTUI.
		var result loops.Result
		for e := range loopRun(ctx, loops.RunOptions{
			Generator:           generator,
			InitialState:        state,
			Components:          nil,
			Command:             "ping",
			InteractionRecorder: recorder,
			PhaseBuilder: func(g generators.Generator) phases.Phase {
				return buildGenerate(g, nil)(nil)
			},
		}, &result) {
			err = e
		}
		ce(err)

		// Validate the emitted blocks after generation. A validation
		// failure exits with status 1 so the command is scriptable:
		// availability alone is not enough, the model must also emit the
		// required blocks in the required format.
		// See TheoryOfPingCommand.
		if err := validatePingBlocks(result, kindA, kindB); err != nil {
			fmt.Fprintf(os.Stderr, "ping failed: %v\n", err)
			if len(result.ParseErrors) > 0 {
				fmt.Fprintf(os.Stderr, "ping: %d malformed block(s) were detected during parsing, e.g. kind %q with boundary %q\n",
					len(result.ParseErrors), result.ParseErrors[0].BlockKind, result.ParseErrors[0].Boundary)
			}
			os.Exit(1)
		}

		// The verdict is written to the command Output writer so it is
		// visible in the TUI's output tab instead of being discarded with
		// stdout. See TheoryOfCommandOutput and TheoryOfPingCommand.
		fmt.Fprintf(output, "ping ok: model emitted the required blocks (%q, %q)\n", kindA, kindB)
	},
}
