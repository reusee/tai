package main

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"strings"

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
instructions live entirely in the user message (pingBlockPrompt) because
ping uses no system prompt. The prompt requires a distinct delimiter per
block: two blocks sharing a delimiter would be mis-parsed as nested (see
blocks.TheoryOfNestedBlockParsing), so identical delimiters cannot pass the
test. Validation reads the blocks collected in loops.Result.RemainingBlocks:
ping runs without components, so no block kind is consumed. A validation
failure prints the observed outcome and exits with status 1, making the
command scriptable; on success the verdict is printed to stdout after the
streamed output.

The command requires a model to be specified via -model; without it, the
generator provider fails during scope resolution, making the dependency on an
explicit model selection explicit. The command performs a single generation
round with no chat loop, no system prompt, and no file context.

Ping runs through the unified generation loop (loops.Run) in single-shot
mode (no components), so it participates in the same mechanisms as the
other generation commands: the TUI's finish-reason observer is applied
via RunOptions.StateDecorators when -tui is enabled, the "generating"
log record drives the TUI's in-flight hint, and interaction recording
is handled by the loop when -record is enabled. See TheoryOfTUI and
loops.TheoryOfLoops.
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

// pingBlockPrompt builds the user message for the block-generation test. It
// carries the full block-format instructions because ping uses no system
// prompt. The delimiter policy — exactly three uncommon Chinese characters,
// distinct per block — is restated here because the block parser rejects any
// other delimiter, and two blocks sharing a delimiter would be mis-parsed as
// nested. See TheoryOfPingCommand.
func pingBlockPrompt(kindA, kindB string) string {
	return fmt.Sprintf(`This is a block-generation test. Emit exactly two blocks in the heredoc-delimited format defined below.

The two required block kinds (each used exactly once, in any order):
1. %s
2. %s

Format of each block:
<<DELIMITER <kind>
<body>
DELIMITER

Rules:
- In the opening marker, replace DELIMITER with exactly three uncommon Chinese characters (for example, a rare trio of Han characters). The closing line of the block must be the same delimiter alone on its own line.
- Each of the two blocks MUST use a different delimiter; never reuse a delimiter.
- The <kind> must be one of the two required kinds listed above.
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
		recorder *records.Recorder,
		generator generators.Generator,
		buildGenerate phases.BuildGenerate,
		loopRun loops.Run,
		randomBlockKinds RandomBlockKinds,
	) {
		ctx := context.Background()

		// Two block kinds are chosen at random on every run so the model
		// cannot match the request from pattern memory; a correct emission
		// demonstrates both availability and basic block-generation ability.
		// See TheoryOfPingCommand.
		kindA, kindB := randomBlockKinds()

		var state generators.State
		state = generators.NewPrompts(
			"",
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
		var err error
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

		fmt.Printf("ping ok: model emitted the required blocks (%q, %q)\n", kindA, kindB)
	},
}
