package pipeline

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"os"
	"strings"

	"github.com/reusee/tai/blocks"
	"github.com/reusee/tai/changes"
	"github.com/reusee/tai/components"
	"github.com/reusee/tai/generators"
	"github.com/reusee/tai/logs"
	"github.com/reusee/tai/nets"
	"github.com/reusee/tai/records"
	"github.com/reusee/tai/tree"
)

const TheoryOfContextPhilosophy = `
The system provides all context the model needs in a single generation
request, not through multi-turn conversation. This single-shot approach sets
it apart from agentic agents that grow context via dialogue.

Upfront construction: file contents, dependency graphs, system prompts, and
task instructions are assembled before the first call. Pruning removes
irrelevant files; simplification strips function bodies and comments from
non-focus packages; token budgeting caps input size. The model reasons over
the complete picture into changes ready for human review.

Architectural constraints:

- No long conversations. The system accumulates no dialogue across tasks;
  each invocation builds fresh context from the filesystem state. The ai
  command's interactive mode lets the user type messages across turns, but
  each turn sends the full accumulated context, not a compressed fragment.

- No conversation compression. Old dialogue is never summarized to free
  token budget; context is managed solely by pruning, AST-level
  simplification, and deterministic file ordering. Handoff
  (TheoryOfHandoff) condenses truncated output for one-shot error recovery,
  not persistent history. Thought summarization (TheoryOfThoughtsSummarize)
  writes to the user's screen for readability; it never feeds back as
  compressed context.

- No blind exploration. The upfront context always carries the complete
  declaration surface, so the model never starts from nothing (see
  gotools.TheoryOfContextStrategy). Implementation source is fetched on
  demand with go-src blocks — a targeted pull from the known surface, not
  semantic-search probing. Ingest blocks serve external resources unavailable
  at construction time (network fetches, glob expansion), not as a
  substitute for upfront context.

- Multi-round generation is task decomposition, not conversation. Continue
  blocks split large tasks into bounded rounds; shell and go-test blocks run
  autonomous verification. The loop executes tasks; it is not a chatbot.

Features assuming a long-conversation model — dialogue-grown context, turns
summarized to free budget, conversation history as knowledge base — violate
this philosophy and are out of scope.
`

const TheoryOfLoops = `
The pipeline unifies the generation loop pattern across all generation
commands (the auto-detected default, ai, next). The core pattern:
1. Wrap state with ParserState to collect blocks during streaming
2. Execute the phase chain until done
3. Unwrap ParserState to get the final state and collected blocks
4. Process collected blocks through components (if any)
5. Repeat until no components trigger or MaxGenerations is reached

Run is exposed as a tree iterator: func(ctx, opts, result *Result)
iter.Seq2[*tree.Tree, error]. The result is filled incrementally as
the run progresses; every notable occurrence — attempt lifecycle
(start, completion, truncation), retry decisions and handoffs,
synthesized completion summaries, per-attempt token usage,
component-triggered and idle continuations — is first recorded as an
event node in the session tree and then the full tree is yielded the
moment its facts are known (see TheoryOfLoopEvents). The terminal
error, if any, arrives with the final yield's error component when
the run stops. Callers may suspend and resume the run via iter.Pull2,
inspecting the result between pulls.

A generation is one pass through the user-driven loop: the model
generates, the summary and parts are collected, and component output (or
an idle handler) may schedule the next generation. A retry is a
re-execution of the phase chain within the same generation, triggered by
a missing completion (no summary block) or an error after content
output; each re-execution is a new attempt, numbered 1-based within the
generation, up to the retry budget. Retries count as attempts in attempt
statistics.

Retry on missing completion and handoff: an attempt without a summary
block — whether the generation limit truncated the model mid-stream
before its closing summary block, the model emitted a summary and
continued until cut off, or the model ended its response without one —
or with an abnormal finish reason (e.g., "length" from max-token
truncation), is retried from the original pre-generation State.
Truncation often happens because the model attempted too many changes in
a single turn. When output meets the minimum threshold, the handoff
process creates a self-contained summary carrying forward established
conclusions, attempted changes, and task-partitioning guidance. The
retry user prompt explicitly instructs the model to partition extensive
modifications: implement an initial manageable subset of changes in the
current response, end with a summary block, and use a continue block to
carry over the remaining work into subsequent generations, preventing
repeated truncation loops. Short or empty outputs are retried directly.
See TheoryOfHandoff.

The summary block is non-negotiable: every attempt that ends without a
summary block is retried when RetryOnMissingCompletion is enabled,
including attempts whose blocks trigger components (ingest, shell,
continue, go-test, go-src). No block kind replaces or implies the
summary. The retry feedback names the violation —
missingSummaryRetryPrefix when the response simply ended without the
summary block, incompleteOutputHandoffPrefix when the finish reason
shows truncation — states the attempt number, and instructs the model to
re-emit every block it intends to take effect together with the summary
block, because the failed attempt's blocks were discarded.

When the retry budget is exhausted and the final attempt still lacks a
summary block, the loop synthesizes a summary from the generation's
output and appends it to the state as a summary block, so the generation
has a completion signal for the attempt statistics and the display
front-end's Tree tab. The synthesis applies to every exhausted
generation, including generations whose blocks trigger components.

Retry on error: an error after content output retries from the state
that includes the partial output, appending the error context and the
handoff summary as user content. Errors before any content output do not
retry.

Retry feedback states the current attempt number (e.g., "retry attempt
1 of 3") so the model knows how much budget remains and can prioritize
correcting the error.

Continuity after correction: both the error-retry feedback
(errorRetryPrefix, covering change-block apply errors) and the
block-correction feedback (formatParseErrors, formatUnknownKindFeedback)
instruct the model to resume the original task after fixing the fault.
The correction round is part of the same generation flow, not a fresh
start: a model that fixes the block, emits the summary, and stops ends
the generation with only its summaries as cross-loop context, so in goal
mode the next loop restarts from a nearly empty picture — the observed
"forgot the task" failure. The correction feedback therefore carries
the resume directive verbatim in the same note.

Unknown-block-kind correction: an attempt that completes with collected
blocks whose kind the session cannot process — an unknown kind, a kind
disabled by configuration, or a kindless block — triggers a correction
round when RunOptions.KnownBlockKinds is configured, mirroring the
parse-error feedback: the loop reports the unprocessed blocks
immediately after the attempt and instructs the model not to re-emit
them (the kind itself is the fault), to use the kind's stated
replacement behavior, and to resume the original task. The two
categories share one correction decision and one budget; see
TheoryOfUnknownBlockKinds.

Applied change blocks are recorded in the session tree: each applied
block's node content leads with its op, target, and file, and the node
carries an applied result child, so the round-triggering feedback's
outline is the applied-changes record and no separate note is fed back.
See TheoryOfSessionTree and TheoryOfStreamingApply.
`

const TheoryOfUsageLogging = `
The token usage of each generation attempt is recorded by the Run loop
itself, not by individual commands. After each attempt, the usage record
carries the 1-based attempt number and the prompt, cached, completion,
and thought token counts from the attempt's final usage. The record is
written as a usage event node in the session tree — the single display
source for a live consumer; the display front-end's Tree tab renders
its usage line from the node's content — and to a "usage" log entry, so
every generation command — the auto-detected default, ai, next, ping —
shows token consumption in its logs and in the TUI's Logs pane. An
attempt that ends with an error carries an outcome marker ("error" in
the log entry, in the usage node's content, and in the rendered line's
"(error)" suffix), so token consumption is traceable for every attempt,
including retries. Attempts that record no token usage emit nothing.

Streaming requests measure speed onto the final usage (see
generators.TheoryOfUsageTiming): the loop appends the one-decimal keys
ttft_seconds and tokens_per_second to the usage log entry, and the
usage node's content ends with the same fragment rendered by
generators.Usage.SpeedSuffix. Non-streaming usages carry no timings, so
the keys and the fragment are omitted rather than printed as zeros. The
statistics table keeps count-only columns and takes its duration from
the loop's own clock, not from these per-request measurements.

The usage is extracted by scanning the state's contents appended since
the start of the attempt and taking the final Usage part, rather than
summing intermediate usage snapshots that may be emitted by streaming
providers (e.g., Gemini's streaming UsageMetadata).
`

const errorRetryPrefix = "[System note: An error occurred: %s. This is retry attempt %d of %d. The failed attempt's output was discarded — its structured blocks were NOT applied. If the intended modifications are extensive, partition the work across multiple rounds using continue blocks rather than emitting all changes at once. Re-emit every block you intend to take effect, then correct the issue and continue the ORIGINAL task: the retry exists only to repair this error, not to restart the work — resume the original task exactly where the failed attempt stopped, continue the remaining plan, and do not treat the correction itself as the task's completion.]\n\n"

const defaultMaxRetries = 3

// maxParseErrorCorrections bounds the number of corrections that feed
// parse errors back to the model for self-correction. The bound is
// cumulative per run: it resets only when a generation produces no
// parse errors, so a model that persistently emits malformed blocks
// cannot restart the correction cycle indefinitely when other
// components keep triggering generations. When the bound is reached,
// feedback stops and the uncorrected parse errors are recorded in
// Result.ParseErrors. See TheoryOfLoops.
const maxParseErrorCorrections = 3

