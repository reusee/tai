package main

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"math/rand/v2"
	"os"
	"slices"
	"strings"

	"github.com/reusee/tai/blocks"
	"github.com/reusee/tai/flags"
	"github.com/reusee/tai/generators"
	"github.com/reusee/tai/modes"
	"github.com/reusee/tai/phases"
	"github.com/reusee/tai/pipeline"
	"github.com/reusee/tai/records"
)

const TheoryOfPingCommand = `
The "ping" subcommand tests whether a model is reachable and responding, and
whether it can emit the heredoc blocks the tai tooling relies on. The command
randomly chooses three block specs — each a kind, one to three parameter
pairs, and an exact single-line body — asks the model to emit exactly one
block of each kind, in the listed order, carrying exactly the listed
parameter pairs in its opening header and exactly the listed body, and
validates the parsed output after generation positionally: block i must
match spec i in kind, attributes, and body, and no extra block may appear.
One random parameter value in each run is replaced with a tricky value that
exercises the header parser's escape handling (embedded quotes, backslashes,
tabs, or newlines); the prompt shows one valid escaping and validation
compares decoded values, so any equivalent escaping passes. The random
specs — produced by RandomPingBlocks from short lowercase letter strings —
are unknown before each run, so a correct result demonstrates genuine
instruction following, format ability, function-call header parameter
ability, escape-sequence ability, and body fidelity rather than pattern
memory. The block-format
instructions live in the system prompt (blocks.BlockFormatSystemPrompt); the
user message (pingBlockPrompt) states only the test requirements — the kinds,
their exact parameter pairs, and their exact bodies — without repeating the
format description. The system prompt also carries the
user-configured extra system prompts (extra_system_prompt and
family_extra_system_prompt), so ping honors the same configuration as the
other generation commands. The block format prompt requires a distinct
delimiter per block: blocks sharing a delimiter would be mis-parsed as
nested (see blocks.TheoryOfNestedBlockParsing), so identical delimiters
cannot pass the test. Validation reads the blocks collected in
pipeline.Result.RemainingBlocks: ping runs without components, so no block kind
is consumed. A validation failure prints the observed outcome and exits with
status 1, making the command scriptable; on success the verdict is printed
to the command Output writer after the streamed output.

The command requires a model to be specified via -model; without it, resolving
the default generator fails, making the dependency on an explicit model
selection explicit. The command performs a single generation round with no
chat loop and no file context; its system prompt carries the block-format
prompt and the user-configured extra system prompts.
`

// PingBlockSpec describes one required block for the ping test: the block
// kind, the exact parameter pairs the block's opening header must carry,
// and the exact body text the block must contain. Validation compares the
// parsed attributes by decoded value and the trimmed body exactly, so a
// correct emission demonstrates the model can use the function-call
// header format with named parameters — including escape-sequence values
// — and reproduce a verbatim body, not only a bare kind.
// See TheoryOfPingCommand.
type PingBlockSpec struct {
	Kind       string
	Attributes map[string]string
	Body       string
}

// RandomPingBlocks returns the three block specs the ping command asks the
// model to emit, chosen at random on each run. Each spec pairs a short
// lowercase kind name and an exact single-line body with one to three
// parameter pairs whose names and values are random lowercase letter
// strings, so the model can reproduce them exactly yet cannot match the
// request from pattern memory; one random parameter value in the run is
// replaced with a tricky value that exercises the header parser's escape
// handling. A correct emission demonstrates instruction-following,
// block-format, function-call header parameter, escape-sequence, and
// body-fidelity ability. See TheoryOfPingCommand.
type RandomPingBlocks func() []PingBlockSpec

