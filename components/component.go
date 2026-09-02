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
prompt verbatim under a short re-read instruction. Generation commands append
it as the last user prompt part before the dynamic user input, so the model
re-reads the complete rules immediately before generating. Because the
restate is assembled from the same text as the system prompt, the two can never
drift out of sync: there is no shortened reminder to keep consistent with the
full instructions. The restate is thresholded: SystemPromptRestateForUserPrompt
counts the tokens of the assembled user prompt and omits the restate within
SystemPromptRestateThreshold, because the restate counters attention decay
across long intervening content and a short user prompt leaves the system
prompt close to the generation point. Token budgets still reserve the restate
conservatively: the decision depends on the assembled size, which is known
only after assembly.

ProcessComponents is the shared function that iterates over Processable
components in registration order, filtering blocks by each component's Kind
and calling the component's Process function with the matching blocks. Both
the ai command (cmd/tai/ai.go) and the generation pipeline (pipeline/generate.go) call
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

const TheoryOfReadOnlyPrefetch = `
Parse-time prefetch runs the side-effect-free part of a block's
processing in a background goroutine as soon as the block is parsed
during streaming, so read-only fetches — go-src symbol resolution,
ingest file and network fetches — overlap the remainder of the
generation instead of starting after it ends. The latency between the
generation's end and the next round shrinks; the model-visible contract
is unchanged: results still arrive only as user content in the next
round (see blocks.TheoryOfDeferredExecution).

A component declares prefetchability with Compute: a function that turns
ONE block into its user-content parts without side effects. Processing
decomposes into compute (block to parts) and apply (the Process function
appends the prefetched parts or state in block order). Kinds whose
processing has side effects — shell runs commands, change applies edits
during streaming — or is handled outside the component loop (summary,
continue) carry no Compute and are never prefetched.

The generation loop starts one goroutine per parsed block whose kind has
a Compute, delivering the outcome through a buffered PrefetchFuture.
Futures travel with the collected blocks: they are per attempt, reset
with the blocks, and a failed attempt's futures are discarded with its
blocks. ProcessComponents passes each component the futures aligned with
its filtered blocks; the component consumes every prefetched outcome in
block order and falls back to a synchronous Compute call when no future
exists (direct callers, tests), so the applied parts keep the block
order regardless of completion order. A dropped future leaks nothing:
the buffered channel lets the computing goroutine finish, and a
panicking computation is recovered and delivered as an error outcome.
`

// ComponentProcessFunc processes blocks of a specific kind from the parser
// state in the main generation loop.
type ComponentProcessFunc func(ctx context.Context, pctx *ProcessContext) ProcessResult

// ComponentComputeFunc computes the user-content parts of one block
// without side effects, so the generation loop can start it in a
// background goroutine at parse time. The filesystem root and HTTP
// client are the block's processing environment, mirroring
// ProcessContext. See TheoryOfReadOnlyPrefetch.
type ComponentComputeFunc func(
	ctx context.Context,
	block blocks.Block,
	root *os.Root,
	httpClient nets.HTTPClient,
) (
	parts []generators.Part,
	err error,
)

// Prefetched carries the outcome of one prefetched block computation.
// See TheoryOfReadOnlyPrefetch.
type Prefetched struct {
	Parts []generators.Part
	Err   error
}

// PrefetchFuture delivers the outcome of one prefetched block
// computation. The channel is buffered with capacity one, so the
// computing goroutine never blocks on delivery, and a future dropped
// without a Wait — the blocks of a failed attempt — leaks nothing: the
// goroutine finishes and the channel is garbage collected. See
// TheoryOfReadOnlyPrefetch.
type PrefetchFuture chan Prefetched

// Wait blocks until the prefetched computation completes and returns
// its outcome. The component consuming the block calls Wait exactly
// once, in block order, so the applied parts keep the block order
// regardless of completion order. See TheoryOfReadOnlyPrefetch.
func (f PrefetchFuture) Wait() Prefetched {
	return <-f
}

// StartPrefetch runs fn in a background goroutine and returns a future
// delivering its outcome. A panicking computation is recovered and
// delivered as an error, so a panic in a prefetch never wedges the
// consumer nor crashes the generation loop. See
// TheoryOfReadOnlyPrefetch.
func StartPrefetch(fn func() Prefetched) PrefetchFuture {
	future := make(PrefetchFuture, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				future <- Prefetched{
					Err: fmt.Errorf("prefetch computation panicked: %v", r),
				}
			}
		}()
		future <- fn()
	}()
	return future
}

// ProcessContext bundles all dependencies a ComponentProcessFunc may need.
type ProcessContext struct {
	// Blocks are the blocks matching this component's Kind, pre-filtered
	// by ProcessComponents.
	Blocks []blocks.Block
	// Prefetched holds, for each entry of Blocks, the prefetched
	// computation of the matching block, aligned by index. A nil entry
	// marks a block that was not prefetched; the component computes it
	// synchronously instead. See TheoryOfReadOnlyPrefetch.
	Prefetched []PrefetchFuture
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
	// modified the state (e.g., ingest appends fetched resources),
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
	// Compute computes the user-content parts of ONE block of this kind
	// without side effects, so the generation loop can start it in a
	// background goroutine at parse time. When nil, the kind is never
	// prefetched: its processing has side effects, or is not decomposable
	// per block (shell, change, continue, summary). See
	// TheoryOfReadOnlyPrefetch.
	Compute ComponentComputeFunc
}