const incompleteOutputHandoffPrefix = "[System note: The previous generation was truncated before completion. This is retry attempt %d of %d. The truncated output was discarded and will not appear in history — its structured blocks were NOT applied. Truncation typically occurs when attempting too many changes in a single response, exceeding the output limit. If the planned modifications are extensive, do NOT attempt to emit all changes at once. Instead, partition the work: implement a manageable initial subset of change blocks in this round, and use a continue block to carry over the remaining tasks into subsequent rounds. Re-emit every block you intend to take effect in this round. Nothing in the interrupted attempt was completed: changes are atomic, so there is no completed work on disk, and no next step to carry forward without implementation. Below is the self-contained handoff summary from the previous attempt, preserving its valuable thinking: discoveries, insights, analysis, decisions, and attempted changes. Use it as reference to partition and guide your work, but continue to think for yourself: the handoff does not replace your own reasoning, and you must still analyze the problem and decide how to proceed.]\n\n"

const missingSummaryRetryPrefix = "[System note: Your previous response ended WITHOUT the required summary block. This is retry attempt %d of %d. The summary block is MANDATORY in every response: it is the completion signal the system uses to verify that generation ended normally and followed the rules. No other block — change, shell, go-test, go-src, ingest, continue — replaces or implies it. The previous attempt was discarded: its structured blocks were NOT processed. Re-emit every block you intend to take effect, then close the response with a summary block (a \"- \" bullet list of what was done; \"No changes were needed.\" when nothing was done). Never end a response on any block other than the summary block.]\n\n"

// StateDecorator wraps a generation state before the loop starts,
// returning a new state that observes or modifies the original. The
// decorator is applied after interaction recording, so it sees every
// subsequent content append. Multiple decorators are applied in order,
// each wrapping the state produced by the previous one. See
// RunOptions.StateDecorators.
type StateDecorator func(generators.State) generators.State

// InteractionRecorder provider: the default is nil, meaning no interaction
// recording. Commands that want recording pass their recorder explicitly
// through RunOptions.InteractionRecorder, which takes precedence over the
// loop's default. Keeping the default provider here (rather than in an
// outer module) avoids duplicate-definition conflicts in dscope scopes.
// See records.TheoryOfInteractionRecording.
func (Module) InteractionRecorder() InteractionRecorder {
	return nil
}

// The records.Recorder implements InteractionRecorder. The assertion lives
// here rather than in records: pipeline imports records, and the reverse
// import would create a cycle. See records.TheoryOfInteractionRecording.
var _ InteractionRecorder = (*records.Recorder)(nil)

// Run executes generation generations in a loop. Each generation wraps
// the state with ParserState, executes the phase chain (retrying
// incomplete attempts as further attempts within the generation),
// processes blocks via components, and continues if a component
// triggers a new generation. When Components is empty, the loop runs a
// single generation (single-shot mode). The result is filled into
// result as the run progresses; every notable occurrence — attempt
// lifecycle (start, completion, truncation), request parameters,
// retries and handoffs, synthesized completion summaries, attempt
// finish reasons, per-attempt token usage, periodic thought summaries,
// and component-triggered or idle continuations — is first recorded as
// an event node in the session tree and then the full tree is yielded,
// the moment its facts are known, with the terminal error, if any,
// arriving with the final yield's error component. Every yield carries
// the whole tree, so a consumer renders — and projects — the same tree
// the pipeline writes, with no separately maintained display state.
// Callers may suspend and resume the run via iter.Pull2, inspecting
// the result between pulls. See TheoryOfLoops and TheoryOfLoopEvents.
type Run func(ctx context.Context, opts RunOptions, result *Result) iter.Seq2[*tree.Tree, error]

// generationResult is the outcome of one generation: the updated
// state, the generation's summary, and the parts that determine whether
// the next generation starts. The parts are the generation's return
// value: when continueNext is true, they are appended to the state as
// user content and the next generation begins. See TheoryOfLoops.
type generationResult struct {
	state        generators.State
	summaries    []string
	parts        []generators.Part
	continueNext bool
	// finalBlocks is set when the generation is the whole run
	// (single-shot mode): the loop ends with these blocks as the
	// result.
	finalBlocks []blocks.Block
}

// loopState holds the mutable state of a generation loop run. The main
// loop in Run executes generations via runGeneration; the state here is
// updated by each generation and carried into the next. Every event
// write yields the full session tree through the guarded
// emitTree/emitTerminalTree methods: after the consumer stops, further
// yields are dropped so the loop's bookkeeping can still complete. See
// TheoryOfLoops and TheoryOfLoopEvents.
type loopState struct {
	ctx     context.Context
	opts    RunOptions
	rec     InteractionRecorder
	result  *Result
	yield   func(*tree.Tree, error) bool
	stopped bool
	state   generators.State

	// attempt is the session-wide 1-based attempt number of the attempt
	// being executed: it increments across every attempt of the run and
	// never resets, so component-triggered generations and
	// idle-handler inputs continue the sequence instead of restarting
	// at 1. attemptInGeneration is the attempt's position within its
	// generation's retry budget (1-based), pairing with maxRetries for
	// the truncated and retry budget lines carried in event node
	// contents. See TheoryOfLoopEvents.
	attempt             int
	attemptInGeneration int

	remainingBlocks []blocks.Block
	maxRetries      int

	parseErrorCorrections  int
	uncorrectedParseErrors []*blocks.BlockParseError
	skipOnAttemptStart     bool

	runErr error

	// logger records the aggregated token usage of each attempt as a
	// "usage" log entry; the usage event node in the session tree is
	// the display source for a live consumer such as the TUI's Tree
	// tab. The logger is dscope provided, captured by the Run provider.
	// See TheoryOfUsageLogging.
	logger logs.Logger

	// temperatureFlag and effortFlag carry the dscope-resolved
	// temperature and reasoning-effort flag values, captured by the Run
	// provider. The request event node's content resolves the effective
	// generation parameters from the generator spec and these flag
	// overrides, mirroring the generators' flag-over-spec precedence.
	// See TheoryOfLoopEvents.
	temperatureFlag generators.TemperatureFlag
	effortFlag      generators.EffortFlag

	// sessionTree is the immutable session tree the loop owns: every
	// operation of the run — initial input, responses, summaries,
	// blocks, results, feedback, and the loop's own event nodes — is
	// written as a node, and every write is yielded as the full tree.
	// It never joins the generators.State chain. See
	// TheoryOfSessionTree.
	sessionTree *tree.Tree
	// sessionRoot names the node under which this session writes its
	// nodes: the tree's root for a fresh run, the goal run's loop-N node
	// for a continued one. Run sets it from the continuation before the
	// first write. See TheoryOfSessionTree.
	sessionRoot string
	// currentResponse names the response node of the latest successful
	// attempt; block nodes default to it as their parent. See
	// TheoryOfSessionTree.
	currentResponse string
	// namingErrs holds the latest attempt's session-tree naming errors,
	// consumed by the shared block-correction decision and cleared
	// after it. See TheoryOfSessionTree and TheoryOfUnknownBlockKinds.
	namingErrs []string
}

// buildContinueReason describes why the generation loop continues to
// the next generation: the kinds of blocks processed by components, the
// parse-error feedback, the unknown-block-kind feedback, or a
// component's state modification. See TheoryOfLoops and
// TheoryOfUnknownBlockKinds.
func buildContinueReason(triggeredKinds []string, parseErrorFeedback bool, unknownKindFeedback bool) string {
	var reasons []string
	if len(triggeredKinds) > 0 {
		reasons = append(reasons, strings.Join(triggeredKinds, ", ")+" blocks")
	}
	if parseErrorFeedback {
		reasons = append(reasons, "parse error feedback")
	}
	if unknownKindFeedback {
		reasons = append(reasons, "unknown block kind feedback")
	}
	if len(reasons) == 0 {
		return "component modified the generation state"
	}
	return strings.Join(reasons, " and ") + " scheduled the next generation"
}