func (Module) RandomPingBlocks() RandomPingBlocks {
	return func() []PingBlockSpec {
		spec := func() PingBlockSpec {
			spec := PingBlockSpec{
				Kind:       randomPingWord(3, 8),
				Attributes: map[string]string{},
				Body:       randomPingBody(),
			}
			pairCount := rand.IntN(3) + 1
			for len(spec.Attributes) < pairCount {
				spec.Attributes[randomPingWord(3, 6)] = randomPingWord(3, 8)
			}
			return spec
		}
		specs := []PingBlockSpec{spec(), spec(), spec()}
		for specs[1].Kind == specs[0].Kind {
			specs[1] = spec()
		}
		for specs[2].Kind == specs[0].Kind || specs[2].Kind == specs[1].Kind {
			specs[2] = spec()
		}
		// Replace one random parameter value with a tricky value so every
		// run exercises the header parser's escape handling. The prompt
		// renders the value in one valid escaped form; validation compares
		// decoded values. See TheoryOfPingCommand.
		target := &specs[rand.IntN(len(specs))]
		names := slices.Collect(maps.Keys(target.Attributes))
		target.Attributes[names[rand.IntN(len(names))]] = pingTrickyValues[rand.IntN(len(pingTrickyValues))]
		return specs
	}
}

// randomPingWord returns a random lowercase letter string whose length is
// between min and max, inclusive. Plain letters only: kind names,
// parameter names, and parameter values must be reproducible exactly, and
// letters avoid escape-sequence complexity in the quoted header values.
// See TheoryOfPingCommand.
func randomPingWord(min, max int) string {
	const letters = "abcdefghijklmnopqrstuvwxyz"
	n := rand.IntN(max-min+1) + min
	b := make([]byte, n)
	for i := range b {
		b[i] = letters[rand.IntN(len(letters))]
	}
	return string(b)
}

// randomPingBody returns the exact single-line body text one required
// block must carry: two or three random lowercase words. Like the other
// random pieces, the body is unknown before each run yet exactly
// reproducible, so a verbatim emission demonstrates body fidelity rather
// than pattern memory. See TheoryOfPingCommand.
func randomPingBody() string {
	words := make([]string, 0, 3)
	for range rand.IntN(2) + 2 {
		words = append(words, randomPingWord(3, 8))
	}
	return strings.Join(words, " ")
}

// pingTrickyValues lists parameter values that exercise the header
// parser's escape handling: embedded double quotes, an apostrophe, a
// backslash, a tab, a newline, and multi-word spaces. Each entry is the
// DECODED value; the prompt renders it via escapePingValue and the model
// may answer with any equivalent escaping. See TheoryOfPingCommand and
// blocks.TheoryOfHeaderTokenizing.
var pingTrickyValues = []string{
	`say "hi"`,
	`it's fine`,
	`back\slash`,
	"col1\tcol2",
	"line1\nline2",
	`two words`,
}

func pingBlockPrompt(specs []PingBlockSpec) string {
	var b strings.Builder
	fmt.Fprintf(&b, "This is a block-generation test. Emit exactly %d blocks.\n\n", len(specs))
	fmt.Fprintf(&b, "The %d required blocks, in this exact order (each used exactly once):\n", len(specs))
	for i, spec := range specs {
		fmt.Fprintf(&b, "%d. %s\n", i+1, formatPingBlockSpec(spec))
		fmt.Fprintf(&b, "   exact body: %s\n", spec.Body)
	}
	b.WriteString(`
Rules:
- Each block's opening header must carry EXACTLY the parameter pairs listed for its kind: same names, same values. The listed values show one valid escaping; any equivalent escaping that decodes to the same value is accepted.
- The body of each block must be EXACTLY the listed body text, and nothing else.
- The blocks must appear in the listed order.
- Emit only the required blocks and nothing else: no prose, no explanations, no additional blocks.`)
	return b.String()
}

// formatPingBlockSpec renders one required block as kind(name="value", ...)
// with the parameter pairs sorted by name, for deterministic prompts and
// error messages. Values are rendered in the double-quoted escaped form.
func formatPingBlockSpec(spec PingBlockSpec) string {
	pairs := make([]string, 0, len(spec.Attributes))
	for _, name := range slices.Sorted(maps.Keys(spec.Attributes)) {
		pairs = append(pairs, name+"="+escapePingValue(spec.Attributes[name]))
	}
	return fmt.Sprintf("%s(%s)", spec.Kind, strings.Join(pairs, ", "))
}

