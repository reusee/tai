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

	"github.com/reusee/tai/apps"
	"github.com/reusee/tai/blocks"
	"github.com/reusee/tai/flags"
	"github.com/reusee/tai/generators"
	"github.com/reusee/tai/modes"
	"github.com/reusee/tai/pipeline"
)

const TheoryOfPingCommand = `
The "ping" subcommand tests whether a model is reachable, responds, and can
perform the capability test the tai pipeline depends on. The test follows the
generator's protocol: the spec's Type field is the only protocol selector
(see TheoryOfSpec), the value "systemone" in any of its spellings selects the
selection test, and every other generator speaks the text/block protocol and
runs the block test. See isSystemOneSpec.

The block test (text protocol): the command randomly chooses three block
specs — each a kind, one to three parameter pairs, and an exact single-line
body — asks the model to emit exactly one block of each kind, in the listed
order, carrying exactly the listed parameter pairs in its opening header and
exactly the listed body, and validates the parsed output after generation
positionally: block i must match spec i in kind, attributes, and body, and no
extra block may appear. One random parameter value in each run is replaced
with a tricky value that exercises the header parser's percent-decoding
(embedded quotes, backslashes, tabs, or newlines); the prompt shows one valid
encoding and validation compares decoded values, so any equivalent encoding
passes. The random specs — produced by RandomPingBlocks from short lowercase
letter strings — are unknown before each run, so a correct result
demonstrates genuine instruction following, format ability, URI header
parameter ability, percent-encoding ability, and body fidelity rather than
pattern memory. The block format prompt requires a distinct delimiter per
block: blocks sharing a delimiter would be mis-parsed as nested (see
blocks.TheoryOfNestedBlockParsing), so identical delimiters cannot pass the
test.

The selection test (System One protocol): the protocol streams no text and
emits no blocks — its whole output is the set of declared functions whose
noul answers reach the selection threshold (see generators.TheoryOfSystemOne)
— so the block test cannot apply. Ping asks its capability question instead:
RandomPingFunctions declares three probe functions, one target and two
decoys, and the user message asks for exactly the target's capability. The
probes share one verb and take distinct objects, so a model that selects by
the shared verb alone selects every probe and fails, while the target is
unambiguous to a model that reads each description whole. The probes enter
the state through generators.NewFuncMap, each with a marker function, so a
selected function also executes and its result is recorded — the same round
trip the pipeline gives a selected function. Validation requires exactly the
target among the model's selections: no selection (a model that rejects
everything) and any extra selection (a model that accepts everything) both
fail. The function names, the verb, which object the request names, and the
target's position are random per run, so a correct result demonstrates
reading the declarations and discriminating between near-neighbors rather
than matching a pattern.

Both tests run one generation through the unified loop with no components, so
no block kind is consumed and the parsed blocks stay in
pipeline.Result.RemainingBlocks. The block-format instructions live in the
block test's system prompt (blocks.BlockFormatSystemPrompt); the user message
(pingBlockPrompt) states only the test requirements — the kinds, their exact
parameter pairs, and their exact bodies — without repeating the format
description. The system prompt of either test also carries the user-configured
extra system prompts (extra_system_prompt and family_extra_system_prompt), so
ping honors the same configuration as the other generation commands. A
validation failure reports the observed outcome through pingFailed and exits
with status 1, making the command scriptable; on success the verdict goes to
the command Output writer after the streamed output.

The command requires a model to be specified via -model; without it, resolving
the default generator fails, making the dependency on an explicit model
selection explicit. Reasoning thoughts stream to stdout by default; the
-no-thoughts flag (flags.Thoughts) suppresses them, matching the next
command's output behavior.
`

// PingBlockSpec describes one required block for the ping test: the block
// kind, the exact parameter pairs the block's opening header must carry,
// and the exact body text the block must contain. Validation compares the
// parsed attributes by decoded value and the trimmed body exactly, so a
// correct emission demonstrates the model can use the URI header format
// with percent-encoded query parameters — including escape-sensitive
// values — and reproduce a verbatim body, not only a bare kind.
// See TheoryOfPingCommand.
type PingBlockSpec struct {
	Kind       string
	Attributes map[string]string
	Body       string
}

