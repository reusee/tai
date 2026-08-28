package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/reusee/dscope"
	"github.com/reusee/tai/changes"
	"github.com/reusee/tai/components"
	"github.com/reusee/tai/flags"
	"github.com/reusee/tai/generators"
	"github.com/reusee/tai/logs"
	"github.com/reusee/tai/nets"
	"github.com/reusee/tai/pipeline/codetypes"
	"github.com/reusee/tai/records"
)

const TheoryOfStreamingApply = `
Change blocks are applied to an in-memory MemoryStore as they are parsed from
streamed model output, rather than directly to disk. The in-memory semantics —
early error detection, reset on retry, single-batch flush on generation
success, and in-memory content as the base for subsequent same-file edits —
are documented in changes.TheoryOfInMemoryApply.

The streaming-specific mechanism is a BlockHandler callback on ParserState: when a
complete change block is parsed during AppendContent, the handler applies it via
changes.ApplyChangeBlockStore to the MemoryStore. The handler is built by
changes.BuildChangeBlockHandler, sharing the change-application logic with the next
command. If a change block fails to apply, the handler returns a *changes.ApplyError
so the retry loop provides change-block-specific guidance — the retry discards all
change blocks from the failed attempt, so the model must re-emit every intended
change block — and routes the error through OnPhaseError like any other phase error,
which summarizes partial model output before the retry (see
TheoryOfSummaryRetryOnError). The MemoryStore is reset by the OnAttemptStart callback
on each retry, discarding failed changes; when retries are exhausted, generation
stops.

Non-change blocks are collected by the handler into an external slice for
post-phase processing by ProcessComponents. Successfully applied change blocks are
consumed by the handler (not collected), so ProcessComponents finds no change
blocks to re-apply. During Flush the handler is not called for unclosed blocks —
they are incomplete (e.g., truncated output) and applying them would produce
errors. When the apply flag is disabled, no handler is set and all blocks are
collected, preserving the no-apply behavior.
`

const maxRetriesForMissingSummary = 3

const TheoryOfReviewLoop = `
The review loop runs after the main generation loop (or after a goal run
completes) when the -review flag is enabled. It is skipped when the session
produced no applied changes — an empty diff set. Without this, enabling -review on
a session where the model emitted no change blocks (or changes were not applied,
e.g., with -no-apply) would still initiate a wasteful review generation over an
empty diff. The diff set is derived from the in-memory store's session originals,
so it is empty exactly when no change blocks were applied to the working tree (see
changes.TheoryOfInMemoryApply). Session originals are retained across attempt
resets so the diff always reflects the full session delta, not only the last
attempt.

When diffs exist, the review loop opens a fresh dscope scope so the latest
filesystem state is loaded as context — the same reset mechanism the goal runner
uses per loop — and runs one generation session per configured review model,
sequentially. Each review session replaces the original chat input with a review
instruction ("审核并修正这些改动") followed by the unified diff of all changes made
through the MemoryStore during the main generation session. The review model works
from an independent context and corrects potential errors in the changes,
improving accuracy.
`

type Generate func(ctx context.Context, output io.Writer) error

// GenerateWithResultWithStats runs the full codes generation pipeline and
// returns the Result together with the attempt statistics collected during the
// run. The statistics are returned so that callers that run multiple
// independent generation sessions — such as the goal runner — can accumulate
// them and attribute each attempt to its goal loop. See
// TheoryOfAttemptStatistics.
type GenerateWithResultWithStats func(ctx context.Context, output io.Writer) (Result, []AttemptStat, error)

// RunReview provider. It is separate from GenerateWithResultWithStats so
// review is opt-in and does not recursively trigger itself. Each review
// session opens a fresh scope (via dscope.Reset) so the model reads the
// latest filesystem state, and replaces Chats with the review instruction
// plus the session diffs. When ReviewModels is empty, the model from the
// -model flag is reused: the resolved generator's Spec is not reusable
// here because built-in shortcuts (flash, gemini, ...) and the ollama
// shorthand do not set Spec.Name, and their Spec.Model values are not
// resolvable model names. See TheoryOfReviewLoop.
func (Module) RunReview(
	reset dscope.Reset,
	review Review,
	reviewModels ReviewModels,
	modelName flags.ModelName,
) RunReview {
	return func(ctx context.Context, output io.Writer, diffs []changes.FileDiff) error {
		if !bool(review) || len(diffs) == 0 {
			return nil
		}

		models := append([]string{}, reviewModels...)
		if len(models) == 0 {
			models = append(models, string(modelName))
		}

		prompt := buildReviewPrompt(diffs)
		for _, model := range models {
			if model == "" {
				continue
			}
			scope := reset()
			scope = scope.Fork(func() flags.Chats {
				return flags.Chats([]string{prompt})
			})
			scope = scope.Fork(func() flags.ModelName {
				return flags.ModelName(model)
			})
			var reviewErr error
			scope.Call(func(generateWithResultWithStats GenerateWithResultWithStats) {
				_, _, reviewErr = generateWithResultWithStats(ctx, output)
			})
			if reviewErr != nil {
				return fmt.Errorf("review with model %s: %w", model, reviewErr)
			}
		}

		return nil
	}
}