// ComponentSet is an ordered collection of Component.
type ComponentSet []Component

// KnownKinds returns the membership test for block kinds this component
// set makes available: every component that declares a Kind (processable
// or prompt-only) plus the given extra kinds — kinds the session
// processes outside the component loop (e.g. "done" by the goal runner).
// The pipeline's unknown-block-kind feedback consults the result so a
// model emitting a kind the session cannot process is corrected instead
// of silently ignored. See TheoryOfComponents and
// pipeline.TheoryOfUnknownBlockKinds.
func (c ComponentSet) KnownKinds(extra ...string) func(kind string) bool {
	known := make(map[string]bool, len(c)+len(extra))
	for _, comp := range c {
		if comp.Kind != "" {
			known[comp.Kind] = true
		}
	}
	for _, kind := range extra {
		known[kind] = true
	}
	return func(kind string) bool {
		return known[kind]
	}
}

// Computes returns the side-effect-free per-block computations declared
// by the set's components, keyed by block kind. The generation loop
// consults the map when a block is parsed: a kind with an entry is
// prefetched at parse time, every other kind is not. See
// TheoryOfReadOnlyPrefetch.
func (c ComponentSet) Computes() map[string]ComponentComputeFunc {
	computes := make(map[string]ComponentComputeFunc, len(c))
	for _, comp := range c {
		if comp.Kind != "" && comp.Compute != nil {
			computes[comp.Kind] = comp.Compute
		}
	}
	return computes
}

// ConsumePrefetchedBlockParts collects the user-content parts of the
// context's blocks in block order: a prefetched block delivers its own
// outcome through the aligned future, a non-prefetched block is computed
// synchronously through computeOne. The helper is the shared consumption
// side of parse-time prefetch: it keeps the applied parts in block order
// regardless of the prefetches' completion order, so a component
// decomposes into a per-block compute and this ordered apply. See
// TheoryOfReadOnlyPrefetch.
func ConsumePrefetchedBlockParts(
	ctx context.Context,
	pctx *ProcessContext,
	computeOne func(ctx context.Context, block blocks.Block) ([]generators.Part, error),
) (
	parts []generators.Part,
	err error,
) {
	for i, block := range pctx.Blocks {
		var blockParts []generators.Part
		if i < len(pctx.Prefetched) && pctx.Prefetched[i] != nil {
			outcome := pctx.Prefetched[i].Wait()
			blockParts, err = outcome.Parts, outcome.Err
		} else {
			blockParts, err = computeOne(ctx, block)
		}
		if err != nil {
			return nil, err
		}
		parts = append(parts, blockParts...)
	}
	return parts, nil
}

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

// SystemPromptRestateThreshold is the user prompt token count at or below
// which the verbatim system prompt restate is omitted. The restate buys
// renewed attention to the rules across long intervening user content; a
// user prompt within the threshold leaves the system prompt close to the
// generation point, so repeating it verbatim would spend tokens without
// effect. See TheoryOfComponents.
const SystemPromptRestateThreshold = 4 << 10

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

// SystemPromptRestateForUserPrompt decides the verbatim system prompt
// restate for the assembled user prompt parts: it counts the tokens of the
// parts' Text content and returns the restate parts when the count exceeds
// SystemPromptRestateThreshold, or no parts when the count is within the
// threshold — the system prompt is then still close to the generation
// point, so the verbatim copy is omitted. The token count of the assembled
// parts is returned alongside for the caller's logging. Only Text parts
// are counted, matching the text content each assembly site contributes.
// See TheoryOfComponents.
func SystemPromptRestateForUserPrompt(
	parts []generators.Part,
	systemPrompt string,
	countTokens func(string) (int, error),
) (
	restate []generators.Part,
	tokens int,
	err error,
) {
	var text strings.Builder
	for _, part := range parts {
		if t, ok := part.(generators.Text); ok {
			text.WriteString(string(t))
		}
	}
	tokens, err = countTokens(text.String())
	if err != nil {
		return nil, 0, err
	}
	if tokens <= SystemPromptRestateThreshold {
		return nil, tokens, nil
	}
	return []generators.Part{SystemPromptRestate(systemPrompt)}, tokens, nil
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
// prefetched optionally carries, aligned with allBlocks by index, the
// prefetched computation of each block: a non-nil entry delivers the
// block's side-effect-free outcome to the component, a nil entry or a
// missing tail leaves the component computing the block synchronously.
// The alignment lets a component consume each prefetched outcome with
// its own block, so duplicate blocks keep their own results. See
// TheoryOfReadOnlyPrefetch.
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
	prefetched ...PrefetchFuture,
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

		// Filter blocks by this component's kind, carrying the aligned
		// prefetched futures along so each block keeps its own outcome.
		// See TheoryOfReadOnlyPrefetch.
		var compBlocks []blocks.Block
		var compFutures []PrefetchFuture
		var otherBlocks []blocks.Block
		for i, b := range allBlocks {
			if b.Kind == comp.Kind {
				compBlocks = append(compBlocks, b)
				if i < len(prefetched) {
					compFutures = append(compFutures, prefetched[i])
				} else {
					compFutures = append(compFutures, nil)
				}
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
			Prefetched: compFutures,
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