func (ls *loopState) runGeneration() (generationResult, error) {
	var collectedBlocks []blocks.Block
	// prefetchedFutures holds, aligned with collectedBlocks by index,
	// the future of each block whose kind declares a side-effect-free
	// per-block Compute: the computation starts in a background
	// goroutine at parse time, so the read-only fetch overlaps the
	// remainder of the generation, and the component consumes the
	// outcome in block order after the generation ends. The slice
	// resets with the blocks at every attempt, so a failed attempt's
	// futures are discarded with its blocks. See
	// components.TheoryOfReadOnlyPrefetch.
	var prefetchedFutures []components.PrefetchFuture
	// handledBlocks records the blocks the BlockHandler consumed
	// without error during the current attempt: for the change
	// handler, consumption follows a successful application, so this
	// is the applied record driving the session tree's applied result
	// children. It resets with every attempt, so a failed attempt's
	// consumed blocks never reach the tree. See TheoryOfStreamingApply.
	var handledBlocks []blocks.Block
	var generationSummaries []string
	var generationParseErrors []*blocks.BlockParseError
	phaseState := ls.state
	var generationErr error

	// computes maps the kinds with a side-effect-free per-block
	// computation declared by the session's components: a parsed block
	// whose kind has an entry is prefetched at parse time. See
	// components.TheoryOfReadOnlyPrefetch.
	computes := ls.opts.Components.Computes()

	// attemptBase is the content count at the start of the current
	// attempt: the window for the finish-reason extraction, the
	// incomplete-output extraction, and the handoff usage injection.
	// It is reassigned at the top of every attempt and read by the
	// post-loop tails (synthesis, usage recording) scoped to the
	// final attempt. See TheoryOfLoops and
	// TheoryOfHandoffUsageAccounting.
	attemptBase := generators.CountContents(ls.state)

	// Inner retry loop: each iteration is one attempt, opened by the
	// attempt-start event node immediately before its work — including
	// retries, so every attempt's opening is recorded the moment it
	// begins. The attempt number is session-wide: it increments on
	// every attempt and never resets across generations;
	// attemptInGeneration records the position within this
	// generation's retry budget. See TheoryOfLoops and
	// TheoryOfLoopEvents.
	for retry := 0; ; retry++ {
		ls.attempt++
		ls.attemptInGeneration = retry + 1
		attemptBase = generators.CountContents(ls.state)
		collectedBlocks = nil
		handledBlocks = nil
		generationParseErrors = nil
		prefetchedFutures = nil

		// Attempt open: report to the interaction recorder, record the
		// attempt-start event node, and reset per-attempt state (e.g.,
		// MemoryStore.Reset). Only the generation's first attempt
		// honors the parse-error correction path's skip; retries
		// reset unconditionally. See TheoryOfLoopEvents.
		if ls.rec != nil && ls.rec.Enabled() {
			ls.rec.AttemptStart()
		}
		ls.writeEventNode("attempt-start", fmt.Sprintf("attempt %d start (%d/%d)",
			ls.attempt, ls.attemptInGeneration, ls.maxRetries))
		if ls.opts.OnAttemptStart != nil && (!ls.skipOnAttemptStart || retry > 0) {
			ls.opts.OnAttemptStart()
		}

		// The request node precedes the attempt's request: it
		// describes the actual generation parameters — the model and
		// the effective temperature, reasoning effort, and token
		// limits — resolved from the generator spec with the flag
		// overrides, mirroring the generators' flag-over-spec
		// precedence. Unlike the generators' "generating" log, which
		// records the spec's effort even when the flag overrides it,
		// the node's content reports the values the request actually
		// carries. The loop cannot see retries internal to the
		// generator's Retrier: one loop attempt may cover several API
		// calls. See TheoryOfLoopEvents.
		if ls.opts.Generator != nil {
			ls.writeEventNode("request", describeRequest(
				ls.opts.Generator.Spec(),
				ls.temperatureFlag,
				ls.effortFlag,
			))
		}

		// Create parser handler that collects blocks and
		// optionally invokes the caller's BlockHandler.
		parserHandler := func(block blocks.Block) error {
			// Report every parsed block to the interaction
			// recorder, whether or not it is consumed by the
			// caller's BlockHandler.
			if ls.rec != nil && ls.rec.Enabled() {
				ls.rec.Block(block)
			}
			if ls.opts.BlockHandler != nil {
				consumed, err := ls.opts.BlockHandler(block)
				if err != nil {
					return err
				}
				if consumed {
					handledBlocks = append(handledBlocks, block)
					return nil
				}
			}
			// Parse-time prefetch: a block whose kind declares a
			// side-effect-free per-block Compute starts in a
			// background goroutine now, so the read-only fetch
			// overlaps the remainder of the generation. The future is
			// stored aligned with the collected block; the component
			// consumes it in block order after the generation ends.
			// See components.TheoryOfReadOnlyPrefetch.
			var future components.PrefetchFuture
			if compute, ok := computes[block.Kind]; ok {
				future = components.StartPrefetch(func() components.Prefetched {
					parts, err := compute(ls.ctx, block, ls.opts.Root, ls.opts.HTTPClient)
					return components.Prefetched{Parts: parts, Err: err}
				})
			}
			collectedBlocks = append(collectedBlocks, block)
			prefetchedFutures = append(prefetchedFutures, future)
			return nil
		}

		// Wrap state with ParserState.
		parserState := blocks.NewParserState(ls.state, parserHandler)
		wrappedState := generators.State(parserState)

		// Build and execute phase chain.
		phase := ls.opts.PhaseBuilder(ls.opts.Generator)
		for phase != nil {
			var err error
			phase, wrappedState, err = phase(ls.ctx, wrappedState)
			if err != nil {
				generationErr = err
				break
			}
		}

		// Unwrap ParserState to get the base state. A phase may
		// return a nil state on error; fall back to the pre-phase
		// state so OnPhaseError receives a valid state rather
		// than a nil pointer that would cause a panic.
		if wrappedState == nil {
			phaseState = ls.state
		} else if ps, ok := generators.As[*blocks.ParserState](wrappedState); ok {
			phaseState = ps.Unwrap()
			// Collect parse errors from the stream so they can be
			// fed back to the model for self-correction.
			// See TheoryOfParseErrorCollection.
			generationParseErrors = ps.ParseErrors()
			// Report malformed blocks to the interaction recorder.
			if ls.rec != nil && ls.rec.Enabled() {
				for _, parseErr := range generationParseErrors {
					ls.rec.ParseError(parseErr)
				}
			}
		} else {
			phaseState = wrappedState
		}

		// The attempt's finish reason feeds both the session tree —
		// every attempt's completion signal, including attempts that
		// later fail — and the completion check below. Recorded
		// immediately when known. See TheoryOfLoopEvents.
		finishReason := extractFinishReason(phaseState, attemptBase)
		if finishReason != "" {
			ls.writeEventNode("finish", "finish: "+finishReason)
		}

		if generationErr != nil {
			// A disk-change failure cannot be repaired by retrying
			// the attempt: the in-memory snapshot no longer matches
			// the disk, so the retry would compute changes against
			// the same stale content. End the run with a handoff
			// error; the goal runner carries it into the next loop,
			// which reloads the filesystem. See
			// TheoryOfDiskChangeHandoff.
			var diskChanged *changes.DiskChangedError
			if errors.As(generationErr, &diskChanged) {
				return generationResult{state: phaseState}, ls.endOnDiskChange(generationErr, phaseState, attemptBase)
			}
			// Retry on any error when content was output during
			// the attempt. The loop summarizes the incomplete
			// output, appends both the error context and the
			// summary as user content so the model can correct
			// its output, resets per-attempt state via
			// OnAttemptStart (which resets the MemoryStore,
			// discarding failed changes), and retries from the
			// updated state. Errors that occur before any
			// content is output do not trigger retry. The
			// feedback states the current attempt number so
			// the model knows how much retry budget remains.
			// See TheoryOfLoops.
			if ls.opts.RetryOnError && retry < ls.maxRetries {
				prevCount := attemptBase
				if generators.CountContents(phaseState) > prevCount {
					ls.state = phaseState

					// Report the failed attempt to the
					// interaction recorder and record the retry
					// decision as an event node, immediately.
					// See TheoryOfLoopEvents.
					if ls.rec != nil && ls.rec.Enabled() {
						ls.rec.AttemptError(generationErr)
						ls.rec.Event("decision", fmt.Sprintf("error after partial output triggered retry: attempt %d/%d: %v", retry+1, ls.maxRetries, generationErr))
					}
					ls.writeEventNode("retry", fmt.Sprintf("retry attempt %d/%d: %v",
						retry+1, ls.maxRetries, generationErr))

					var retryParts []generators.Part
					retryParts = append(retryParts, generators.Text(
						fmt.Sprintf(errorRetryPrefix, generationErr.Error(), retry+1, ls.maxRetries)))

					// For change block apply errors, add specific
					// guidance: the retry discards ALL change
					// blocks from the failed attempt (OnAttemptStart
					// resets the in-memory store below), so the
					// model must re-emit every intended change
					// block, correcting the one that failed.
					// See TheoryOfLoops.
					var applyErr *changes.ApplyError
					if errors.As(generationErr, &applyErr) {
						retryParts = append(retryParts, generators.Text(
							"\nThe change block that caused the error was NOT applied, and this retry discards ALL change blocks from the failed attempt. Re-emit every intended change block, correcting the one that caused the error.\n"))
					}

					summary := ""
					retryPrompt := ""
					if ls.opts.Handoff != nil {
						incompleteText := ExtractIncompleteOutput(phaseState, prevCount)
						if incompleteText != "" {
							// Record the handoff request's start
							// immediately, before the request is
							// sent. Handoff event nodes carry the
							// attempt attribution but no budget
							// figures: handoff generation itself
							// retries without an attempt limit, so
							// an "attempt x/y" display would
							// misrepresent it. See
							// TheoryOfLoopEvents and
							// TheoryOfHandoff.
							ls.writeEventNode("handoff-start", "handoff started")
							handoff, handoffErr := ls.opts.Handoff(handoffInput(incompleteText, ls.sessionTree, ls.sessionParent()))
							if handoffErr == nil && handoff != nil {
								summary = handoff.Summary
								retryPrompt = handoff.Prompt
								// Record the produced handoff. See
								// TheoryOfLoopEvents.
								ls.writeEventNode("handoff", "handoff summary:\n"+handoff.Summary)
								// Account the handoff request's own token
								// spend before the failed attempt is
								// recorded. The window starts at the
								// failed attempt's base, so usage
								// retained from earlier error retries is
								// never re-attributed to this attempt.
								// See TheoryOfHandoffUsageAccounting.
								phaseState = appendHandoffUsage(phaseState, prevCount, handoff.Usage)
							}
						}
					}

					// Record the attempt's token usage, including the
					// injected handoff spend. See TheoryOfUsageLogging.
					ls.recordAttemptUsage(phaseState, attemptBase, "")

					// Record the failed attempt in attempt statistics
					// so it appears as a separate entry.
					// See TheoryOfAttemptStatistics.
					if ls.opts.OnAttemptTruncated != nil {
						if rerr := ls.opts.OnAttemptTruncated(phaseState, ls.state, summary); rerr != nil {
							generationErr = rerr
							break
						}
					}

					// Append the handoff prompt as the retry user input.
					if retryPrompt != "" {
						retryParts = append(retryParts, generators.Text(
							formatHandoffPrompt(retryPrompt, retry+1, ls.maxRetries)))
					}
					// The retry feedback is a round-triggering
					// feedback: it ends with the session tree
					// outline. See TheoryOfSessionTree.
					retryParts = append(retryParts, treeOutlinePart(ls.sessionTree, ls.sessionParent()))

					var appendErr error
					ls.state, appendErr = ls.state.AppendContent(&generators.Content{
						Role:  generators.RoleUser,
						Parts: retryParts,
					})
					if appendErr != nil {
						break
					}
					// The retry feedback joins the session tree as an
					// input node. See TheoryOfSessionTree.
					ls.writeFeedbackInputNode(retryParts)

					generationErr = nil
					continue
				}
			}
			break
		}

		// Always extract and remove summary blocks from
		// collectedBlocks. Summaries must be available to
		// OnAttemptSuccess regardless of whether retry is
		// enabled. See TheoryOfLoops. The prefetched futures are
		// carried along in lockstep, so each surviving block keeps
		// its own outcome; summary blocks never carry futures, so
		// their positions contribute nil entries only. See
		// components.TheoryOfReadOnlyPrefetch.
		generationSummaries = nil
		var remaining []blocks.Block
		var remainingFutures []components.PrefetchFuture
		for i, block := range collectedBlocks {
			if block.Kind == "summary" {
				generationSummaries = append(generationSummaries, block.Body)
				continue
			}
			remaining = append(remaining, block)
			if i < len(prefetchedFutures) {
				remainingFutures = append(remainingFutures, prefetchedFutures[i])
			} else {
				remainingFutures = append(remainingFutures, nil)
			}
		}
		collectedBlocks = remaining
		prefetchedFutures = remainingFutures

		// If retry is disabled, we're done with this generation.
		if !ls.opts.RetryOnMissingCompletion {
			break
		}

		// Check for completion: the summary block is the only
		// completion signal — no other block kind (ingest, shell,
		// continue, go-test, go-src) completes an attempt — and an
		// abnormal finish reason (e.g., "length" from max-token
		// truncation) overrides the summary signal and triggers
		// retry. See TheoryOfSummaryCompletionRetry in summarize_incomplete.go.
		hasCompletion := len(generationSummaries) > 0
		isAbnormalFinish := isAbnormalFinishReason(finishReason)

		if hasCompletion && !isAbnormalFinish {
			break
		}
		if retry >= ls.maxRetries {
			break
		}

		// Report the truncated attempt to the interaction recorder
		// and record the truncation as an event node, immediately,
		// before the handoff request. See TheoryOfLoopEvents.
		if ls.rec != nil && ls.rec.Enabled() {
			ls.rec.AttemptTruncated()
			if isAbnormalFinish {
				ls.rec.Event("decision", fmt.Sprintf("abnormal finish reason %q triggered retry: attempt %d/%d", finishReason, retry+1, ls.maxRetries))
			} else {
				ls.rec.Event("decision", fmt.Sprintf("missing completion (no summary block) triggered retry: attempt %d/%d", retry+1, ls.maxRetries))
			}
		}
		truncatedDetail := "missing completion (no summary block)"
		if isAbnormalFinish {
			truncatedDetail = fmt.Sprintf("abnormal finish reason %q", finishReason)
		}
		ls.writeEventNode("truncated", fmt.Sprintf("attempt %d truncated (%d/%d): %s",
			ls.attempt, ls.attemptInGeneration, ls.maxRetries, truncatedDetail))

		// Perform handoff summary on incomplete output if threshold met.
		// attemptBase is both the incomplete-output window and the
		// usage-injection window: the injection sums this attempt's
		// own last usage with the handoff spend, never a prior
		// attempt's. See TheoryOfHandoffUsageAccounting.
		summary := ""
		retryPrompt := ""
		if ls.opts.Handoff != nil {
			incompleteText := ExtractIncompleteOutput(phaseState, attemptBase)
			if incompleteText != "" {
				// Record the handoff request's start immediately.
				// See TheoryOfLoopEvents.
				ls.writeEventNode("handoff-start", "handoff started")
				handoff, rerr := ls.opts.Handoff(handoffInput(incompleteText, ls.sessionTree, ls.sessionParent()))
				if rerr == nil && handoff != nil {
					summary = handoff.Summary
					retryPrompt = handoff.Prompt
					// Record the produced handoff. See TheoryOfLoopEvents.
					ls.writeEventNode("handoff", "handoff summary:\n"+handoff.Summary)
					phaseState = appendHandoffUsage(phaseState, attemptBase, handoff.Usage)
				}
			}
		}

		// Record the attempt's token usage, including the injected
		// handoff spend. See TheoryOfUsageLogging.
		ls.recordAttemptUsage(phaseState, attemptBase, "")

		// Record the truncated attempt in attempt statistics.
		// See TheoryOfAttemptStatistics.
		if ls.opts.OnAttemptTruncated != nil {
			if rerr := ls.opts.OnAttemptTruncated(phaseState, ls.state, summary); rerr != nil {
				generationErr = rerr
				break
			}
		}

		// Append the retry feedback. The feedback always names the
		// reason and the attempt number: an abnormal finish reason
		// frames the retry as truncation; any other missing-summary
		// attempt is a rule violation — the model ended its response
		// without the mandatory summary block — so the feedback says
		// so explicitly. Blocks from the failed attempt were
		// discarded, so the model must re-emit every block it intends
		// to take effect, together with the summary block. The
		// handoff prompt, when one was produced, follows the prefix.
		// See TheoryOfLoops and TheoryOfSummaryCompletionRetry.
		prefixTemplate := incompleteOutputHandoffPrefix
		if !isAbnormalFinish {
			prefixTemplate = missingSummaryRetryPrefix
		}
		retryParts := []generators.Part{
			generators.Text(fmt.Sprintf(prefixTemplate, retry+1, ls.maxRetries)),
		}
		if retryPrompt != "" {
			retryParts = append(retryParts, generators.Text(retryPrompt))
		}
		// The retry feedback is a round-triggering feedback: it ends
		// with the session tree outline. See TheoryOfSessionTree.
		retryParts = append(retryParts, treeOutlinePart(ls.sessionTree, ls.sessionParent()))
		var appendErr error
		ls.state, appendErr = ls.state.AppendContent(&generators.Content{
			Role:  generators.RoleUser,
			Parts: retryParts,
		})
		if appendErr != nil {
			break
		}
		// The retry feedback joins the session tree as an input node.
		// See TheoryOfSessionTree.
		ls.writeFeedbackInputNode(retryParts)

		// The retry attempt opens on the next loop iteration: its
		// attempt-start node and OnAttemptStart hook fire there,
		// keeping every attempt's opening bookkeeping in one place.
		// See TheoryOfLoopEvents.
	}

	if generationErr != nil {
		ls.recordAttemptError(generationErr)
		if ls.opts.OnPhaseError != nil {
			phaseState = ls.opts.OnPhaseError(phaseState, generationErr)
		}
		ls.recordAttemptUsage(phaseState, attemptBase, "error")
		return generationResult{state: phaseState}, generationErr
	}

	// When the retry budget is exhausted and the final attempt still
	// produced no summary block, synthesize a summary from the
	// generation's output and append it to the state as a summary
	// block. The synthesis applies to every exhausted generation —
	// including generations whose blocks trigger components — because
	// the summary block is mandatory in every response: the attempt
	// statistics and the display front-end's Tree tab need the
	// completion signal. See TheoryOfLoops.
	if len(generationSummaries) == 0 && ls.opts.Handoff != nil {
		incompleteText := ExtractIncompleteOutput(phaseState, attemptBase)
		if incompleteText != "" {
			// Record the handoff request's start immediately.
			// See TheoryOfLoopEvents.
			ls.writeEventNode("handoff-start", "handoff started")
			if handoff, serr := ls.opts.Handoff(handoffInput(incompleteText, ls.sessionTree, ls.sessionParent())); serr == nil && handoff != nil {
				// Record the synthesized completion summary. See
				// TheoryOfLoopEvents.
				ls.writeEventNode("synthesized-summary", "synthesized completion summary:\n"+handoff.Summary)
				// Account the handoff request's own token spend so the
				// synthesized completion's attempt statistics and usage
				// line include it. The window starts at the final
				// attempt's base. See TheoryOfHandoffUsageAccounting.
				phaseState = appendHandoffUsage(phaseState, attemptBase, handoff.Usage)
				var appendErr error
				phaseState, appendErr = phaseState.AppendContent(&generators.Content{
					Role: generators.RoleLog,
					Parts: []generators.Part{
						generators.Text(FormatSummaryBlock(handoff.Summary)),
					},
				})
				if appendErr != nil {
					ls.recordAttemptError(appendErr)
					if ls.opts.OnPhaseError != nil {
						phaseState = ls.opts.OnPhaseError(phaseState, appendErr)
					}
					ls.recordAttemptUsage(phaseState, attemptBase, "error")
					return generationResult{state: phaseState}, appendErr
				}
				generationSummaries = append(generationSummaries, handoff.Summary)
			}
		}
	}

	// OnAttemptSuccess hook.
	if ls.opts.OnAttemptSuccess != nil {
		if serr := ls.opts.OnAttemptSuccess(phaseState, generationSummaries); serr != nil {
			// A disk-change failure at flush time ends the run: the
			// snapshot diverged, so a retry cannot repair it. See
			// TheoryOfDiskChangeHandoff.
			var flushDiskChanged *changes.DiskChangedError
			if errors.As(serr, &flushDiskChanged) {
				return generationResult{state: phaseState}, ls.endOnDiskChange(serr, phaseState, attemptBase)
			}
			ls.recordAttemptError(serr)
			ls.recordAttemptUsage(phaseState, attemptBase, "error")
			return generationResult{state: phaseState}, serr
		}
	}

	// Record the attempt's token usage and report the successfully
	// completed attempt to the interaction recorder and the session
	// tree. See TheoryOfUsageLogging and TheoryOfLoopEvents.
	ls.recordAttemptUsage(phaseState, attemptBase, "")
	if ls.rec != nil && ls.rec.Enabled() {
		ls.rec.AttemptCompleted(generationSummaries)
	}
	// The completed node carries the attempt's summary bodies, so the
	// Tree tab shows the completion with its summary collapsed by
	// default. See TheoryOfLoopEvents.
	completedContent := fmt.Sprintf("attempt %d complete", ls.attempt)
	if len(generationSummaries) > 0 {
		completedContent += ":\n" + strings.Join(generationSummaries, "\n")
	}
	ls.writeEventNode("completed", completedContent)

	// The successful attempt joins the session tree: the response
	// node, one summary node per summary body, and the block batch
	// (handled plus collected). Blocks whose parent names a node an
	// earlier block of the batch creates are deferred; the runner
	// writes them after the components have produced the named nodes.
	// A naming fault discards the block batch and is fed back through
	// the shared correction decision below. See TheoryOfSessionTree.
	blockNodeNames, deferredBlockIndexes := ls.recordAttemptTree(phaseState, attemptBase, generationSummaries, handledBlocks, collectedBlocks)

	ls.state = phaseState

	// Correction feedback: parse errors, unknown block kinds, and
	// session-tree naming errors are all unprocessable output — a
	// malformed block cannot be parsed, a well-formed block of an
	// unavailable kind cannot take effect, a mis-named block batch
	// cannot be recorded — so they share one decision and one
	// correction budget. Unknown kinds are computed from the
	// collected blocks; summary blocks were already extracted above,
	// so the summary kind never reaches the predicate. The naming
	// errors were stored by recordAttemptTree and are consumed here.
	// See TheoryOfUnknownBlockKinds and TheoryOfSessionTree.
	var unknownKinds []blocks.Block
	if ls.opts.KnownBlockKinds != nil {
		unknownKinds = unknownKindBlocks(collectedBlocks, ls.opts.KnownBlockKinds)
	}
	// The attempt's unprocessable output joins the tree as an error
	// node under the current response, regardless of the correction
	// budget: the node is the record; the budget governs only whether
	// the model is asked to correct. See TheoryOfSessionTree.
	ls.recordAttemptErrorNodes(generationParseErrors, unknownKinds)
	var correctionParts []generators.Part
	var generationUncorrected []*blocks.BlockParseError
	correctionParts, ls.parseErrorCorrections, ls.skipOnAttemptStart, generationUncorrected =
		decideBlockCorrectionFeedback(generationParseErrors, unknownKinds, ls.namingErrs, ls.parseErrorCorrections)
	ls.namingErrs = nil
	if len(generationUncorrected) > 0 {
		ls.uncorrectedParseErrors = appendUncorrectedParseErrors(ls.uncorrectedParseErrors, generationUncorrected)
	}
	if ls.rec != nil && ls.rec.Enabled() {
		if len(correctionParts) > 0 {
			ls.rec.Event("decision", fmt.Sprintf("block correction attempt %d/%d: %d malformed block(s) and %d unavailable-kind block(s) fed back to the model", ls.parseErrorCorrections, maxParseErrorCorrections, len(generationParseErrors), len(unknownKinds)))
		} else if len(generationUncorrected) > 0 {
			ls.rec.Event("decision", fmt.Sprintf("parse error correction budget exhausted: %d malformed block(s) recorded as uncorrected", len(generationUncorrected)))
		}
	}

	// Single-shot mode: no component processing.
	if len(ls.opts.Components) == 0 {
		if len(correctionParts) > 0 {
			feedbackParts := correctionParts
			// The feedback closes with the session tree outline, so
			// the model sees the session's structure. See
			// TheoryOfSessionTree.
			feedbackParts = append(feedbackParts, treeOutlinePart(ls.sessionTree, ls.sessionParent()))
			var aerr error
			ls.state, aerr = ls.state.AppendContent(&generators.Content{
				Role:  generators.RoleUser,
				Parts: feedbackParts,
			})
			if aerr != nil {
				ls.recordAttemptError(aerr)
				return generationResult{state: ls.state}, aerr
			}
			ls.writeFeedbackInputNode(feedbackParts)
			ls.writeEventNode("continue", fmt.Sprintf("attempt %d continues: %s",
				ls.attempt,
				buildContinueReason(nil,
					len(generationParseErrors) > 0,
					len(unknownKinds) > 0)))
			return generationResult{
				state:        ls.state,
				summaries:    generationSummaries,
				continueNext: true,
			}, nil
		}
		return generationResult{
			state:       ls.state,
			summaries:   generationSummaries,
			finalBlocks: collectedBlocks,
		}, nil
	}

	// Process components. The prefetched futures travel with the
	// collected blocks, so each component consumes its blocks' own
	// outcomes in block order; the failed attempt's futures were
	// discarded with its blocks at the attempt reset. The session
	// tree is threaded through the tree-writing components. See
	// components.TheoryOfReadOnlyPrefetch and TheoryOfSessionTree.
	var generationRemaining []blocks.Block
	var combinedParts []generators.Part
	var outputs []components.ComponentOutput
	var treeOut *tree.Tree
	var triggered bool
	var cerr error
	generationRemaining, ls.state, combinedParts, outputs, treeOut, triggered, cerr = components.ProcessComponents(
		ls.ctx, ls.opts.Components, collectedBlocks, ls.state,
		ls.opts.Root, ls.opts.HTTPClient,
		ls.sessionTree,
		prefetchedFutures...,
	)
	if cerr != nil {
		ls.recordAttemptError(cerr)
		return generationResult{state: ls.state}, cerr
	}
	ls.remainingBlocks = append(ls.remainingBlocks, generationRemaining...)

	// The deferred block nodes are written now: their parents name
	// nodes the batch's new-plan and response components created, and
	// the components have run, so the named nodes exist. See
	// TheoryOfSessionTree.
	treeOut = writeDeferredBlockNodes(treeOut, ls.currentResponse, collectedBlocks, blockNodeNames, deferredBlockIndexes)
	// Block-result nodes attach to the block nodes written at the
	// attempt's success: one result child per block when the
	// component produced one part per block, a shared result node
	// otherwise. See TheoryOfSessionTree.
	ls.sessionTree = writeBlockResultNodes(treeOut, outputs, blockNodeNames)

	if len(correctionParts) > 0 {
		combinedParts = append(correctionParts, combinedParts...)
		triggered = true
	}

	if triggered {
		// The continue reason states why the next generation starts:
		// the kinds of blocks processed by components, the correction
		// feedback, or a component's state modification. A user-part
		// count is misleading here: a component that triggers through
		// a state modification alone (e.g., ingest appending fetched
		// content to the state) appends no parts, so the count reads
		// as 0. See TheoryOfLoops.
		matchedKinds := make(map[string]bool)
		for _, block := range collectedBlocks {
			matchedKinds[block.Kind] = true
		}
		for _, block := range generationRemaining {
			delete(matchedKinds, block.Kind)
		}
		var triggeredKinds []string
		for _, comp := range ls.opts.Components {
			if comp.Process != nil && matchedKinds[comp.Kind] {
				matchedKinds[comp.Kind] = false
				triggeredKinds = append(triggeredKinds, comp.Kind)
			}
		}
		continueReason := buildContinueReason(triggeredKinds,
			len(generationParseErrors) > 0,
			len(unknownKinds) > 0)
		// The feedback closes with the session tree outline, so the
		// model sees the session's structure. See TheoryOfSessionTree.
		combinedParts = append(combinedParts, treeOutlinePart(ls.sessionTree, ls.sessionParent()))
		if len(combinedParts) > 0 {
			var aerr error
			ls.state, aerr = ls.state.AppendContent(&generators.Content{
				Role:  generators.RoleUser,
				Parts: combinedParts,
			})
			if aerr != nil {
				ls.recordAttemptError(aerr)
				return generationResult{state: ls.state}, aerr
			}
			ls.writeFeedbackInputNode(combinedParts)
		}
		if ls.rec != nil && ls.rec.Enabled() {
			ls.rec.Event("decision", continueReason)
		}
		ls.writeEventNode("continue", fmt.Sprintf("attempt %d continues: %s",
			ls.attempt, continueReason))
		return generationResult{
			state:        ls.state,
			summaries:    generationSummaries,
			parts:        combinedParts,
			continueNext: true,
		}, nil
	}

	if ls.opts.OnIdle != nil {
		var idleContinue bool
		// The content count before the idle handler runs bounds the
		// user-input extraction: only the handler's delta is recorded
		// as the input node. See TheoryOfSessionTree.
		prevCount := generators.CountContents(ls.state)
		ls.state, idleContinue, cerr = ls.opts.OnIdle(ls.ctx, ls.state)
		if cerr != nil {
			ls.recordAttemptError(cerr)
			return generationResult{state: ls.state}, cerr
		}
		if idleContinue {
			ls.recordIdleUserInput(ls.state, prevCount)
			if ls.rec != nil && ls.rec.Enabled() {
				ls.rec.Event("decision", "idle handler returned user input; starting a new generation")
			}
			ls.writeEventNode("idle", "idle input received; starting the next generation")
			return generationResult{
				state:        ls.state,
				summaries:    generationSummaries,
				continueNext: true,
			}, nil
		}
	}

	return generationResult{
		state:     ls.state,
		summaries: generationSummaries,
	}, nil
}