// buildReviewPrompt assembles the review user message: the review
// instruction followed by the session diffs.
func buildReviewPrompt(diffs []changes.FileDiff) string {
	return "审核并修正这些改动\n\n以下是本次改动产生的diff：\n\n" + changes.FormatFileDiffs(diffs)
}

// GenerateWithResult runs the full codes generation pipeline and returns the
// Result, which includes the final state and any remaining (unconsumed)
// blocks. It wraps GenerateWithResultWithStats, discarding the attempt
// statistics. Used by commands that do not need the statistics (go, any).
type GenerateWithResult func(ctx context.Context, output io.Writer) (Result, error)

// RunReview runs one or more review generation sessions after the main
// generation completes. Each review session uses a fresh scope (latest
// filesystem context) and a review model from ReviewModels, in order.
// When ReviewModels is empty, the model from the -model flag is reused.
// The diffs are passed to the model through Chats so they appear in the user
// prompt. See TheoryOfReviewLoop.
type RunReview func(ctx context.Context, output io.Writer, diffs []changes.FileDiff) error

const TheoryOfTokenBudgetStability = `
Accurate token budgeting preserves the prefix cache by ensuring deterministic
file inclusion across requests. Function declarations from all sources — state
layers, code/diff providers, and configuration files — are counted together
and sorted by name before measuring their token cost. The input token budget
is the full context window (or configured max tokens) without reserving space
for max generate tokens, because most tasks complete in a single generation
pass and reserving output space wastes context budget that could carry more
file context.
`

func countFuncsTokens(funcs []generators.FuncDecl, count func(string) (int, error)) (int, error) {
	if len(funcs) == 0 {
		return 0, nil
	}
	data, err := json.Marshal(funcs)
	if err != nil {
		return 0, err
	}
	return count(string(data))
}

// buildUserPromptText concatenates the Text parts of the user prompt parts
// into a single string for token counting. It uses strings.Builder so the
// accumulation is linear in the total context size: repeated += over
// hundreds of file context parts would copy the accumulated string on every
// iteration, which is quadratic for large contexts.
func buildUserPromptText(parts []generators.Part) string {
	var b strings.Builder
	for _, part := range parts {
		if text, ok := part.(generators.Text); ok {
			b.WriteString(string(text))
		}
	}
	return b.String()
}

const TheoryOfAttemptStatistics = `
Attempt statistics are collected per attempt to provide visibility into
token usage and duration. Each attempt produces a single AttemptStat
entry with the 1-based attempt number within its generation; prompt,
completion, thought, and cached token counts from the attempt's final
usage; the duration (from OnAttemptStart to OnAttemptSuccess); and the
summary from the attempt's summary blocks. Attempt management is
decoupled from usage parts: intermediate usage snapshots emitted during
streaming (e.g., Gemini's streaming UsageMetadata) do not create
duplicate entries. Truncated attempts (no summary block) that are
retried are recorded via OnAttemptTruncated with the summary synthesized
by the retry process, so they appear as separate entries; the completing
attempt itself is recorded by OnAttemptSuccess. At run end the collected
statistics are printed to the generation output writer via a deferred
call, so the table is still shown in command-line mode even when the
session ends early due to an error; a live consumer reads the per-attempt
usage from the run's EventUsage events instead (see
TheoryOfUsageLogging). When the goal runner aggregates the statistics
of every loop, it tags each entry with AttemptStat.Loop.
`

// AttemptStat records per-attempt token usage (prompt, completion,
// thoughts, cached), running time, and summary for a single generation
// attempt. The Attempt field is the 1-based attempt number within its
// generation. The Loop field identifies the goal loop that produced the
// attempt when the statistics are aggregated across a goal run's loops;
// it is zero for single-session runs (ai, next, any). See
// TheoryOfAttemptStatistics.
type AttemptStat struct {
	Loop             int
	Attempt          int
	PromptTokens     int
	CompletionTokens int
	ThoughtTokens    int
	CachedTokens     int
	Duration         time.Duration
	Summary          string
}