// escapePingValue renders a decoded parameter value in the double-quoted
// escaped form of the block header format, escaping exactly the
// characters blocks.TheoryOfHeaderTokenizing defines. The prompt shows
// this form as one valid encoding; validation compares decoded values, so
// any equivalent escaping (e.g., single quotes around a value containing
// a double quote) also passes.
func escapePingValue(value string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range value {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\t':
			b.WriteString(`\t`)
		case '\r':
			b.WriteString(`\r`)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// validatePingBlocks checks that the model emitted exactly one block per
// spec, in the listed order, each with exactly the required parameter
// pairs and exactly the required body, and no other blocks. Attribute
// comparison is order-independent and value-decoded: the header parser
// stores parameters in a map and decodes escape sequences, so only the
// decoded name-to-value pairs are significant — any equivalent escaping
// passes. Only parsed blocks are inspected: prose around the blocks is
// discarded by ParserState and never appears in
// pipeline.Result.RemainingBlocks. See TheoryOfPingCommand.
func validatePingBlocks(result pipeline.Result, specs []PingBlockSpec) error {
	emitted := result.RemainingBlocks
	var problems []string
	if len(emitted) != len(specs) {
		problems = append(problems, fmt.Sprintf("expected exactly %d block(s), got %d", len(specs), len(emitted)))
	}
	for i := 0; i < min(len(emitted), len(specs)); i++ {
		spec := specs[i]
		block := emitted[i]
		if block.Kind != spec.Kind {
			problems = append(problems, fmt.Sprintf("block %d: expected kind %q, got %q", i+1, spec.Kind, block.Kind))
			continue
		}
		if !pingAttributesEqual(block.Attributes, spec.Attributes) {
			problems = append(problems, fmt.Sprintf("block %d: parameter mismatch: expected %s, got %s",
				i+1,
				formatPingBlockSpec(spec),
				formatPingBlockSpec(PingBlockSpec{Kind: block.Kind, Attributes: block.Attributes})))
		}
		if body := strings.TrimSpace(block.Body); body != spec.Body {
			problems = append(problems, fmt.Sprintf("block %d: body mismatch: expected %q, got %q", i+1, spec.Body, body))
		}
	}
	if len(problems) == 0 {
		return nil
	}
	return errors.New(strings.Join(problems, "; "))
}

// pingAttributesEqual reports whether two attribute maps hold exactly the
// same name-to-value pairs.
func pingAttributesEqual(got, want map[string]string) bool {
	if len(got) != len(want) {
		return false
	}
	for name, value := range want {
		gotValue, ok := got[name]
		if !ok || gotValue != value {
			return false
		}
	}
	return true
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
		loopRun pipeline.Run,
		randomPingBlocks RandomPingBlocks,
		extra flags.ExtraSystemPrompt,
		familyExtra flags.FamilyExtraSystemPrompt,
		modelFamily generators.ModelFamily,
	) {
		ctx := context.Background()

		generator, err := getDefaultGenerator()
		ce(err)

		// Three block specs — each a kind, one to three random parameter
		// pairs, and an exact body — are chosen at random on every run so
		// the model cannot match the request from pattern memory; a
		// correct emission demonstrates availability, block-generation
		// ability, function-call header parameter ability (including
		// escape sequences), and verbatim body fidelity.
		// See TheoryOfPingCommand.
		specs := randomPingBlocks()

		// The block-format instructions live in the system prompt
		// (blocks.BlockFormatSystemPrompt), so the user message
		// (pingBlockPrompt) states only the test requirements — the
		// kinds, their exact parameter pairs, and their exact bodies —
		// without repeating the format description. The system prompt
		// also carries the user-configured extra system prompts
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
						generators.Text(pingBlockPrompt(specs)),
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
		// error, if any. See pipeline.TheoryOfLoops and TheoryOfTUI.
		var result pipeline.Result
		for e := range loopRun(ctx, pipeline.RunOptions{
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

		// Validate the emitted blocks after generation positionally:
		// block i must match spec i in kind, attributes (compared by
		// decoded value, so any equivalent escaping passes), and body,
		// and no extra block may appear. A validation failure exits
		// with status 1 so the command is scriptable: availability
		// alone is not enough, the model must also emit the required
		// blocks in the required format.
		// See TheoryOfPingCommand.
		if err := validatePingBlocks(result, specs); err != nil {
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
		rendered := make([]string, len(specs))
		for i, spec := range specs {
			rendered[i] = formatPingBlockSpec(spec)
		}
		fmt.Fprintf(output, "ping ok: model emitted %d blocks in order with exact parameters and bodies (%s)\n",
			len(specs), strings.Join(rendered, ", "))
	},
}