// describeRequest renders the actual generation parameters of one
// request as the request event node's content: the resolved spec path,
// the model identity, and the effective temperature, reasoning effort,
// and token limits. The spec path is the full resolved generator path
// (Spec.Name after resolveSpec, e.g. "google/flash"); specs constructed
// without resolution (built-in shortcuts, the ollama shorthand) carry
// no path and omit the field. The effective values mirror the
// generators' flag-over-spec precedence — the -temperature and -effort
// flags override the spec fields (see Gemini.Generate and
// OpenAI.Generate) — so the node's content reports the values the
// request actually carries, unlike the generators' "generating" log,
// which records the spec's effort even when the flag overrides it. Max
// generate tokens come from the spec: every built-in command passes nil
// GenerateOptions, so the spec field is the effective limit;
// flags.MaxTokens bounds only the input budget and is not part of the
// request. Unset values are omitted from the detail. See
// TheoryOfLoopEvents.
func describeRequest(
	spec generators.Spec,
	temperatureFlag generators.TemperatureFlag,
	effortFlag generators.EffortFlag,
) string {
	var parts []string
	if spec.Name != "" {
		parts = append(parts, fmt.Sprintf("spec %s", spec.Name))
	}
	parts = append(parts, fmt.Sprintf("model %s", spec.Model))
	if spec.Family != "" {
		parts = append(parts, fmt.Sprintf("family %s", spec.Family))
	}
	if temperatureFlag.Value != nil {
		parts = append(parts, fmt.Sprintf("temperature %g", *temperatureFlag.Value))
	} else if spec.Temperature != nil {
		parts = append(parts, fmt.Sprintf("temperature %g", *spec.Temperature))
	}
	if effortFlag != "" {
		parts = append(parts, fmt.Sprintf("effort %s", effortFlag))
	} else if spec.ReasoningEffort != "" {
		parts = append(parts, fmt.Sprintf("effort %s", spec.ReasoningEffort))
	}
	if spec.MaxGenerateTokens != nil {
		parts = append(parts, fmt.Sprintf("max tokens %d", *spec.MaxGenerateTokens))
	}
	if spec.MaxThinkingTokens != nil {
		parts = append(parts, fmt.Sprintf("thinking tokens %d", *spec.MaxThinkingTokens))
	}
	if spec.ContextTokens > 0 {
		parts = append(parts, fmt.Sprintf("context %d", spec.ContextTokens))
	}
	return strings.Join(parts, ", ")
}