// PrintAttemptStats writes the attempt statistics table to w. The
// optional title replaces the default "Generation Statistics" header.
// When any stat has a non-zero Loop field (goal run aggregation), a
// Loop column is rendered and summary lines are prefixed with the loop
// number. See TheoryOfAttemptStatistics.
func PrintAttemptStats(w io.Writer, stats []AttemptStat, title ...string) {
	if len(stats) == 0 {
		return
	}
	header := "Generation Statistics"
	if len(title) > 0 && title[0] != "" {
		header = title[0]
	}
	hasLoop := false
	for _, s := range stats {
		if s.Loop != 0 {
			hasLoop = true
			break
		}
	}

	fmt.Fprintf(w, "\n=== %s ===\n", header)
	fmt.Fprintf(w, "Total generations: %d\n\n", len(stats))
	if hasLoop {
		fmt.Fprintf(w, "%-6s %-8s %12s %12s %12s %12s %12s\n", "Loop", "Attempt", "Prompt", "Completion", "Thoughts", "Cached", "Duration")
		fmt.Fprintf(w, "%-6s %-8s %12s %12s %12s %12s %12s\n", "-----", "-------", "------", "----------", "--------", "-------", "--------")
	} else {
		fmt.Fprintf(w, "%-8s %12s %12s %12s %12s %12s\n", "Attempt", "Prompt", "Completion", "Thoughts", "Cached", "Duration")
		fmt.Fprintf(w, "%-8s %12s %12s %12s %12s %12s\n", "-------", "------", "----------", "--------", "-------", "--------")
	}
	var totalPrompt, totalCompletion, totalThoughts, totalCached int
	var totalDuration time.Duration
	for _, s := range stats {
		if hasLoop {
			fmt.Fprintf(w, "%-6d %-8d %12d %12d %12d %12d %12s\n",
				s.Loop, s.Attempt, s.PromptTokens, s.CompletionTokens, s.ThoughtTokens, s.CachedTokens,
				s.Duration.Round(time.Millisecond).String())
		} else {
			fmt.Fprintf(w, "%-8d %12d %12d %12d %12d %12s\n",
				s.Attempt, s.PromptTokens, s.CompletionTokens, s.ThoughtTokens, s.CachedTokens,
				s.Duration.Round(time.Millisecond).String())
		}
		totalPrompt += s.PromptTokens
		totalCompletion += s.CompletionTokens
		totalThoughts += s.ThoughtTokens
		totalCached += s.CachedTokens
		totalDuration += s.Duration
	}
	if hasLoop {
		fmt.Fprintf(w, "%-6s %-8s %12s %12s %12s %12s %12s\n", "-----", "-------", "------", "----------", "--------", "-------", "--------")
		fmt.Fprintf(w, "%-6s %-8s %12d %12d %12d %12d %12s\n", "", "Total", totalPrompt, totalCompletion, totalThoughts, totalCached,
			totalDuration.Round(time.Millisecond).String())
	} else {
		fmt.Fprintf(w, "%-8s %12s %12s %12s %12s %12s\n", "-------", "------", "----------", "--------", "-------", "--------")
		fmt.Fprintf(w, "%-8s %12d %12d %12d %12d %12s\n", "Total", totalPrompt, totalCompletion, totalThoughts, totalCached,
			totalDuration.Round(time.Millisecond).String())
	}
	fmt.Fprintf(w, "==============================\n")

	// Print attempt summaries if any exist. See TheoryOfSummaryBlocks.
	hasSummaries := false
	for _, s := range stats {
		if s.Summary != "" {
			hasSummaries = true
			break
		}
	}
	if hasSummaries {
		fmt.Fprintf(w, "\n=== Attempt Summaries ===\n")
		for _, s := range stats {
			if s.Summary != "" {
				if hasLoop {
					fmt.Fprintf(w, "Loop %d Attempt %d: %s\n", s.Loop, s.Attempt, s.Summary)
				} else {
					fmt.Fprintf(w, "Attempt %d: %s\n", s.Attempt, s.Summary)
				}
			}
		}
		fmt.Fprintf(w, "==============================\n")
	}
}

