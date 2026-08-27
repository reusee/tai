package components

import (
	"context"
	"os"
	"strings"

	"github.com/reusee/tai/blocks"
	"github.com/reusee/tai/generators"
	"github.com/reusee/tai/nets"
)

const TheoryOfComponents = `
Component is the unified extension mechanism for the generation pipeline. It
generalizes beyond block processing: a Component can contribute a system prompt
section, user prompt parts, define a block kind for parsing, process blocks of
that kind, or any combination thereof.

A Component with a Process function is processed in the main generation loop; a
Component without one (prompt-only or informational) contributes its
PromptSection to the system prompt but is not invoked during output processing.
A Component can also contribute UserPromptParts, which are prepended to the
user's input similar to how PartsProvider.Parts provides context. ComponentSet is
an ordered collection of Components that provides PromptSections (concatenating
all system prompt contributions), UserPromptParts (concatenating all user prompt
parts), and Processable (returning the subset with Process functions for the
generation loop). PromptSections joins its contributions with a blank line (two
newlines) after trimming each section's trailing whitespace, so adjacent prompt
sections never stick together.

System prompt restate: the late reminder is the system prompt itself.
SystemPromptRestate builds a user prompt part that repeats the full system
prompt verbatim under a short re-read instruction. Every generation command
appends it as the last user prompt part before the dynamic user input, so the
model re-reads the complete rules immediately before generating. Because the
restate is assembled from the same text as the system prompt, the two can never
drift out of sync: there is no shortened reminder to keep consistent with the
full instructions.

ProcessComponents is the shared function that iterates over Processable
components in registration order, filtering blocks by each component's Kind
and calling the component's Process function with the matching blocks. Both
the ai command (cmd/tai/ai.go) and the codes module (codes/generate.go) call
ProcessComponents with a []Block slice (collected by the BlockHandler during
generation) and the current state. The function returns remaining blocks (not
matched by any component), the updated state, combined parts, and whether any
component triggered a new generation.

The mechanism makes the coupling between prompt and processing explicit and
machine-checkable. The system prompt assembly, user prompt assembly, and output
processing loop share a single ComponentSet, ensuring that every prompt
contribution is registered, every block kind has a matching processor, and
every user prompt part is assembled through the same unified mechanism.
`

// ComponentProcessFunc processes blocks of a specific kind from the parser
// state in the main generation loop.
type ComponentProcessFunc func(ctx context.Context, pctx *ProcessContext) ProcessResult

// ProcessContext bundles all dependencies a ComponentProcessFunc may need.
type ProcessContext struct {
	// Blocks are the blocks matching this component's Kind, pre-filtered
	// by ProcessComponents.
	Blocks []blocks.Block
	// State is the current generators state. Components may modify it
	// (e.g., ingest appends fetched resources).
	State generators.State
	// Root is the filesystem root for file operations.
	Root *os.Root
	// HttpClient is the HTTP client for network operations.
	HttpClient nets.HTTPClient
}

// ProcessResult holds the outcome of processing blocks of a single kind.
type ProcessResult struct {
	// State is the updated generators state. When non-nil, the component
	// modified the state (e.g., request-context appends fetched resources),
	// and a new generation is triggered.
	State generators.State
	// Parts are user parts to append to the state, triggering a new
	// generation.
	Parts []generators.Part
	// Err is the error encountered during processing, if any.
	Err error
}

// Component is the unified extension mechanism for the generation pipeline.
// A Component can contribute a system prompt section, user prompt parts,
// define a block kind, process blocks, or any combination. A Component with
// an empty Kind is a prompt-only component that contributes its
// PromptSection to the system prompt but does not process blocks.
// See TheoryOfComponents.
type Component struct {
	// Kind is the block kind name (e.g., "change", "shell", "continue").
	// Empty for prompt-only components that contribute to the system prompt
	// but do not process blocks.
	Kind string
	// PromptSection is the system prompt text that teaches the model how
	// to use this component. Empty if no prompt is needed.
	PromptSection string
	// UserPromptParts are user prompt parts contributed by this component.
	// These are prepended to the user's input, similar to how
	// PartsProvider.Parts provides context. Unlike PromptSection which goes
	// into the system prompt, UserPromptParts goes into the user content.
	// Empty for components that contribute only to the system prompt.
	UserPromptParts []generators.Part
	// Process extracts and handles blocks of this kind from the block list
	// in the main generation loop. If nil, the block kind is either
	// prompt-only (Kind == "") or handled by specialized logic outside the
	// component loop (e.g., change blocks applied via BlockHandler during
	// streaming, summary blocks processed in runGeneration, memory
	// blocks processed post-loop).
	Process ComponentProcessFunc
}

