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
system prompt.

ProcessComponents is the shared function that iterates over Processable
components in registration order, calling each component's Process function and
accumulating results. Both the ai command (cmd/tai/ai.go) and the codes module
(codes/generate.go) call ProcessComponents, so the component processing loop is
identical across all generation commands — only the ComponentSet differs. The
function parameterizes Root and HttpClient (nil for commands whose components
do not need them, like ai's shell/continue), MaxRounds enforcement (disabled
for ai, enabled for codes), and roundCounts tracking (nil for ai). This is the
practical limit of loop unification: the outer loop structure differs because
ai uses an interactive chat phase (buildChat) while codes is single-shot, but
the component processing inner loop is fully shared.

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
	// State is the updated generators state. When non-nil, the component
	// modified the state (e.g., request-context appends fetched resources),
	// and a new generation round is triggered.
	State generators.State
	// Parts are user parts to append to the state, triggering a new round.
	Parts []generators.Part
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
	// trigger by producing Parts or modifying State. 0 means no limit. Used
	// to prevent infinite loops (e.g., request-context components that keep
	// requesting more context).
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

// ProcessComponents iterates over processable components in registration order,
// calling each component's Process function and accumulating results. It
// returns the updated ParserState (with consumed blocks removed), the updated
// State (if any component modified it), combined Parts from all components,
// whether any component triggered a new round (produced Parts or modified
// State), and an error if any component failed. When enforceMaxRounds is true
// and roundCounts is non-nil, per-component MaxRounds limits are enforced,
// preventing infinite loops from components that keep producing output.
//
// Both the ai command and the codes module call this function, so the
// component processing loop is identical across all generation commands —
// only the ComponentSet differs. See TheoryOfComponents.
func ProcessComponents(
	ctx context.Context,
	comps ComponentSet,
	currentPs *blocks.ParserState,
	state generators.State,
	root *os.Root,
	httpClient nets.HTTPClient,
	roundCounts map[string]int,
	enforceMaxRounds bool,
) (
	newPs *blocks.ParserState,
	newState generators.State,
	combinedParts []generators.Part,
	triggered bool,
	err error,
) {
	stateModified := false
	for _, comp := range comps.Processable() {
		result := comp.Process(ctx, &ProcessContext{
			ParserState: currentPs,
			State:       state,
			Root:        root,
			HttpClient:  httpClient,
		})
		if result.Err != nil {
			return currentPs, state, combinedParts, triggered, result.Err
		}
		if result.ParserState != nil {
			currentPs = result.ParserState
		}

		componentTriggered := false
		if result.State != nil {
			state = result.State
			stateModified = true
			componentTriggered = true
		}
		if len(result.Parts) > 0 {
			combinedParts = append(combinedParts, result.Parts...)
			componentTriggered = true
		}

		if componentTriggered {
			triggered = true
			if enforceMaxRounds && comp.MaxRounds > 0 && roundCounts != nil {
				roundCounts[comp.Kind]++
				if roundCounts[comp.Kind] > comp.MaxRounds {
					return currentPs, state, combinedParts, triggered,
						fmt.Errorf("max %s rounds (%d) exceeded", comp.Kind, comp.MaxRounds)
				}
			}
		}
	}
	return currentPs, state, combinedParts, stateModified || len(combinedParts) > 0, nil
}