// collectAttemptStats extracts the last Usage part from the contents
// appended since prevContentCount and appends one AttemptStat entry
// carrying the 1-based attempt number, the usage counts, the elapsed
// duration, and the summary.
func collectAttemptStats(
	attemptStats []AttemptStat,
	state generators.State,
	prevContentCount int,
	elapsed time.Duration,
	summary string,
) ([]AttemptStat, int) {
	var lastUsage generators.Usage
	contentIndex := 0
	for c := range state.Contents() {
		if contentIndex >= prevContentCount {
			for _, part := range c.Parts {
				if usage, ok := part.(generators.Usage); ok {
					lastUsage = usage
				}
			}
		}
		contentIndex++
	}

	attemptStats = append(attemptStats, AttemptStat{
		Attempt:          len(attemptStats) + 1,
		PromptTokens:     lastUsage.Prompt.TokenCount,
		CompletionTokens: lastUsage.Candidates.TokenCount,
		ThoughtTokens:    lastUsage.Thoughts.TokenCount,
		CachedTokens:     lastUsage.Prompt.TokenCountCached,
		Duration:         elapsed,
		Summary:          summary,
	})
	return attemptStats, contentIndex
}

// CreateHandoff summarizes truncated or failed generation output before
// retry, producing a self-contained handoff carried into the next
// generation. The summarize generator, logger, and interaction recorder
// are bound from the dscope scope. See TheoryOfHandoff.
type CreateHandoff func(
	ctx context.Context,
	incompleteText string,
) (*Handoff, error)

func (Module) CreateHandoff(
	logger logs.Logger,
	recorder *records.Recorder,
	getHandoffGenerators GetHandoffGenerators,
	handoffDecorator HandoffStateDecorator,
	handoffObserver HandoffObserver,
) CreateHandoff {
	return func(
		ctx context.Context,
		incompleteText string,
	) (*Handoff, error) {
		generators, err := getHandoffGenerators()
		if err != nil {
			return nil, err
		}
		return createHandoff(ctx, logger, recorder, generators, incompleteText, handoffDecorator, handoffObserver)
	}
}

const TheoryOfDscopeBoundFunctions = `
Functions whose parameters are consistently drawn from the dscope scope —
loggers, recorders, generators — are provided as dscope-bound function
types whose Module methods capture those dependencies. Callers depend on
the type and invoke it with only the runtime values that belong to the
call's semantics, keeping signatures free of cross-cutting infrastructure.
The package-level implementation remains a plain function so tests can
call it directly.
`

func handoffRetryState(
	errState generators.State,
	phaseErr error,
	prevContentCount int,
	createHandoff func(string) (*Handoff, error),
) (newState generators.State, contentCount int, summary string, err error) {
	partialText := ExtractIncompleteOutput(errState, prevContentCount)
	if partialText != "" {
		if handoff, handoffErr := createHandoff(partialText); handoffErr != nil {
			fallbackState, fallbackCount, fallbackSummary := fallbackRetryState(errState, phaseErr)
			return fallbackState, fallbackCount, fallbackSummary, handoffErr
		} else if handoff != nil {
			// Account the handoff request's own token spend: inject the
			// summed usage into the state the caller scans for attempt
			// statistics, before the retry feedback is built on top of
			// it. See TheoryOfHandoffUsageAccounting.
			errState = appendHandoffUsage(errState, prevContentCount, handoff.Usage)
			prefix := fmt.Sprintf(
				"[System note: The previous generation attempt was interrupted by an error after producing partial output: %v. This is a retry. The failed attempt's output was discarded — its structured blocks were NOT applied. If the intended modifications are extensive, partition the work across multiple rounds using continue blocks rather than emitting all changes at once. Re-emit every block you intend to take effect, then correct the issue and continue.]\n\n",
				phaseErr,
			)
			msg := FormatHandoffPrompt(prefix, handoff.Prompt)
			newState, appendErr := errState.AppendContent(&generators.Content{
				Role: generators.RoleUser,
				Parts: []generators.Part{
					generators.Text(msg),
				},
			})
			if appendErr == nil {
				return newState, generators.CountContents(newState), handoff.Summary, nil
			}
		}
	}
	fallbackState, fallbackCount, fallbackSummary := fallbackRetryState(errState, phaseErr)
	return fallbackState, fallbackCount, fallbackSummary, nil
}

// fallbackRetryState appends the phase error as a log content with a summary
// block, returning the resulting state and its content count. It is used when
// no partial output exists or when summarization fails, so the caller always
// has a valid state to return while aborting the run.
func fallbackRetryState(
	errState generators.State,
	phaseErr error,
) (newState generators.State, contentCount int, summary string) {
	newState, err := errState.AppendContent(&generators.Content{
		Role: generators.RoleLog,
		Parts: []generators.Part{
			generators.Error{Error: phaseErr},
			generators.Text(FormatSummaryBlock("[Error: " + phaseErr.Error() + "]")),
		},
	})
	if err != nil {
		return errState, generators.CountContents(errState), "[Error: " + phaseErr.Error() + "]"
	}
	return newState, generators.CountContents(newState), "[Error: " + phaseErr.Error() + "]"
}