// recordAttemptUsage records the aggregated token usage of one attempt:
// as a usage event node in the session tree (the display source for a
// live consumer) and as a "usage" log entry. Attempts that record no
// token usage emit nothing. Streaming attempts additionally append the
// measured timing fragment to the node's content; unmeasured usages
// leave it out. See TheoryOfUsageLogging and TheoryOfLoopEvents.
func (ls *loopState) recordAttemptUsage(state generators.State, attemptBaseCount int, outcome string) {
	// The usage is the last Usage part among the contents appended since
	// the attempt started, not a sum of streaming snapshots.
	// See TheoryOfUsageLogging.
	usage := extractLastUsage(state, attemptBaseCount)
	if usage.Prompt.TokenCount == 0 &&
		usage.Prompt.TokenCountCached == 0 &&
		usage.Candidates.TokenCount == 0 &&
		usage.Thoughts.TokenCount == 0 {
		return
	}
	outcomeSuffix := ""
	if outcome != "" {
		outcomeSuffix = " (" + outcome + ")"
	}
	// SpeedSuffix carries the streaming ttft and average generation
	// speed when measured, staying empty for unmeasured usages.
	// See TheoryOfUsageTiming.
	ls.writeEventNode("usage", fmt.Sprintf("attempt %d usage%s: prompt %d, cached %d, completion %d, thoughts %d",
		ls.attempt, outcomeSuffix,
		usage.Prompt.TokenCount,
		usage.Prompt.TokenCountCached,
		usage.Candidates.TokenCount,
		usage.Thoughts.TokenCount,
	)+usage.SpeedSuffix())
	args := []any{
		"attempt", ls.attempt,
		"prompt", usage.Prompt.TokenCount,
		"cached", usage.Prompt.TokenCountCached,
		"completion", usage.Candidates.TokenCount,
		"thoughts", usage.Thoughts.TokenCount,
	}
	if outcome != "" {
		args = append([]any{"outcome", outcome}, args...)
	}
	// One-decimal string fields keep the fractional digit visible in the
	// text handler output; float values would print "30" instead of
	// "30.0". See TheoryOfUsageTiming.
	if usage.HasSpeed() {
		args = append(args,
			"ttft_seconds", fmt.Sprintf("%.1f", usage.TimeToFirstToken.Seconds()),
			"tokens_per_second", fmt.Sprintf("%.1f",
				float64(usage.GeneratedTokens())/usage.GenerateDuration.Seconds()),
		)
	}
	ls.logger.InfoContext(ls.ctx, "usage", args...)
}