// ComponentSet is an ordered collection of Component.
type ComponentSet []Component

// PromptSections returns the concatenated system prompt sections from all
// components that have a non-empty PromptSection, in registration order.
// Each section's trailing whitespace is trimmed and sections are joined
// with a blank line (two newlines), so adjacent prompt sections never
// stick together.
func (c ComponentSet) PromptSections() string {
	var sb strings.Builder
	for _, comp := range c {
		section := strings.TrimRight(comp.PromptSection, " \t\n\r")
		if section != "" {
			sb.WriteString(section)
			sb.WriteString("\n\n")
		}
	}
	return sb.String()
}

// UserPromptParts returns the concatenated user prompt parts from all
// components, in registration order. These are prepended to the user's
// input, similar to how PartsProvider.Parts provides context. Unlike
// PromptSections which goes into the system prompt, UserPromptParts goes
// into the user content. Components without UserPromptParts contribute
// nothing. See TheoryOfComponents.
func (c ComponentSet) UserPromptParts() []generators.Part {
	var parts []generators.Part
	for _, comp := range c {
		parts = append(parts, comp.UserPromptParts...)
	}
	return parts
}

// systemPromptRestateHeader introduces the verbatim system prompt restate in
// the user prompt: it tells the model why the text is repeated and what to do
// with it. See TheoryOfComponents.
const systemPromptRestateHeader = "[System note: The system instructions are restated verbatim below. Re-read them carefully now — every rule in them applies in full to the response you are about to generate.]\n\n"

// SystemPromptRestate returns the user prompt part that repeats the full
// system prompt verbatim under a short re-read instruction. The restate
// gives the model a second, late exposure to the complete rules — the last
// content before the dynamic user input — and because it is built from the
// same text as the system prompt, it can never drift out of sync with it.
// Callers append it after all other user prompt parts. The part ends with
// a blank line so following content starts a fresh paragraph; see
// generators.TheoryOfContentUnitSeparation. See TheoryOfComponents.
func SystemPromptRestate(systemPrompt string) generators.Text {
	prompt := strings.TrimRight(systemPrompt, " \t\n\r")
	return generators.Text(systemPromptRestateHeader + prompt + "\n\n")
}

// Processable returns the subset of components that have a Process function,
// in registration order. These are processed in the main generation loop.
func (c ComponentSet) Processable() []Component {
	var result []Component
	for _, comp := range c {
		if comp.Process != nil {
			result = append(result, comp)
		}
	}
	return result
}

// ProcessComponents iterates over processable components in registration order,
// filtering blocks by each component's Kind and calling the component's Process
// function with the matching blocks. It returns the remaining blocks (not
// matched by any component), the updated State (if any component modified it),
// combined Parts from all components, whether any component triggered a new
// generation (produced Parts or modified State), and an error if any component
// failed. There are no per-component generation limits: a component may
// trigger generations for as long as the model keeps emitting its blocks,
// and run-duration control belongs to the caller via
// pipeline.RunOptions.MaxGenerations.
//
// Both the ai command and the pipeline call this function, so the
// component processing loop is identical across all generation commands —
// only the ComponentSet and block list differ. See TheoryOfComponents.
func ProcessComponents(
	ctx context.Context,
	comps ComponentSet,
	allBlocks []blocks.Block,
	state generators.State,
	root *os.Root,
	httpClient nets.HTTPClient,
) (
	remainingBlocks []blocks.Block,
	newState generators.State,
	combinedParts []generators.Part,
	triggered bool,
	err error,
) {
	for _, comp := range comps.Processable() {
		if comp.Kind == "" {
			continue
		}

		// Filter blocks by this component's kind.
		var compBlocks []blocks.Block
		var otherBlocks []blocks.Block
		for _, b := range allBlocks {
			if b.Kind == comp.Kind {
				compBlocks = append(compBlocks, b)
			} else {
				otherBlocks = append(otherBlocks, b)
			}
		}
		allBlocks = otherBlocks

		if len(compBlocks) == 0 {
			continue
		}

		result := comp.Process(ctx, &ProcessContext{
			Blocks:     compBlocks,
			State:      state,
			Root:       root,
			HttpClient: httpClient,
		})
		if result.Err != nil {
			return allBlocks, state, combinedParts, triggered, result.Err
		}

		if result.State != nil {
			state = result.State
			triggered = true
		}
		if len(result.Parts) > 0 {
			combinedParts = append(combinedParts, result.Parts...)
			triggered = true
		}
	}

	return allBlocks, state, combinedParts, triggered, nil
}