const TheoryOfSummaryCompletionRetry = `
The summary block is the mandatory completion signal for each generation
attempt. An attempt is complete only when a summary block is present AND
the finish reason is not abnormal. An attempt missing the summary block
has one of three causes: the generation limit truncated the model
mid-stream before its closing summary block; the model emitted a summary
and continued generating until it was cut off; or the model violated the
every-response rule and simply ended its output without one. All three
are retried from the original pre-generation State. State immutability
(see TheoryOfStateImmutability in generators/state.go) is the foundation
for this retry: the pre-generation State is unaffected by the failed
attempt, so retrying starts from a clean snapshot rather than corrupted
partial state. The retry count is bounded to prevent infinite loops when
a model consistently truncates or omits the summary. Change blocks from
a failed attempt are NOT applied: the retry discards the partial output
entirely and regenerates from the pre-attempt state, avoiding incomplete
or malformed change blocks. This is distinct from the generator-level
retry (see TheoryOfRetry in generators/gemini.go and
TheoryOfGenerateRetry in generators/generate.go), which handles transient
API errors; this retry handles successful-but-incomplete or
non-conforming output.

Completion is detected by checking the externally collected blocks for
summary kind and the finish reason in the state for abnormal termination.
No block kind other than summary completes an attempt: a
component-triggering block (ingest, shell, continue, go-test, go-src)
without a summary block is a rule violation, not a completed attempt, so
such attempts are retried with the missing-summary feedback
(missingSummaryRetryPrefix); an abnormal finish reason instead frames the
retry as truncation (incompleteOutputHandoffPrefix). Because blocks are
collected by the BlockHandler during AppendContent (not stored in
ParserState), the check is a simple scan of the collected slice. The
finish reason is extracted from RoleLog content appended by the
generator. On retry, the collected blocks are reset alongside the
MemoryStore in the OnAttemptStart callback, ensuring both external states
are consistent with the rolled-back State (see TheoryOfParserState in
blocks/parser_state.go); re-emitting the blocks in the retry attempt is
what makes them take effect.

This retry uses handoff (TheoryOfHandoff) to carry forward established
conclusions, attempted changes, and partitioning guidance into the retry
attempt, directing the model to complete an initial subset of changes
first and use continue blocks for remaining work, without retaining
unstructured conversation history.
`

const TheoryOfSummaryRetryOnError = `
Generation errors that occur after the model has already produced partial output
(thoughts or body text) are retried with a handoff summary of that output.
Handoff condenses the partial output into a compact user message that
preserves context while freeing budget, and changes the input so the retry produces
a different response. All generation-phase errors — including missing completion
and change-block apply errors — are routed through the same OnPhaseError retry path
with handoff, ensuring consistent retry behavior regardless of the error type.

The handoff extracts the valuable content of the partial output — the
discoveries, decisions, facts, and attempted changes the model had already
established — and presents them to the retry attempt with guidance on task
partitioning. The retry therefore continues from the model's conclusions and
completes a manageable subset of changes first, using continue blocks for
remaining work to prevent exceeding output limits again. See TheoryOfHandoff.

This handoff is transient error recovery. The condensed content is injected
into one retry request and does not persist as compressed history. The system does
not compress conversation. See TheoryOfContextPhilosophy.
`

// Generate wraps GenerateWithResult, discarding the Result so existing
// callers (go, any) see the same func(ctx, output) error signature.
func (Module) Generate(
	generateWithResult GenerateWithResult,
) Generate {
	return func(ctx context.Context, output io.Writer) error {
		_, err := generateWithResult(ctx, output)
		return err
	}
}

// GenerateWithResult wraps GenerateWithResultWithStats, discarding the
// attempt statistics so existing callers see the same (Result, error)
// signature. Callers that need the statistics (e.g., the goal runner)
// should use GenerateWithResultWithStats directly. See TheoryOfAttemptStatistics.
func (Module) GenerateWithResult(
	generateWithResultWithStats GenerateWithResultWithStats,
) GenerateWithResult {
	return func(ctx context.Context, output io.Writer) (Result, error) {
		result, _, err := generateWithResultWithStats(ctx, output)
		return result, err
	}
}