// recordAttemptError reports a failed attempt to the interaction
// recorder when recording is active.
func (ls *loopState) recordAttemptError(err error) {
	if ls.rec != nil && ls.rec.Enabled() {
		ls.rec.AttemptError(err)
	}
}

// endOnDiskChange terminates the run on a disk-change failure: it
// condenses the interrupted output into a handoff when one is available,
// records the failed attempt, and returns the terminal error the goal
// runner forwards to the next loop. See TheoryOfDiskChangeHandoff.
func (ls *loopState) endOnDiskChange(err error, phaseState generators.State, attemptBase int) *DiskChangeHandoffError {
	var handoff *Handoff
	if ls.opts.Handoff != nil {
		incompleteText := ExtractIncompleteOutput(phaseState, attemptBase)
		if incompleteText != "" {
			ls.writeEventNode("handoff-start", "handoff started")
			if h, herr := ls.opts.Handoff(handoffInput(incompleteText, ls.sessionTree, ls.sessionParent())); herr == nil && h != nil {
				handoff = h
				ls.writeEventNode("handoff", "handoff summary:\n"+h.Summary)
				phaseState = appendHandoffUsage(phaseState, attemptBase, h.Usage)
			}
		}
	}
	ls.recordAttemptError(err)
	ls.recordAttemptUsage(phaseState, attemptBase, "error")
	return &DiskChangeHandoffError{Err: err, Handoff: handoff}
}

// finishWithError fills the result with the final state and yields the
// terminal error, ending the run. The caller must return immediately
// after the call.
func (ls *loopState) finishWithError(err error, finalState generators.State) {
	ls.result.FinalState = finalState
	ls.result.RemainingBlocks = ls.remainingBlocks
	ls.result.ParseErrors = ls.uncorrectedParseErrors
	// The result carries the run's final session tree so callers
	// outside the loop can extract subtree projections from it. See
	// TheoryOfSessionTree.
	ls.result.SessionTree = ls.sessionTree
	ls.runErr = err
	// The terminal error joins the tree as an event node before the
	// final yield, so the record carries it even when the consumer
	// stops at the terminal yield. See TheoryOfLoopEvents.
	if ls.sessionTree != nil {
		if next, _, werr := ls.sessionTree.WriteAuto(ls.sessionParent(), "run-error", tree.TypeEvent, tree.AuthorProgram, "run error: "+err.Error()); werr == nil {
			ls.sessionTree = next
		}
	}
	ls.emitTerminalTree(err)
}

// finish fills the result with the final state and ends the run without
// an error.
func (ls *loopState) finish(finalState generators.State, finalBlocks []blocks.Block) {
	ls.result.FinalState = finalState
	ls.result.RemainingBlocks = finalBlocks
	ls.result.ParseErrors = ls.uncorrectedParseErrors
	// The result carries the run's final session tree so callers
	// outside the loop can extract subtree projections from it. See
	// TheoryOfSessionTree.
	ls.result.SessionTree = ls.sessionTree
}

// BlockHandler processes a block during streaming. If consumed is true,
// the block is not passed to ProcessComponents. If err is non-nil,
// streaming stops immediately. See TheoryOfLoops.
type BlockHandler func(block blocks.Block) (consumed bool, err error)

type InteractionRecorder interface {
	// Enabled reports whether recording is active. When false, the loop
	// does not wrap the state, record contents, or call the lifecycle
	// methods.
	Enabled() bool
	// StartSession begins a recording session for the given command.
	// Called once when the loop starts.
	StartSession(command string)
	// EndSession closes the current session with the given outcome.
	// A non-nil error marks the session as failed.
	EndSession(err error)
	// SystemPrompt records the session's system prompt. Called once when
	// the loop starts.
	SystemPrompt(prompt string)
	// AttemptStart marks the beginning of a generation attempt: one
	// pass through the phase chain, numbered within its generation.
	AttemptStart()
	// AttemptCompleted marks an attempt that completed normally,
	// carrying the summary block bodies.
	AttemptCompleted(summaries []string)
	// AttemptTruncated marks an attempt that ended without a completion
	// signal (no summary block or abnormal finish reason) and was
	// retried.
	AttemptTruncated()
	// AttemptError marks an attempt that failed with an error.
	AttemptError(err error)
	// Content records a content appended to the generation state.
	Content(content *generators.Content)
	// Block records a structured block parsed from the model output.
	Block(block blocks.Block)
	// ParseError records a malformed block that could not be parsed.
	ParseError(parseErr *blocks.BlockParseError)
	// Event records an arbitrary session event with the current attempt
	// number, carrying a type and a free-form detail. The generation
	// loop uses it for flow decisions (retries, parse-error
	// corrections, component-triggered generations, session metadata),
	// and generator implementations use it through the dscope-injected
	// EventRecorder for API-level events (api_call, api_error). The
	// transcript renders each event by its type.
	// See records.TheoryOfEventRecording.
	Event(typ string, detail string)
}

