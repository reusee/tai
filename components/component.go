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
contributions), UserPromptParts (concatenating all user prompt parts, with
restate prompts appended as the last element so critical format reminders are
the last content the model reads before generating), and Processable (returning
the subset with Process functions for the generation loop). Restate prompts are
placed at the end of the user prompt, not the system prompt, so critical format
reminders are the last thing the model reads before generating.

ProcessComponents is the shared function that iterates over Processable
components in registration order, filtering blocks by each component's Kind
and calling the component's Process function with the matching blocks. Both
the ai command (cmd/tai/ai.go) and the codes module (codes/generate.go) call
ProcessComponents with a []Block slice (collected by the BlockHandler during
generation) and the current state. The function returns remaining blocks (not
matched by any component), the updated state, combined parts, and whether any
component triggered a new round. This eliminates the need for ParserState
reconciliation: blocks are managed externally by the caller, and the state
chain is modified exclusively through the State interface (AppendContent),
with no extra methods that bypass the immutable state chain.

ProcessResult carries a BackgroundParts field for informational output that
should reach the model only when a subsequent round exists. Components like
go-test produce BackgroundParts (e.g., a pass confirmation) without triggering
a round themselves; ProcessComponents collects them and prepends them to the
combined output when another component triggers a new round. When no component
triggers, BackgroundParts are discarded because there is no next round to
carry them. This prevents loops where the model re-emits blocks (e.g.,
go-test) because it never learned the previous invocation's result.

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
	// Blocks are the blocks matching this component's Kind, pre-filtered
	// by ProcessComponents.
	Blocks []blocks.Block
	// State is the current generators state. Components may modify it
	// (e.g., request-context appends fetched resources).
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
	// and a new generation round is triggered.
	State generators.State
	// Parts are user parts to append to the state, triggering a new round.
	Parts []generators.Part
	// BackgroundParts are parts included in the combined output only when
	// some other component triggers a new round (via Parts or State). Used
	// by components like go-test that produce informational output (e.g.,
	// "tests passed") which should be communicated to the model only when
	// there is a next round, preventing the model from re-emitting
	// unnecessary blocks. When no component triggers a new round,
	// BackgroundParts are discarded.
	BackgroundParts []generators.Part
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
	// Process extracts and handles blocks of this kind from the block list
	// in the main generation loop. If nil, the block kind is either
	// prompt-only (Kind == "") or handled by specialized logic outside the
	// component loop (e.g., change blocks applied via BlockHandler during
	// streaming, summary blocks processed in runPhaseWithRetry, memory
	// blocks processed post-loop).
	Process ComponentProcessFunc
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
// into the user content. Restate prompts are appended as the last user
// prompt part so critical format reminders are the last thing the model
// reads before generating. Components without UserPromptParts contribute
// nothing. See TheoryOfComponents.
func (c ComponentSet) UserPromptParts() []generators.Part {
	var parts []generators.Part
	for _, comp := range c {
		parts = append(parts, comp.UserPromptParts...)
	}
	// Append restate prompts at the end of user prompt parts so critical
	// format reminders are the last content the model reads before
	// generating. Restate prompts are placed in the user prompt, not the
	// system prompt. See TheoryOfComponents.
	if restate := c.RestatePrompts(); restate != "" {
		parts = append(parts, generators.Text(restate))
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
// filtering blocks by each component's Kind and calling the component's Process
// function with the matching blocks. It returns the remaining blocks (not
// matched by any component), the updated State (if any component modified it),
// combined Parts from all components, whether any component triggered a new
// round (produced Parts or modified State), and an error if any component
// failed. When enforceMaxRounds is true and roundCounts is non-nil, per-
// component MaxRounds limits are enforced, preventing infinite loops from
// components that keep producing output.
//
// BackgroundParts from non-triggering components are collected during iteration
// and prepended to combinedParts when any component triggers a new round. This
// ensures the model receives informational output (e.g., "tests passed") from
// non-triggering components alongside the triggering content, preventing
// unnecessary re-emission of blocks in subsequent rounds. When no component
// triggers, BackgroundParts are discarded.
//
// Both the ai command and the codes module call this function, so the
// component processing loop is identical across all generation commands —
// only the ComponentSet and block list differ. See TheoryOfComponents.
func ProcessComponents(
	ctx context.Context,
	comps ComponentSet,
	allBlocks []blocks.Block,
	state generators.State,
	root *os.Root,
	httpClient nets.HTTPClient,
	roundCounts map[string]int,
	enforceMaxRounds bool,
) (
	remainingBlocks []blocks.Block,
	newState generators.State,
	combinedParts []generators.Part,
	triggered bool,
	err error,
) {
	// Background parts are collected from components that produce
	// informational output (e.g., go-test pass confirmation) without
	// triggering a new round. They are prepended to combinedParts only
	// when some other component triggers a round, ensuring the model
	// receives the information alongside the triggering content.
	var backgroundParts []generators.Part

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

		// Collect background parts from non-triggering components.
		if len(result.BackgroundParts) > 0 {
			backgroundParts = append(backgroundParts, result.BackgroundParts...)
		}

		componentTriggered := false
		if result.State != nil {
			state = result.State
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
					return allBlocks, state, combinedParts, triggered,
						fmt.Errorf("max %s rounds (%d) exceeded", comp.Kind, comp.MaxRounds)
				}
			}
		}
	}

	// When a component triggered a new round, include background parts
	// so the model receives informational output (e.g., "tests passed")
	// from non-triggering components. Without this, the model would not
	// know that tests passed and would re-emit go-test blocks in
	// subsequent rounds, creating unnecessary loops.
	if triggered && len(backgroundParts) > 0 {
		combinedParts = append(backgroundParts, combinedParts...)
	}

	return allBlocks, state, combinedParts, triggered, nil
}