const TheoryOfChatBracketing = `
Chat bracketing: at user prompt assembly points backed by a parts
provider, the chat input is placed before the parts provider content as
well as after it. The pipeline prepends a copy of the joined -chat
arguments before the provider parts and keeps appending the chat input
itself after the system prompt restate, so the long file context is
bracketed by the user request on both sides: the model reads the task
before the context — knowing what to look for while reading — and reads
it again as the freshest input before generating. The prepended copy
ends with a blank line so the context starts a fresh paragraph
(generators.TheoryOfContentUnitSeparation). The copy is dynamic content
at the head of the user content, so different chat inputs shift the file
context in the request and forfeit user-content prefix reuse across
tasks; comprehension is deliberately traded for cache. The next
command's UserPrompt prepends the chat input the same way when given;
its single-shot design has no trailing chat content, so the restate
remains the last part.
`

// GenerateWithResultWithStats runs the full codes generation pipeline
// and returns the Result together with the attempt statistics collected
// during the run. The statistics are returned so that callers that run
// multiple independent generation sessions — such as the goal runner —
// can accumulate them and attribute each attempt to its goal loop. See
// TheoryOfAttemptStatistics.
func (Module) GenerateWithResultWithStats(
	partsProvider codetypes.PartsProvider,
	comps CodesComponents,
	systemPrompt SystemPrompt,
	logger logs.Logger,
	getDefaultGenerator generators.GetDefaultGenerator,
	getDefaultSummarizer GetDefaultSummarizer,
	getHandoffGenerator GetHandoffGenerator,
	buildGenerate generators.BuildGenerate,
	maxTokens flags.MaxTokens,
	buildChangeBlockHandler changes.BuildChangeBlockHandler,
	patterns Patterns,
	flagThoughts flags.Thoughts,
	summarizeThoughts flags.SummarizeThoughts,
	httpClient nets.HTTPClient,
	flagChats flags.Chats,
	debug Debug,
	funcDecls generators.FuncDecls,
	apply flags.Apply,
	loopRun Run,
	recorder *records.Recorder,
	writeTimes *changes.FileWriteTimes,
	hashes *changes.FileHashes,
	createHandoff CreateHandoff,
	goalLoop GoalLoop,
) GenerateWithResultWithStats {
	return func(ctx context.Context, output io.Writer) (Result, []AttemptStat, error) {

		// Open a root on the current directory to restrict all file I/O
		// to the project tree. See blocks.TheoryOfIngestBlocks.
		root, err := os.OpenRoot(".")
		if err != nil {
			return Result{}, nil, err
		}
		defer root.Close()

		// MemoryStore buffers change block modifications in memory during
		// streaming, deferring disk writes until the generation succeeds.
		// The underlying root store enables write conflict detection and
		// disk-change detection: a file modified externally since the
		// context snapshot was assembled is rejected at read, write, and
		// flush time. See TheoryOfStreamingApply,
		// changes.TheoryOfInMemoryApply,
		// changes.TheoryOfWriteConflictDetection and
		// changes.TheoryOfDiskChangeDetection.
		memStore := changes.NewMemoryStore(changes.NewRootStoreWithSnapshot(root, writeTimes, hashes))

		// generator
		generator, err := getDefaultGenerator()
		if err != nil {
			return Result{}, nil, err
		}
		spec := generator.Spec()
		logger.Info("initial generator",
			"name", spec.Name,
			"family", spec.Family,
			"model", spec.Model,
			"effort", spec.ReasoningEffort,
		)
		if recorder != nil && recorder.Enabled() {
			recorder.Event("decision", fmt.Sprintf("generator selected: name=%q family=%q model=%q effort=%q", spec.Name, spec.Family, spec.Model, spec.ReasoningEffort))
		}

		// handoff generator
		handoffGenerator, err := getHandoffGenerator()
		if err != nil {
			return Result{}, nil, err
		}
		if recorder != nil && recorder.Enabled() {
			recorder.Event("decision", fmt.Sprintf("handoff generator selected: model=%s", handoffGenerator.Spec().Model))
		}

		// Calculate basic limits. The full context window is available for
		// input without reserving max generate tokens: most tasks complete
		// in a single generation pass, so reserving output space wastes
		// context budget that could carry more file context.
		// See TheoryOfTokenBudgetStability.
		maxInputTokens := min(
			spec.ContextTokens,
			int(maxTokens),
		)

		// Count tokens for fixed parts
		systemPromptTokens, err := generator.CountTokens(string(systemPrompt))
		if err != nil {
			return Result{}, nil, err
		}

		// Collect function declarations from all sources for accurate token
		// counting. See TheoryOfTokenBudgetStability.
		var allFuncDecls []generators.FuncDecl
		if spec.DisableTools != nil && !*spec.DisableTools {
			allFuncDecls = append(allFuncDecls, funcDecls...)
			sort.SliceStable(allFuncDecls, func(i, j int) bool {
				return allFuncDecls[i].Name < allFuncDecls[j].Name
			})
		}
		funcTokens, err := countFuncsTokens(allFuncDecls, generator.CountTokens)
		if err != nil {
			return Result{}, nil, err
		}

		// Calculate remaining budget for user content. The system prompt is
		// charged twice: once as the actual system prompt, and once for
		// the verbatim restate appended at the end of the user prompt
		// (components.SystemPromptRestate), which re-sends the full
		// system prompt inside the user content. See
		// components.TheoryOfComponents.
		maxUserPromptTokens := maxInputTokens - systemPromptTokens*2 - funcTokens - 1000
		if maxUserPromptTokens <= 0 {
			return Result{}, nil, fmt.Errorf("token limit too low, need at least %d more", -maxUserPromptTokens)
		}
		logger.Info("token limits",
			"system", systemPromptTokens,
			"functions", funcTokens,
			"max user content", maxUserPromptTokens,
		)
		if recorder != nil && recorder.Enabled() {
			recorder.Event("decision", fmt.Sprintf("token limits computed: max_input=%d system=%d functions=%d user_capacity=%d", maxInputTokens, systemPromptTokens, funcTokens, maxUserPromptTokens))
		}

		// The chat input brackets the parts provider content: a copy of
		// the joined -chat arguments is prepended before the context so
		// the model knows the task while reading it, and the chat input
		// itself still follows the context after the restate. The
		// prepended copy ends with a blank line so the context starts a
		// fresh paragraph. See TheoryOfChatBracketing and
		// generators.TheoryOfContentUnitSeparation.
		var userPromptParts []generators.Part
		if chats := strings.Join(flagChats, "\n"); chats != "" {
			userPromptParts = append(userPromptParts, generators.Text(chats+"\n\n"))
		}
		providerParts, err := partsProvider.Parts(maxUserPromptTokens, generator.CountTokens, patterns)
		if err != nil {
			return Result{}, nil, err
		}
		userPromptParts = append(userPromptParts, providerParts...)

		// Component user prompt parts are appended after parts provider parts.
		userPromptParts = append(userPromptParts, comps.UserPromptParts()...)

		// The system prompt restate is the last user prompt part before
		// the dynamic chat input: the model re-reads the complete
		// instructions verbatim immediately before generating, and the
		// restate is built from the same text as the system prompt so the
		// two can never diverge. See components.TheoryOfComponents.
		userPromptParts = append(userPromptParts, components.SystemPromptRestate(string(systemPrompt)))

		// Concatenate the text parts with strings.Builder for token counting.
		userPromptText := buildUserPromptText(userPromptParts)
		userPromptTokens, err := generator.CountTokens(userPromptText)
		if err != nil {
			return Result{}, nil, err
		}
		logger.Info("user prompt ready",
			"tokens", userPromptTokens,
			"parts", len(userPromptParts),
		)
		if recorder != nil && recorder.Enabled() {
			recorder.Event("decision", fmt.Sprintf("user prompt assembled: parts=%d tokens=%d", len(userPromptParts), userPromptTokens))
		}

		if debug {
			fmt.Fprintf(output, "system prompt: %s\n", systemPrompt)
			fmt.Fprintf(output, "user prompt: %s\n", userPromptParts)
		}

		// initial state
		var initialContents []*generators.Content
		if len(userPromptParts) > 0 {
			initialContents = []*generators.Content{
				{
					Role:  "user",
					Parts: userPromptParts,
				},
			}
		}
		var state generators.State
		state = generators.NewPrompts(
			string(systemPrompt),
			initialContents,
		)
		showThoughts := true
		if flagThoughts.Value != nil {
			showThoughts = *flagThoughts.Value
		}

		if showThoughts && bool(summarizeThoughts) {
			summarizer, err := getDefaultSummarizer()
			if err != nil {
				return Result{}, nil, err
			}
			state = generators.NewOutput(state, output, false)
			state = NewThoughtsSummarize(ctx, state, summarizer, output)
		} else {
			state = generators.NewOutput(state, output, showThoughts)
		}

		var attemptStats []AttemptStat
		defer func() {
			// The table goes to the generation output writer, so the
			// statistics stay visible in command-line mode even when the
			// session ends early. In TUI mode that writer is the
			// redirected null device and the TUI reads the usage from
			// the run's per-attempt EventUsage events instead. See
			// TheoryOfAttemptStatistics and TheoryOfUsageLogging.
			PrintAttemptStats(output, attemptStats)
		}()

		var attemptStartTime time.Time

		var hasChats bool
		if chats := strings.Join(flagChats, "\n"); chats != "" {
			state, err = state.AppendContent(&generators.Content{
				Role: "user",
				Parts: []generators.Part{
					generators.Text(chats),
				},
			})
			if err != nil {
				return Result{}, nil, err
			}
			hasChats = true
		}

		if !hasChats {
			return Result{}, nil, nil
		}

		prevContentCount := generators.CountContents(state)

		var blockHandler BlockHandler
		if bool(apply) {
			handler := buildChangeBlockHandler(memStore)
			blockHandler = BlockHandler(handler)
		}

		runCtx, cancel := context.WithCancel(ctx)
		defer cancel()
		var fatalErr error

		var result Result
		// The loop yields one Event per notable occurrence; the terminal
		// error, if any, arrives with the final yield's error component.
		// Events with a nil error are informational and do not set err.
		// See TheoryOfLoopEvents.
		for _, e := range loopRun(runCtx, RunOptions{
			Generator:    generator,
			InitialState: state,
			Components:   comps.ComponentSet,
			BlockHandler: blockHandler,
			PhaseBuilder: func(g generators.Generator) generators.Phase {
				return buildGenerate(g, nil)(nil)
			},
			Root:                root,
			HTTPClient:          httpClient,
			Command:             "codes",
			InteractionRecorder: recorder,
			Loop:                int(goalLoop),

			OnAttemptStart: func() {
				memStore.Reset()
				attemptStartTime = time.Now()
			},

			OnAttemptSuccess: func(attemptState generators.State, summaries []string) error {
				if err := memStore.Flush(); err != nil {
					return err
				}
				if recorder != nil && recorder.Enabled() {
					recorder.Event("decision", "attempt succeeded: in-memory changes flushed to disk")
				}

				elapsed := time.Since(attemptStartTime)

				// The loop guarantees a summary for every attempt: the
				// model's summary blocks, or the synthesized summary
				// appended when the retry budget is exhausted (see
				// TheoryOfLoops). No handoff fallback runs here: a
				// handoff is an extra model request for error
				// recovery, never a substitute for the model's own
				// summary block.
				summaryText := strings.Join(summaries, "\n")
				attemptStats, prevContentCount = collectAttemptStats(
					attemptStats, attemptState, prevContentCount, elapsed, summaryText,
				)

				attemptStartTime = time.Now()
				return nil
			},

			OnAttemptTruncated: func(truncatedState generators.State, retryBaseState generators.State, summary string) error {
				elapsed := time.Since(attemptStartTime)
				attemptStats, _ = collectAttemptStats(
					attemptStats, truncatedState, prevContentCount, elapsed, summary,
				)
				prevContentCount = generators.CountContents(retryBaseState)
				return nil
			},

			OnPhaseError: func(errState generators.State, phaseErr error) generators.State {
				if fatalErr != nil {
					return errState
				}

				elapsed := time.Since(attemptStartTime)
				newState, newContentCount, summary, handoffErr := handoffRetryState(
					errState,
					phaseErr,
					prevContentCount,
					func(text string) (*Handoff, error) {
						return createHandoff(runCtx, text)
					},
				)
				// Scan the returned state rather than the error state:
				// the success path injects the handoff request's own
				// usage into newState (see
				// TheoryOfHandoffUsageAccounting), and the fallback path
				// differs only by usage-free appended content, so
				// scanning either yields the same last usage there.
				attemptStats, _ = collectAttemptStats(attemptStats, newState, prevContentCount, elapsed, summary)
				prevContentCount = newContentCount

				if handoffErr != nil {
					fatalErr = handoffErr
					cancel()
				}

				return newState
			},

			RetryOnMissingCompletion: true,
			RetryOnError:             true,
			MaxRetries:               maxRetriesForMissingSummary,
			Handoff: func(incompleteText string) (*Handoff, error) {
				if fatalErr != nil {
					return nil, fatalErr
				}
				handoff, err := createHandoff(runCtx, incompleteText)
				if err != nil {
					fatalErr = err
					cancel()
				}
				return handoff, err
			},
		}, &result) {
			if e != nil {
				err = e
			}
		}

		if fatalErr != nil {
			err = fatalErr
		}

		if err == nil && len(result.ParseErrors) > 0 {
			logger.WarnContext(ctx, "uncorrected malformed blocks",
				"count", len(result.ParseErrors),
			)
		}

		if err != nil {
			memStore.Reset()
		}
		result.Diffs = memStore.Diffs()

		return result, attemptStats, err
	}
}
