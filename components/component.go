package components

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/reusee/tai/blocks"
	"github.com/reusee/tai/generators"
	"github.com/reusee/tai/nets"
)

const TheoryOfComponents = `
Component is the unified extension mechanism for the generation pipeline. It
generalizes beyond block processing: a Component can contribute a system prompt
section, a restate/reminder prompt, user prompt parts, define a block kind for
parsing, process blocks of that kind, or any combination thereof. This
unification eliminates the need for a separate concept for prompt-only
mechanisms (e.g., read-only file rules, mandatory planning) that do not produce
or consume blocks but still need to be assembled into the system prompt and
managed as reusable, composable units.

A Component with a Process function is processed in the main generation loop;
a Component without one (prompt-only or informational) contributes its
PromptSection to the system prompt but is not invoked during output processing.
A Component can also contribute UserPromptParts, which are prepended to the
user's input similar to how CodeProvider.Parts provides context. This unifies
user prompt contributions under the same Component framework as system prompt
sections and restate reminders. ComponentSet is an ordered collection of
Components that provides PromptSections (concatenating all system prompt
contributions), RestatePrompts (concatenating all restate/reminder prompt
contributions), UserPromptParts (concatenating all user prompt parts), and
Processable (returning the subset with Process functions for the generation
loop). RestatePrompts are assembled separately from PromptSections to keep
critical format reminders grouped as a distinct section at the end of the
system prompt. Validate ensures every Component with a Kind either has a
Process function or declares a ProcessingPath, preventing silent gaps where a
block kind is taught to the model but no code processes its output. Prompt-only
Components (empty Kind) are exempt from this check because they contribute only
prompt text and have no block output to process.

The mechanism is the integrity guarantee: it makes the coupling between prompt
and processing explicit and machine-checkable rather than implicit and
human-maintained. By extending the same mechanism to prompt-only contributions,
restate reminders, and user prompt parts, the system prompt assembly, user
prompt assembly, and output processing loop share a single ComponentSet,
ensuring that every prompt contribution is registered, every block kind has a
matching processor, and every restate reminder and user prompt part is
assembled through the same unified mechanism.
`

// ComponentProcessFunc processes blocks of a specific kind from the parser
// state in the main generation loop.
type ComponentProcessFunc func(ctx context.Context, pctx *ProcessContext) ProcessResult

// ProcessContext bundles all dependencies a ComponentProcessFunc may need.
type ProcessContext struct {
	ParserState *blocks.ParserState
	State       generators.State
	Root        *os.Root
	HttpClient  nets.HTTPClient
}

// ProcessResult holds the outcome of processing blocks of a single kind.
type ProcessResult struct {
	// ParserState is the new parser state with consumed blocks removed.
	ParserState *blocks.ParserState
	// State is the updated generators state (if the handler appended content
	// directly, e.g., request-context appends fetched resources to state).
	State generators.State
	// Parts are user parts to append to the state, triggering a new round.
	Parts []generators.Part
	// Continue indicates whether a new generation round should be triggered
	// immediately, stopping further component processing in the current round.
	Continue bool
	// Err is the error encountered during processing, if any.
	Err error
}

// Component is the unified extension mechanism for the generation pipeline.
// A Component can contribute a system prompt section, a restate/reminder prompt,
// user prompt parts, define a block kind, process blocks, or any combination.
// A Component with an empty Kind is a prompt-only component that contributes
// its PromptSection to the system prompt but does not process blocks.
// See TheoryOfComponents.
type Component struct {
	// Kind is the block kind name (e.g., "change", "shell", "continue").
	// Empty for prompt-only components that contribute to the system prompt
	// but do not process blocks.
	Kind string
	// PromptSection is the system prompt text that teaches the model how
	// to use this component. Empty if no prompt is needed.
	PromptSection string
	// RestatePrompt is a short critical reminder that reinforces the block
	// format rules for this component. Unlike PromptSection which provides
	// initial instructions, RestatePrompt is assembled separately via
	// ComponentSet.RestatePrompts() to keep reminders grouped as a distinct
	// section at the end of the system prompt. Empty if no restate prompt
	// is needed.
	RestatePrompt string
	// UserPromptParts are user prompt parts contributed by this component.
	// These are prepended to the user's input, similar to how
	// CodeProvider.Parts provides context. Unlike PromptSection which goes
	// into the system prompt, UserPromptParts goes into the user content.
	// Empty for components that contribute only to the system prompt.
	UserPromptParts []generators.Part
	// Process extracts and handles blocks of this kind from the parser
	// state in the main generation loop. If nil and Kind is non-empty,
	// ProcessingPath must describe where the block is processed instead.
	Process ComponentProcessFunc
	// ProcessingPath documents where blocks of this kind are processed
	// when Process is nil (e.g., "applyChangeBlocks", "runPhaseWithRetry",
	// "informational"). A non-empty ProcessingPath with a nil Process
	// declares that the block is handled by specialized logic outside the
	// component loop or is intentionally unprocessed. An empty ProcessingPath
	// with a nil Process is valid for prompt-only components (Kind == "").
	ProcessingPath string
	// MaxRounds limits the number of consecutive rounds this component can
	// trigger via Continue=true. 0 means no limit. Used to prevent infinite
	// loops (e.g., request-context components that keep requesting more context).
	MaxRounds int
}

// ComponentSet is an ordered collection of Component.
type ComponentSet []Component

// PromptSections returns the concatenated system prompt sections from all
// components that have a non-empty PromptSection, in registration order.
func (c ComponentSet) PromptSections() string {
	var sb strings.Builder
	for _, comp := range c {
		if comp.PromptSection != "" {
			sb.WriteString(comp.PromptSection)
			sb.WriteString("\n")
		}
	}
	return sb.String()
}

// RestatePrompts returns the concatenated restate/reminder prompt sections
// from all components that have a non-empty RestatePrompt, in registration
// order. These are short critical reminders that reinforce block format
// rules, assembled separately from PromptSections to keep them grouped as
// a distinct reminder section at the end of the system prompt.
func (c ComponentSet) RestatePrompts() string {
	var sb strings.Builder
	for _, comp := range c {
		if comp.RestatePrompt != "" {
			sb.WriteString(comp.RestatePrompt)
			sb.WriteString("\n")
		}
	}
	return sb.String()
}

// UserPromptParts returns the concatenated user prompt parts from all
// components, in registration order. These are prepended to the user's
// input, similar to how CodeProvider.Parts provides context. Unlike
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

// Validate returns an error if any component with a non-empty Kind has neither
// a Process function nor a ProcessingPath, indicating an unprocessed block kind
// that would cause emitted blocks to be silently ignored. Prompt-only components
// (Kind == "") are exempt from this check.
func (c ComponentSet) Validate() error {
	for _, comp := range c {
		if comp.Kind != "" && comp.Process == nil && comp.ProcessingPath == "" {
			return fmt.Errorf("component with kind %q has no Process function and no ProcessingPath; "+
				"it would be silently ignored if emitted by the model", comp.Kind)
		}
	}
	return nil
}