type RunOptions struct {
	// Generator is the model used for generation.
	Generator generators.Generator
	// InitialState is the starting state (without ParserState wrapping).
	// Run wraps it with ParserState internally.
	InitialState generators.State
	// StateDecorators wrap the state before the loop starts, in order.
	// Each decorator receives the state produced by the previous one.
	// The default is none; commands that need to observe state (e.g., the
	// TUI observing output content) pass their own implementations.
	// See StateDecorator.
	StateDecorators []StateDecorator
	// Components is the component set for block processing between
	// generations. When empty, the loop runs a single generation
	// (single-shot mode).
	Components components.ComponentSet
	// BlockHandler processes blocks during streaming. May be nil.
	// If consumed is true, the block is not passed to ProcessComponents.
	BlockHandler BlockHandler
	// KnownBlockKinds reports whether a block kind is processable in
	// this session. When non-nil, the loop checks every collected block
	// — after summary extraction, so the summary kind never reaches the
	// predicate — and feeds back a correction error for each block whose
	// kind the predicate rejects, so a model emitting a kind the session
	// cannot process (an unknown or disabled kind) is corrected instead
	// of silently ignored; the feedback shares the parse-error
	// correction budget. When nil, no unknown-kind check happens and
	// collected blocks are trusted. Callers derive the predicate from
	// their ComponentSet via ComponentSet.KnownKinds, adding kinds
	// processed outside the component loop. See
	// TheoryOfUnknownBlockKinds.
	KnownBlockKinds func(kind string) bool
	// PhaseBuilder builds the phase chain for each generation.
	PhaseBuilder func(generators.Generator) generators.Phase
	// Root is the filesystem root for ProcessComponents. Optional.
	Root *os.Root
	// HTTPClient is the HTTP client for ProcessComponents. Optional.
	HTTPClient nets.HTTPClient
	// MaxGenerations limits the number of generations. 0 means
	// unlimited.
	MaxGenerations int

	// InteractionRecorder receives generation events (contents, blocks,
	// attempt lifecycle) for interaction recording and self-improvement
	// analysis. When nil, the Recorder provider default is used (see the
	// InteractionRecorder provider in this package).
	// See records.TheoryOfInteractionRecording.
	InteractionRecorder InteractionRecorder

	// Command identifies the invoking command (e.g., "ai", "next"). It is
	// recorded as the session's command name when interaction recording
	// is active. When empty, "codes" is used.
	// See records.TheoryOfInteractionRecording.
	Command string

	// OnAttemptStart is called before each attempt (including retries).
	// Used to reset per-attempt state (e.g., MemoryStore.Reset).
	OnAttemptStart func()

	// OnAttemptSuccess is called after a successful attempt, before
	// component processing. If it returns an error, the loop stops.
	// Used to flush per-attempt state (e.g., MemoryStore.Flush) and
	// collect attempt-level metadata (e.g., token statistics).
	// summaries contains summary block bodies extracted from the attempt.
	OnAttemptSuccess func(state generators.State, summaries []string) error

	// OnAttemptTruncated is called when an attempt is truncated (no
	// summary block or abnormal finish reason) and will be retried. It
	// receives the state with the truncated output, the state that will
	// be the base for the retry attempt, and the synthesized summary of
	// the truncated output. The callback records the truncated attempt
	// in attempt statistics. Unlike OnAttemptSuccess, it must not flush
	// per-attempt state (e.g., MemoryStore) because the truncated
	// attempt's changes are discarded. See TheoryOfLoops.
	OnAttemptTruncated func(truncatedState generators.State, retryBaseState generators.State, summary string) error

	// OnPhaseError is called when a phase returns an error, before
	// the loop stops. The returned state is included in the Result.
	// Used for error logging, tapping, or appending error content.
	OnPhaseError func(state generators.State, err error) generators.State

	// RetryOnMissingCompletion enables retry when no summary block is
	// found in the collected blocks after an attempt, or when the finish
	// reason indicates abnormal termination (e.g., "length" from
	// max-token truncation). The summary block is the mandatory
	// completion signal — no other block kind (ingest, shell, continue,
	// go-test, go-src) replaces or implies it — so every attempt missing
	// a summary block is retried, including attempts whose blocks
	// trigger components. See TheoryOfSummaryCompletionRetry in
	// summarize_incomplete.go.
	RetryOnMissingCompletion bool
	// RetryOnError enables retry when any error occurs after the model
	// has output content during an attempt. The loop summarizes the
	// incomplete output (using Handoff if available),
	// appends both the error context and the summary as user content,
	// resets per-attempt state via OnAttemptStart (which resets the
	// MemoryStore), and retries from the updated state. Errors that
	// occur before any content is output do not trigger retry. See
	// TheoryOfLoops.
	RetryOnError bool
	// MaxRetries limits retries per generation when
	// RetryOnMissingCompletion or RetryOnError is true. Defaults to 3
	// when either is true and MaxRetries is 0.
	MaxRetries int
	// Handoff summarizes incomplete output into a self-contained handoff
	// before retrying. If output is below the threshold or handoff is nil,
	// retry proceeds directly.
	Handoff func(incompleteText string) (*Handoff, error)

	// OnIdle is called when no component triggers after a generation. It
	// allows the caller to provide interactive input (e.g., chat prompt)
	// and decide whether to continue with another generation. If OnIdle
	// returns continue=true, a new generation starts. If false or OnIdle
	// is nil, the loop ends. OnIdle is only invoked in multi-generation
	// mode (when Components is non-empty). See TheoryOfIdleHandler.
	OnIdle IdleHandler
}

// formatHandoffPrompt formats the retry user prompt with the handoff content.
// See TheoryOfHandoff.
func formatHandoffPrompt(retryPrompt string, attempt, maxAttempts int) string {
	return fmt.Sprintf(incompleteOutputHandoffPrefix, attempt, maxAttempts) + retryPrompt
}

// Result holds the outcome of a generation loop.
type Result struct {
	// FinalState is the state after the last generation (without
	// ParserState).
	FinalState generators.State
	// RemainingBlocks are blocks not matched by any component.
	RemainingBlocks []blocks.Block
	// ParseErrors lists blocks that could not be parsed and were not
	// corrected within the maxParseErrorCorrections correction budget.
	// In unattended operation, callers (e.g., the goal runner) can
	// inspect this to detect silent change loss from persistently
	// malformed model output. See TheoryOfLoops.
	ParseErrors []*blocks.BlockParseError
	// SessionTree is the run's final session tree: every operation of
	// the run — inputs, responses, summaries, blocks, results, errors
	// — is a node, and callers outside the loop extract subtree
	// projections from it. See TheoryOfSessionTree.
	SessionTree *tree.Tree
	// Diffs are the session diffs of all changes applied through the
	// in-memory file store during this run. They are used by the review
	// loop to present the changes to a second model. See
	// TheoryOfReviewLoop in generate.go.
	Diffs []changes.FileDiff
}

// RecordState reports the given state's system prompt and contents to the
// interaction recorder and returns a state that captures future appends.
// When the recorder is nil or disabled, the state is returned unchanged
// and enabled is false. Commands that run phases outside the loop (e.g.,
// ping) use this to participate in interaction recording.
// See records.TheoryOfInteractionRecording.
func RecordState(recorder InteractionRecorder, state generators.State) (generators.State, bool) {
	if recorder == nil || !recorder.Enabled() {
		return state, false
	}
	recorder.SystemPrompt(state.SystemPrompt())
	for content := range state.Contents() {
		recorder.Content(content)
	}
	return recordedState{upstream: state, recorder: recorder}, true
}

// recordedState is a State layer that reports appended contents to an
// InteractionRecorder. It sits below ParserState so every content append
// (user input, model output, reasoning thoughts, tool calls, retry
// feedback) is captured for interaction recording. State immutability is
// preserved: AppendContent and Flush return a new recordedState.
// See records.TheoryOfInteractionRecording.
type recordedState struct {
	upstream generators.State
	recorder InteractionRecorder
}

func (s recordedState) Unwrap() generators.State {
	return s.upstream
}

func (s recordedState) Flush() (generators.State, error) {
	newUpstream, err := s.upstream.Flush()
	if err != nil {
		return nil, err
	}
	return recordedState{upstream: newUpstream, recorder: s.recorder}, nil
}

func (s recordedState) Functions() iter.Seq[*generators.Function] {
	return s.upstream.Functions()
}

func (s recordedState) SystemPrompt() string {
	return s.upstream.SystemPrompt()
}

func (s recordedState) Contents() iter.Seq[*generators.Content] {
	return s.upstream.Contents()
}

var _ generators.State = recordedState{}

func (s recordedState) AppendContent(content *generators.Content) (generators.State, error) {
	s.recorder.Content(content)
	newUpstream, err := s.upstream.AppendContent(content)
	if err != nil {
		return nil, err
	}
	return recordedState{upstream: newUpstream, recorder: s.recorder}, nil
}

