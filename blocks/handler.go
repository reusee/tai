package blocks

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/reusee/tai/generators"
	"github.com/reusee/tai/nets"
)

const TheoryOfBlockBindings = `
Block bindings couple a block kind's system prompt with its processing logic,
ensuring that any block kind taught to the model via the prompt always has a
matching processor. This eliminates the failure mode where a block kind is
introduced in the system prompt but no code processes the model's output,
causing emitted blocks to be silently ignored.

A single BlockBindings list is shared between the prompt assembler and the
output processor. Adding a binding in one place automatically wires both the
prompt section and the processing function. Bindings without a Process function
declare their ProcessingPath to document where the block is handled (e.g.,
change blocks by applyChangeBlocks, summary blocks by runPhaseWithRetry, finish
blocks as informational). A binding with neither Process nor ProcessingPath is a
configuration error, caught by Validate, indicating an unprocessed block kind
that would cause emitted blocks to be silently ignored.

The mechanism is the integrity guarantee: it makes the coupling between prompt
and processing explicit and machine-checkable rather than implicit and
human-maintained.
`

// BlockBinding couples a block kind's system prompt with its processing logic.
type BlockBinding struct {
	// Kind is the block kind name (e.g., "change", "shell", "continue").
	Kind string
	// PromptSection is the system prompt text that teaches the model how
	// to emit this block kind. Empty if the prompt is assembled elsewhere
	// or no prompt is needed.
	PromptSection string
	// Process extracts and handles blocks of this kind from the parser
	// state in the main generation loop. If nil, ProcessingPath must
	// describe where the block is processed instead.
	Process BlockProcessFunc
	// ProcessingPath documents where blocks of this kind are processed
	// when Process is nil (e.g., "applyChangeBlocks", "runPhaseWithRetry",
	// "informational"). A non-empty ProcessingPath with a nil Process
	// declares that the block is handled by specialized logic outside the
	// binding loop or is intentionally unprocessed. An empty ProcessingPath
	// with a nil Process is a configuration error.
	ProcessingPath string
	// MaxRounds limits the number of consecutive rounds this binding can
	// trigger via Continue=true. 0 means no limit. Used to prevent infinite
	// loops (e.g., request-context blocks that keep requesting more context).
	MaxRounds int
}

// BlockProcessFunc processes blocks of a specific kind from the parser state
// in the main generation loop.
type BlockProcessFunc func(ctx context.Context, pctx *ProcessContext) ProcessResult

// ProcessContext bundles all dependencies a BlockProcessFunc may need.
type ProcessContext struct {
	ParserState *ParserState
	State       generators.State
	Root        *os.Root
	HttpClient  nets.HTTPClient
}

// ProcessResult holds the outcome of processing blocks of a single kind.
type ProcessResult struct {
	// ParserState is the new parser state with consumed blocks removed.
	ParserState *ParserState
	// State is the updated generators state (if the handler appended content
	// directly, e.g., request-context appends fetched resources to state).
	State generators.State
	// Parts are user parts to append to the state, triggering a new round.
	Parts []generators.Part
	// Continue indicates whether a new generation round should be triggered
	// immediately, stopping further binding processing in the current round.
	Continue bool
	// Err is the error encountered during processing, if any.
	Err error
}

// BlockBindings is an ordered collection of BlockBinding.
type BlockBindings []BlockBinding

// PromptSections returns the concatenated system prompt sections from all
// bindings that have a non-empty PromptSection, in registration order.
func (b BlockBindings) PromptSections() string {
	var sb strings.Builder
	for _, binding := range b {
		if binding.PromptSection != "" {
			sb.WriteString(binding.PromptSection)
			sb.WriteString("\n")
		}
	}
	return sb.String()
}

// Processable returns the subset of bindings that have a Process function,
// in registration order. These are processed in the main generation loop.
func (b BlockBindings) Processable() []BlockBinding {
	var result []BlockBinding
	for _, binding := range b {
		if binding.Process != nil {
			result = append(result, binding)
		}
	}
	return result
}

// Validate returns an error if any binding has neither a Process function
// nor a ProcessingPath, indicating an unprocessed block kind that would
// cause emitted blocks to be silently ignored.
func (b BlockBindings) Validate() error {
	for _, binding := range b {
		if binding.Process == nil && binding.ProcessingPath == "" {
			return fmt.Errorf("block kind %q has no Process function and no ProcessingPath; "+
				"it would be silently ignored if emitted by the model", binding.Kind)
		}
	}
	return nil
}

const TheoryOfCommonBlockBindings = `
CommonBlockBindings returns the block kinds shared across all generation
commands: shell (conditional on the shell flag) and continue. These are
generic, side-effect-free block kinds that any generation pipeline may use
regardless of whether it performs code modification or dynamic context
fetching. Commands that need additional block kinds (e.g., change for code
generation, request-context for dynamic context, finish and summary for
round statistics) prepend or append their specific bindings to this common
set. The common bindings are constructed once and reused by both the ai
command (via AIBlockBindings) and the codes module (via CodesBlockBindings),
ensuring that shell and continue blocks are consistently configured across
all generation pipelines and eliminating the duplicate binding construction
that previously existed in each module.
`

// CommonBlockBindings returns the block bindings shared across all generation
// commands: shell (conditional on the shell flag) and continue. Commands that
// need additional block kinds prepend or append their specific bindings to
// this common set.
// See TheoryOfCommonBlockBindings.
func CommonBlockBindings(shell bool) BlockBindings {
	var bindings BlockBindings
	if shell {
		bindings = append(bindings, BlockBinding{
			Kind:          "shell",
			PromptSection: ShellBlockSystemPrompt,
			Process: func(ctx context.Context, pctx *ProcessContext) ProcessResult {
				parts, newPs, err := ProcessShellBlocks(pctx.ParserState)
				return ProcessResult{ParserState: newPs, Parts: parts, Err: err}
			},
		})
	}
	bindings = append(bindings, BlockBinding{
		Kind:          "continue",
		PromptSection: ContinueBlockSystemPrompt,
		Process: func(ctx context.Context, pctx *ProcessContext) ProcessResult {
			parts, newPs := ProcessContinueBlocks(pctx.ParserState)
			return ProcessResult{ParserState: newPs, Parts: parts}
		},
	})
	return bindings
}