// PingFunctionSpec describes one probe function of the selection test: the
// function's random name, the description the model reads, and whether the
// function is the target the request message asks for. Exactly one probe of
// a set is the target. See TheoryOfPingCommand.
type PingFunctionSpec struct {
	Name        string
	Description string
	Target      bool
}

// RandomPingFunctions returns the probe functions the selection test
// declares, chosen at random on each run: the probes share one random verb
// and take distinct objects, exactly one of them is the target, and the
// request message names the target's object. The function names are random
// lowercase letter strings the model must reproduce exactly in its answers,
// so a correct result cannot come from pattern memory. See
// TheoryOfPingCommand.
type RandomPingFunctions func() []PingFunctionSpec

// RandomPingBlocks returns the three block specs the ping command asks the
// model to emit, chosen at random on each run. Each spec pairs a short
// lowercase kind name and an exact single-line body with one to three
// parameter pairs whose names and values are random lowercase letter
// strings, so the model can reproduce them exactly yet cannot match the
// request from pattern memory; one random parameter value in the run is
// replaced with a tricky value that exercises the header parser's
// percent-decoding. A correct emission demonstrates instruction-following,
// block-format, URI header parameter, percent-encoding, and body-fidelity
// ability. See TheoryOfPingCommand.
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
		// run exercises the header parser's percent-decoding. The prompt
		// renders the value in one valid encoded form; validation compares
		// decoded values. See TheoryOfPingCommand.
		target := &specs[rand.IntN(len(specs))]
		names := slices.Collect(maps.Keys(target.Attributes))
		target.Attributes[names[rand.IntN(len(names))]] = pingTrickyValues[rand.IntN(len(pingTrickyValues))]
		return specs
	}
}

func (Module) RandomPingFunctions() RandomPingFunctions {
	verbs := []string{"summarize", "translate", "redact", "archive"}
	objects := []string{"report", "invoice", "contract"}
	return func() []PingFunctionSpec {
		verb := verbs[rand.IntN(len(verbs))]
		order := slices.Clone(objects)
		rand.Shuffle(len(order), func(i, j int) {
			order[i], order[j] = order[j], order[i]
		})
		target := rand.IntN(len(order))
		names := make(map[string]bool, len(order))
		specs := make([]PingFunctionSpec, 0, len(order))
		for i, object := range order {
			name := randomPingWord(4, 8)
			for names[name] {
				name = randomPingWord(4, 8)
			}
			names[name] = true
			specs = append(specs, PingFunctionSpec{
				Name:        name,
				Description: fmt.Sprintf("%s the %s", verb, object),
				Target:      i == target,
			})
		}
		return specs
	}
}