func (Module) Run(
	recorder InteractionRecorder,
	logger logs.Logger,
	temperatureFlag generators.TemperatureFlag,
	effortFlag generators.EffortFlag,
	continuation SessionTreeContinuation,
) Run {
	return func(ctx context.Context, opts RunOptions, result *Result) iter.Seq2[*tree.Tree, error] {
		if result == nil {
			result = &Result{}
		}
		return func(yield func(*tree.Tree, error) bool) {
			// Determine the active interaction recorder. When the caller does
			// not pass one explicitly, the provider-injected default is used,
			// so every loop run records interactions automatically.
			// See records.TheoryOfInteractionRecording.
			rec := opts.InteractionRecorder
			if rec == nil {
				rec = recorder
			}
			opts.InteractionRecorder = rec

			// The loop state carries the mutable state of the run. Every
			// event write yields the full session tree through the
			// guarded emitTree/emitTerminalTree methods. The temperature
			// and effort flag values feed the request event node's
			// content; they are dscope provided, captured here like the
			// logger. See TheoryOfLoopEvents.
			ls := &loopState{
				ctx:             ctx,
				opts:            opts,
				rec:             rec,
				result:          result,
				yield:           yield,
				state:           opts.InitialState,
				maxRetries:      opts.MaxRetries,
				logger:          logger,
				temperatureFlag: temperatureFlag,
				effortFlag:      effortFlag,
			}
			if ls.maxRetries == 0 && (opts.RetryOnMissingCompletion || opts.RetryOnError) {
				ls.maxRetries = defaultMaxRetries
			}

			// One run, one tree: a fresh Run builds the tree under the
			// tree's root; a continued Run — a goal loop — receives the
			// run's tree and the loop node the goal runner prepared, and
			// writes every session node of the loop under that node. The
			// session's system prompt and initial input join under the
			// session root either way. See TheoryOfSessionTree and
			// TheoryOfGoalMode.
			if continuation.Tree != nil {
				ls.sessionTree = continuation.Tree
				ls.sessionRoot = continuation.Parent
			} else {
				ls.sessionTree = tree.New()
				ls.sessionRoot = "root"
			}
			ls.sessionTree = writeInitialTreeNodes(ls.sessionTree, ls.sessionRoot, opts.InitialState)
			// The initial tree — system prompt and input under the
			// session root — is yielded before the first generation, so
			// a consumer renders the session from its first pull. See
			// TheoryOfLoopEvents.
			ls.emitTree()

			recording := rec != nil && rec.Enabled()
			if recording {
				command := opts.Command
				if command == "" {
					command = "codes"
				}
				rec.StartSession(command)
				// EndSession is deferred so every return path — including
				// errors — closes the session with the final outcome. The
				// deferred function runs when the iterator is exhausted,
				// whether the consumer pulled the terminal error and
				// resumed, or stopped the iteration early.
				defer func() {
					rec.EndSession(ls.runErr)
				}()
				// Session-level metadata is recorded as events so the
				// transcript identifies the invocation and the model that
				// powered it. See records.TheoryOfEventRecording.
				rec.Event("decision", fmt.Sprintf("command line: %s", strings.Join(os.Args, " ")))
				if opts.Generator != nil {
					spec := opts.Generator.Spec()
					rec.Event("decision", fmt.Sprintf("generator selected: name=%q model=%q family=%q effort=%q",
						spec.Name, spec.Model, spec.Family, spec.ReasoningEffort))
				}
			}

			// Report the initial system prompt and contents, then wrap the
			// state so every subsequent content append is captured for
			// interaction recording. The recordedState layer sits below
			// ParserState so both the parsed blocks and the contents
			// carrying them are recorded. Recording is skipped entirely when
			// the recorder is nil or disabled.
			// See records.TheoryOfInteractionRecording.
			ls.state, _ = RecordState(rec, ls.state)

			// Apply the state decorators after recording so decorations (e.g.,
			// observing output content for a TUI) see every subsequent
			// content append. Decorators are applied in order, each wrapping
			// the state produced by the previous one. See StateDecorator.
			for _, decorator := range opts.StateDecorators {
				if decorator != nil {
					ls.state = decorator(ls.state)
				}
			}

			// Thought summaries produced during generation join the
			// session tree: bind the ThoughtsSummarize layer's emitter,
			// when the command wrapped one, to the guarded yield. Each
			// summary is recorded as a thought-summary event node and the
			// full tree is yielded. Summaries are produced synchronously
			// inside phase execution on this goroutine, so the reentrant
			// yield is safe. See TheoryOfLoopEvents and
			// TheoryOfThoughtsSummarize.
			installThoughtSummaryEmitter(ls.state, func(summary string) {
				ls.writeEventNode("thought-summary", "thought summary:\n"+summary)
			})

			// The main loop: each iteration is one generation. A
			// generation produces a summary and parts; when parts exist,
			// the next generation starts. The generation's occurrences
			// are recorded as event nodes and the full tree is yielded by
			// runGeneration through the guarded yield; usage recording
			// lives inside runGeneration, scoped to each attempt. See
			// TheoryOfLoops and TheoryOfLoopEvents.
			for generation := 1; opts.MaxGenerations == 0 || generation <= opts.MaxGenerations; generation++ {
				outcome, err := ls.runGeneration()
				if err != nil {
					ls.finishWithError(err, outcome.state)
					return
				}
				if outcome.continueNext {
					continue
				}
				if outcome.finalBlocks != nil {
					ls.finish(outcome.state, outcome.finalBlocks)
				} else {
					ls.finish(ls.state, ls.remainingBlocks)
				}
				return
			}
			ls.finish(ls.state, ls.remainingBlocks)
		}
	}
}

// ExtractIncompleteOutput collects Text and Thought parts from contents
// appended after prevCount, returning them as a single string for
// summarization. It is shared by the pipeline's retry summarization
// (handoffRetryState) and the loop's own retry paths.
func ExtractIncompleteOutput(state generators.State, prevCount int) string {
	var parts []string
	i := 0
	for c := range state.Contents() {
		if i < prevCount {
			i++
			continue
		}
		for _, p := range c.Parts {
			switch p := p.(type) {
			case generators.Text:
				parts = append(parts, string(p))
			case generators.Thought:
				parts = append(parts, string(p))
			}
		}
		i++
	}
	return strings.Join(parts, "\n")
}

// extractFinishReason scans new contents (after prevCount) for FinishReason
// parts and returns the last finish reason found. Used to detect abnormal
// termination such as max-token truncation ("length"). See
// TheoryOfSummaryCompletionRetry in generate.go.
func extractFinishReason(state generators.State, prevCount int) string {
	var reason string
	i := 0
	for c := range state.Contents() {
		if i >= prevCount {
			for _, p := range c.Parts {
				if fr, ok := p.(generators.FinishReason); ok {
					reason = string(fr)
				}
			}
		}
		i++
	}
	return reason
}

// abnormalFinishReasons lists finish reasons that indicate the output was
// truncated or ended abnormally, warranting a retry with content
// summarization. "length" (OpenAI) and "max_tokens" (some providers) mean the
// model hit the output token limit. The comparison is case-insensitive.
var abnormalFinishReasons = map[string]bool{
	"length":     true,
	"max_tokens": true,
}

// isAbnormalFinishReason reports whether the finish reason indicates
// the output was truncated or otherwise ended abnormally, warranting a
// retry with content summarization. See TheoryOfSummaryCompletionRetry in
// generate.go.
func isAbnormalFinishReason(reason string) bool {
	return abnormalFinishReasons[strings.ToLower(reason)]
}

// formatParseErrors formats collected parse errors as user content fed
// back to the model for self-correction. The message states that only the
// listed blocks were not applied and must be re-emitted, so the model does
// not re-emit already-applied blocks (which would duplicate
// ADD_BEFORE/ADD_AFTER changes). The attempt number makes the correction
// budget explicit so the model knows when it is on its final attempt and
// that persistently malformed blocks will be silently dropped. The full
// error text — block kind, delimiter, collision hints, and partial
// content — gives the model a concrete target for correction. After the
// correction, the model must resume the original task: the correction
// round is part of the same work, and ending after the fix would strand
// the remaining plan in a fresh goal loop with no other context. See
// TheoryOfParseErrorCollection and TheoryOfLoops.
func formatParseErrors(errors []*blocks.BlockParseError, attempt, maxAttempts int) string {
	var sb strings.Builder
	sb.WriteString("[System note: The following blocks in your previous output could not be parsed and were not applied. Re-emit ONLY the corrected versions of these blocks. Do NOT re-emit any other blocks — they were applied successfully and re-emitting them would duplicate changes. ")
	fmt.Fprintf(&sb, "This is correction attempt %d of %d; if the corrected blocks remain malformed after the final attempt, they will be silently dropped. ", attempt, maxAttempts)
	sb.WriteString("After re-emitting the corrected blocks, CONTINUE the original task exactly where it stopped before the malformed blocks: the correction is not the completion of the task. Then end your response with a summary block.]\n\n")
	for _, parseErr := range errors {
		sb.WriteString(parseErr.Error())
		sb.WriteString("\n\n")
	}
	return sb.String()
}

// appendUncorrectedParseErrors appends parse errors to the accumulated
// uncorrected list, skipping errors already recorded from previous
// generations. A model that fails to correct tends to repeat the same
// malformed block; deduplication keeps Result.ParseErrors concise.
func appendUncorrectedParseErrors(
	accumulated []*blocks.BlockParseError,
	generationErrors []*blocks.BlockParseError,
) []*blocks.BlockParseError {
	for _, parseErr := range generationErrors {
		duplicate := false
		for _, existing := range accumulated {
			if existing.Boundary == parseErr.Boundary &&
				existing.BlockKind == parseErr.BlockKind &&
				existing.Content == parseErr.Content {
				duplicate = true
				break
			}
		}
		if !duplicate {
			accumulated = append(accumulated, parseErr)
		}
	}
	return accumulated
}