// randomPingWord returns a random lowercase letter string whose length is
// between min and max, inclusive. Plain letters only: kind names,
// parameter names, and parameter values must be reproducible exactly, and
// letters avoid percent-encoding complexity in the query values.
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
// parser's percent-decoding: embedded double quotes, an apostrophe, a
// backslash, a tab, a newline, and multi-word spaces. Each entry is the
// DECODED value; the prompt renders it via percentEncodePingValue and the
// model may answer with any equivalent encoding. See TheoryOfPingCommand
// and blocks.TheoryOfHeaderTokenizing.
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
- Each block's opening header must carry EXACTLY the parameter pairs listed for its kind: same names, same decoded values. The listed values show one valid percent-encoding; any equivalent encoding that decodes to the same value is accepted.
- The body of each block must be EXACTLY the listed body text, and nothing else.
- The blocks must appear in the listed order.
- Emit only the required blocks and nothing else: no prose, no explanations, no additional blocks.`)
	return b.String()
}

// formatPingBlockSpec renders one required block as
// kind:?k=v&k2=v2 — the URI header form with the parameter pairs
// percent-encoded and sorted by name — for deterministic prompts and
// error messages. Values are rendered in the percent-encoded form.
func formatPingBlockSpec(spec PingBlockSpec) string {
	if len(spec.Attributes) == 0 {
		return spec.Kind
	}
	pairs := make([]string, 0, len(spec.Attributes))
	for _, name := range slices.Sorted(maps.Keys(spec.Attributes)) {
		pairs = append(pairs, name+"="+percentEncodePingValue(spec.Attributes[name]))
	}
	return fmt.Sprintf("%s:?%s", spec.Kind, strings.Join(pairs, "&"))
}

// percentEncodePingValue renders a decoded parameter value in the
// percent-encoded form of the URI header format, encoding every byte
// outside the RFC 3986 unreserved set (A-Z a-z 0-9 - . _ ~) as %XX. The
// prompt shows this form as one valid encoding; validation compares
// decoded values, so any equivalent encoding also passes.
func percentEncodePingValue(value string) string {
	const unreserved = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-._~"
	var b strings.Builder
	for i := 0; i < len(value); i++ {
		c := value[i]
		if strings.IndexByte(unreserved, c) >= 0 {
			b.WriteByte(c)
		} else {
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
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

// isSystemOneSpec reports whether a generator speaks the System One
// protocol: the spec's Type field is the only protocol selector (see
// TheoryOfSpec), so the test reads it. The spellings and the case-insensitive
// comparison mirror the dispatch in generators.Module.GetGenerator. See
// TheoryOfPingCommand.
func isSystemOneSpec(spec generators.Spec) bool {
	switch strings.ToLower(spec.Type) {
	case "systemone", "system-one", "system_one":
		return true
	}
	return false
}

// pingSelectionSystemPrompt is the system prompt of the selection test. It
// states the one rule the model's judgments must follow: a function is
// selected only when its description states what the user message asks for.
// See TheoryOfPingCommand.
const pingSelectionSystemPrompt = `This is a function-selection test. Every question carries one declared function with its name and description, and asks whether that function handles the latest user message. Judge each question on its own: a function is selected only when its description states what the user message asks for, and no function handles a message that asks for something else.`

// pingSystemPrompt renders a ping system prompt: the protocol's base prompt
// followed by the user-configured extra system prompts (extra_system_prompt
// and family_extra_system_prompt), so ping honors the same configuration as
// the other generation commands. Each section is trimmed and sections are
// joined with a blank line; an empty section contributes nothing, so equal
// configurations produce byte-identical prompts. See TheoryOfPingCommand and
// generators.TheoryOfContentUnitSeparation.
func pingSystemPrompt(
	base string,
	extra flags.ExtraSystemPrompt,
	familyExtra flags.FamilyExtraSystemPrompt,
	modelFamily generators.ModelFamily,
) string {
	var b strings.Builder
	writeSection := func(section string) {
		section = strings.TrimRight(section, " \t\n\r")
		if section == "" {
			return
		}
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString(section)
	}
	writeSection(base)
	for _, prompt := range extra {
		writeSection(prompt)
	}
	for _, prompt := range familyExtra[string(modelFamily)] {
		writeSection(prompt)
	}
	b.WriteString("\n")
	return b.String()
}

// pingFunctionRequest renders the selection test's request message: the
// sentence that asks for exactly the target probe's capability. See
// TheoryOfPingCommand.
func pingFunctionRequest(specs []PingFunctionSpec) string {
	for _, spec := range specs {
		if spec.Target {
			return fmt.Sprintf("Please %s.", spec.Description)
		}
	}
	return ""
}

// pingSelectionState builds the selection test's initial state: the request
// message as the user content, and the probe functions declared through
// generators.NewFuncMap with a marker function each, so a selected function
// also executes and its result is recorded — the same round trip the pipeline
// gives a selected function (see generators.FuncMap). See TheoryOfPingCommand.
func pingSelectionState(systemPrompt string, specs []PingFunctionSpec) generators.State {
	state := generators.NewPrompts(systemPrompt, []*generators.Content{
		{
			Role:  generators.RoleUser,
			Parts: []generators.Part{generators.Text(pingFunctionRequest(specs))},
		},
	})
	funcs := make([]*generators.Function, 0, len(specs))
	for _, spec := range specs {
		funcs = append(funcs, &generators.Function{
			Decl: generators.FuncDecl{
				Name:        spec.Name,
				Description: spec.Description,
			},
			Func: func(map[string]any) (map[string]any, error) {
				return map[string]any{"probe": spec.Name}, nil
			},
		})
	}
	return generators.NewFuncMap(state, funcs...)
}

// pingProbeTarget returns the single target probe of a set. A set without
// exactly one target is a programming fault, reported as an error so a
// malformed set cannot pass validation vacuously. See TheoryOfPingCommand.
func pingProbeTarget(specs []PingFunctionSpec) (PingFunctionSpec, error) {
	var (
		ret   PingFunctionSpec
		count int
	)
	for _, spec := range specs {
		if spec.Target {
			ret = spec
			count++
		}
	}
	if count != 1 {
		return PingFunctionSpec{}, fmt.Errorf("the probe set carries %d target function(s), want exactly 1", count)
	}
	return ret, nil
}

// validatePingSelection checks the selection test's outcome: the run's
// selections must be exactly the target probe function. The selections are
// the FuncCall parts the System One generator appended for the functions
// whose noul answers reached the selection threshold (see
// generators.TheoryOfSystemOne). No selection — the model rejected every
// function — and any additional selection — the model accepted a decoy —
// both fail, so a blanket answer in either direction cannot pass. See
// TheoryOfPingCommand.
func validatePingSelection(finalState generators.State, specs []PingFunctionSpec) error {
	target, err := pingProbeTarget(specs)
	if err != nil {
		return err
	}
	var selected []string
	if finalState != nil {
		for content := range finalState.Contents() {
			for _, part := range content.Parts {
				if call, ok := part.(generators.FuncCall); ok {
					selected = append(selected, call.Name)
				}
			}
		}
	}
	slices.Sort(selected)
	if len(selected) == 1 && selected[0] == target.Name {
		return nil
	}
	if len(selected) == 0 {
		return fmt.Errorf("the model selected no function; the target probe %q (%s) must be selected",
			target.Name, target.Description)
	}
	return fmt.Errorf("the model selected %s; exactly the target probe %q (%s) must be selected",
		strings.Join(selected, ", "), target.Name, target.Description)
}

// pingFailed reports a failed test and exits with status 1, so a ping run is
// scriptable. The malformed-block note is printed when the block test saw
// unparseable output. See TheoryOfPingCommand.
func pingFailed(err error, parseErrors []*blocks.BlockParseError) {
	fmt.Fprintf(os.Stderr, "ping failed: %v\n", err)
	if len(parseErrors) > 0 {
		fmt.Fprintf(os.Stderr, "ping: %d malformed block(s) were detected during parsing, e.g. kind %q with boundary %q\n",
			len(parseErrors), parseErrors[0].BlockKind, parseErrors[0].Boundary)
	}
	os.Exit(1)
}

// pingRunOptions builds the run options shared by both ping tests: one
// generation through the unified loop with no components, so no block kind is
// consumed and the parsed blocks stay in the result. The loop handles
// ParserState wrapping, its recording session, and the TUI's observers. See
// TheoryOfPingCommand and pipeline.TheoryOfLoops.
func pingRunOptions(
	generator generators.Generator,
	buildGenerate generators.BuildGenerate,
	state generators.State,
) pipeline.RunOptions {
	return pipeline.RunOptions{
		Generator:    generator,
		InitialState: state,
		Components:   nil,
		Command:      "ping",
		PhaseBuilder: func(g generators.Generator) generators.Phase {
			return buildGenerate(g, nil)(nil)
		},
	}
}

// runPingLoop drives the unified generation loop to its end and returns the
// filled result: the loop's terminal error arrives with its final yield and
// ends the iterator, so the first non-nil error is the run's outcome and the
// caller must stop — pipeline.Run documents that contract. See
// pipeline.TheoryOfLoops.
func runPingLoop(
	ctx context.Context,
	loopRun pipeline.Run,
	opts pipeline.RunOptions,
) (pipeline.Result, error) {
	var result pipeline.Result
	for _, err := range loopRun(ctx, opts, &result) {
		if err != nil {
			return result, err
		}
	}
	return result, nil
}

// runBlockPing runs the block test of the text protocol: one generation
// through the unified loop, then the positional validation of the parsed
// blocks. See TheoryOfPingCommand.
func runBlockPing(
	ctx context.Context,
	output Output,
	generator generators.Generator,
	buildGenerate generators.BuildGenerate,
	loopRun pipeline.Run,
	specs []PingBlockSpec,
	systemPrompt string,
	showThoughts bool,
) apps.MainErr {
	// The state variable is declared with the interface type: the state
	// layers below are concrete types (Prompts, Output), and only the
	// interface variable accepts each layer in turn.
	var state generators.State
	state = generators.NewPrompts(systemPrompt, []*generators.Content{
		{
			Role:  generators.RoleUser,
			Parts: []generators.Part{generators.Text(pingBlockPrompt(specs))},
		},
	})
	state = generators.NewOutput(state, os.Stdout, showThoughts)

	result, err := runPingLoop(ctx, loopRun, pingRunOptions(generator, buildGenerate, state))
	if err != nil {
		return err
	}

	if err := validatePingBlocks(result, specs); err != nil {
		pingFailed(err, result.ParseErrors)
	}

	rendered := make([]string, len(specs))
	for i, spec := range specs {
		rendered[i] = formatPingBlockSpec(spec)
	}
	fmt.Fprintf(output, "ping ok: model emitted %d blocks in order with exact parameters and bodies (%s)\n",
		len(specs), strings.Join(rendered, ", "))

	return nil
}

// runSystemOnePing runs the selection test of the System One protocol: one
// generation through the unified loop, then the check that the model selected
// exactly the target probe function. The tools-disabled pre-flight fails
// before any request is sent: such a generator carries no questions, so the
// generator's own error would name the wrong cause. See TheoryOfPingCommand.
func runSystemOnePing(
	ctx context.Context,
	output Output,
	generator generators.Generator,
	buildGenerate generators.BuildGenerate,
	loopRun pipeline.Run,
	specs []PingFunctionSpec,
	systemPrompt string,
	showThoughts bool,
) apps.MainErr {
	if spec := generator.Spec(); spec.DisableTools != nil && *spec.DisableTools {
		return fmt.Errorf(
			"the generator %q disables tools (disable_tools), so a System One request carries no function questions; enable tools to run the selection test",
			spec.Model,
		)
	}

	target, err := pingProbeTarget(specs)
	if err != nil {
		return err
	}

	// The state variable carries the interface type so both state layers
	// (FuncMap and Output) assign into it.
	var state generators.State
	state = pingSelectionState(systemPrompt, specs)
	state = generators.NewOutput(state, os.Stdout, showThoughts)

	result, err := runPingLoop(ctx, loopRun, pingRunOptions(generator, buildGenerate, state))
	if err != nil {
		return err
	}

	if err := validatePingSelection(result.FinalState, specs); err != nil {
		pingFailed(err, nil)
	}

	fmt.Fprintf(output, "ping ok: model selected exactly the target function %q for the request %q\n",
		target.Name, pingFunctionRequest(specs))

	return nil
}

var PingCommand = apps.New("ping",
	"Test whether a model is reachable and passes its protocol's capability test",
	func(
		output Output,
		getDefaultGenerator generators.GetDefaultGenerator,
		buildGenerate generators.BuildGenerate,
		loopRun pipeline.Run,
		randomPingBlocks RandomPingBlocks,
		randomPingFunctions RandomPingFunctions,
		extra flags.ExtraSystemPrompt,
		familyExtra flags.FamilyExtraSystemPrompt,
		modelFamily generators.ModelFamily,
		flagThoughts flags.Thoughts,
	) apps.MainErr {
		ctx := context.Background()

		generator, err := getDefaultGenerator()
		if err != nil {
			return err
		}

		showThoughts := true
		if flagThoughts.Value != nil {
			showThoughts = *flagThoughts.Value
		}

		// The test follows the generator's protocol. The System One
		// protocol streams no text and emits no blocks, so the block test
		// cannot apply there and the selection test runs instead. See
		// TheoryOfPingCommand.
		if isSystemOneSpec(generator.Spec()) {
			return runSystemOnePing(
				ctx,
				output,
				generator,
				buildGenerate,
				loopRun,
				randomPingFunctions(),
				pingSystemPrompt(pingSelectionSystemPrompt, extra, familyExtra, modelFamily),
				showThoughts,
			)
		}

		return runBlockPing(
			ctx,
			output,
			generator,
			buildGenerate,
			loopRun,
			randomPingBlocks(),
			pingSystemPrompt(blocks.BlockFormatSystemPrompt, extra, familyExtra, modelFamily),
			showThoughts,
		)
	},
	modes.ForProduction(),
)
