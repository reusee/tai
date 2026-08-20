package codes

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
	"github.com/reusee/tai/codes/codetypes"
	"github.com/reusee/tai/flags"
	"github.com/reusee/tai/generators"
	"github.com/reusee/tai/logs"
	"github.com/reusee/tai/loops"
	"github.com/reusee/tai/nets"
	"github.com/reusee/tai/phases"
	"github.com/reusee/tai/records"
	"github.com/reusee/tai/states"
)

const TheoryOfStreamingApply = `
Change blocks are applied to an in-memory MemoryStore as they are parsed from
streamed model output, rather than directly to disk. The in-memory semantics —
early error detection, reset on retry, single-batch flush on round success, and
in-memory content as the base for subsequent same-file edits — are documented in
changes.TheoryOfInMemoryApply.

The streaming-specific mechanism is a BlockHandler callback on ParserState: when a
complete change block is parsed during AppendContent, the handler applies it via
changes.ApplyChangeBlockStore to the MemoryStore. The handler is built by
changes.BuildChangeBlockHandler, sharing the change-application logic with the next
command. If a change block fails to apply, the handler returns a *changes.ApplyError
so the retry loop provides change-block-specific guidance — the retry discards all
change blocks from the failed attempt, so the model must re-emit every intended
change block — and routes the error through OnPhaseError like any other phase error,
which summarizes partial model output before the retry (see
TheoryOfSummaryRetryOnError). The MemoryStore is reset by the OnRoundStart callback
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

const maxRequestContextRounds = 5

const maxGoTestRounds = 10

const maxRetriesForMissingSummary = 3

const TheoryOfReviewLoop = `
The review loop runs after the main generation loop (or after the goal command
completes) when the -review flag is enabled. It is skipped when the session
produced no applied changes — an empty diff set. Without this, enabling -review on
a session where the model emitted no change blocks (or changes were not applied,
e.g., with -no-apply) would still initiate a wasteful review generation over an
empty diff. The diff set is derived from the in-memory store's session originals,
so it is empty exactly when no change blocks were applied to the working tree (see
changes.TheoryOfInMemoryApply). Session originals are retained across round resets
so the diff always reflects the full session delta, not only the last round.

When diffs exist, the review loop opens a fresh dscope scope so the latest
filesystem state is loaded as context — the same reset mechanism the goal command
uses per loop — and runs one generation session per configured review model,
sequentially. Each review session replaces the original chat input with a review
instruction ("审核并修正这些改动") followed by the unified diff of all changes made
through the MemoryStore during the main generation session. The review model works
from an independent context and corrects potential errors in the changes,
improving accuracy.
`

type Generate func(ctx context.Context, output io.Writer) error

// GenerateWithResultWithStats runs the full codes generation pipeline and
// returns the loops.Result together with the round statistics collected
// during the run. The statistics are returned (not only printed) so that
// callers that run multiple independent generation sessions — such as the
// goal command — can accumulate them and re-print the entire process
// aggregated after all sessions complete. See TheoryOfRoundStatistics.
type GenerateWithResultWithStats func(ctx context.Context, output io.Writer) (loops.Result, []RoundStat, error)

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
// loops.Result, which includes the final state and any remaining (unconsumed)
// blocks. It wraps GenerateWithResultWithStats, discarding the round
// statistics. Used by commands that do not need the statistics (go, any).
type GenerateWithResult func(ctx context.Context, output io.Writer) (loops.Result, error)

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

const TheoryOfRoundStatistics = `
Round statistics are collected per round to provide visibility into token usage
and duration. Each round produces a single RoundStat entry with the 1-based round
number; prompt, completion, thought, and cached token counts from the round's
final usage; the duration (from OnRoundStart to OnRoundSuccess); and the summary
from the round's summary blocks. Round management is decoupled from usage parts:
intermediate usage snapshots emitted during streaming (e.g., Gemini's streaming
UsageMetadata) do not create duplicate round entries. Truncated rounds (no
summary block) that are retried are recorded via OnRoundTruncated with the
summary synthesized by the retry process, so they appear as separate loops in the
statistics; the retry round itself is recorded by OnRoundSuccess when it
completes successfully. The statistics are printed at the end of the session
via a deferred call, so they are shown even when the session ends early due to
an error.
`

// RoundStat records per-round token usage (prompt, completion, thoughts,
// cached), running time, and summary for a single generation round. The
// Loop field identifies the goal loop that produced the round when the
// statistics are aggregated across multiple goal loops (goal command);
// it is zero for single-session runs (go, any). See
// TheoryOfRoundStatistics.
type RoundStat struct {
	Loop             int
	Round            int
	PromptTokens     int
	CompletionTokens int
	ThoughtTokens    int
	CachedTokens     int
	Duration         time.Duration
	Summary          string
}

// PrintRoundStats writes the round statistics table to w. The optional
// title replaces the default "Generation Statistics" header. When any
// stat has a non-zero Loop field (goal command aggregation), a Loop
// column is rendered and summary lines are prefixed with the loop
// number. See TheoryOfRoundStatistics.
func PrintRoundStats(w io.Writer, stats []RoundStat, title ...string) {
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
	fmt.Fprintf(w, "Total rounds: %d\n\n", len(stats))
	if hasLoop {
		fmt.Fprintf(w, "%-6s %-6s %12s %12s %12s %12s %12s\n", "Loop", "Round", "Prompt", "Completion", "Thoughts", "Cached", "Duration")
		fmt.Fprintf(w, "%-6s %-6s %12s %12s %12s %12s %12s\n", "-----", "-----", "------", "----------", "--------", "-------", "--------")
	} else {
		fmt.Fprintf(w, "%-6s %12s %12s %12s %12s %12s\n", "Round", "Prompt", "Completion", "Thoughts", "Cached", "Duration")
		fmt.Fprintf(w, "%-6s %12s %12s %12s %12s %12s\n", "-----", "------", "----------", "--------", "-------", "--------")
	}
	var totalPrompt, totalCompletion, totalThoughts, totalCached int
	var totalDuration time.Duration
	for _, s := range stats {
		if hasLoop {
			fmt.Fprintf(w, "%-6d %-6d %12d %12d %12d %12d %12s\n",
				s.Loop, s.Round, s.PromptTokens, s.CompletionTokens, s.ThoughtTokens, s.CachedTokens,
				s.Duration.Round(time.Millisecond).String())
		} else {
			fmt.Fprintf(w, "%-6d %12d %12d %12d %12d %12s\n",
				s.Round, s.PromptTokens, s.CompletionTokens, s.ThoughtTokens, s.CachedTokens,
				s.Duration.Round(time.Millisecond).String())
		}
		totalPrompt += s.PromptTokens
		totalCompletion += s.CompletionTokens
		totalThoughts += s.ThoughtTokens
		totalCached += s.CachedTokens
		totalDuration += s.Duration
	}
	if hasLoop {
		fmt.Fprintf(w, "%-6s %-6s %12s %12s %12s %12s %12s\n", "-----", "-----", "------", "----------", "--------", "-------", "--------")
		fmt.Fprintf(w, "%-6s %-6s %12d %12d %12d %12d %12s\n", "", "Total", totalPrompt, totalCompletion, totalThoughts, totalCached,
			totalDuration.Round(time.Millisecond).String())
	} else {
		fmt.Fprintf(w, "%-6s %12s %12s %12s %12s %12s\n", "-----", "------", "----------", "--------", "-------", "--------")
		fmt.Fprintf(w, "%-6s %12d %12d %12d %12d %12s\n", "Total", totalPrompt, totalCompletion, totalThoughts, totalCached,
			totalDuration.Round(time.Millisecond).String())
	}
	fmt.Fprintf(w, "==============================\n")

	// Print round summaries if any exist. See TheoryOfSummaryBlocks.
	hasSummaries := false
	for _, s := range stats {
		if s.Summary != "" {
			hasSummaries = true
			break
		}
	}
	if hasSummaries {
		fmt.Fprintf(w, "\n=== Round Summaries ===\n")
		for _, s := range stats {
			if s.Summary != "" {
				if hasLoop {
					fmt.Fprintf(w, "Loop %d Round %d: %s\n", s.Loop, s.Round, s.Summary)
				} else {
					fmt.Fprintf(w, "Round %d: %s\n", s.Round, s.Summary)
				}
			}
		}
		fmt.Fprintf(w, "==============================\n")
	}
}

func collectRoundStats(
	roundStats []RoundStat,
	state generators.State,
	prevContentCount int,
	elapsed time.Duration,
	summary string,
) ([]RoundStat, int) {
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

	roundStats = append(roundStats, RoundStat{
		Round:            len(roundStats) + 1,
		PromptTokens:     lastUsage.Prompt.TokenCount,
		CompletionTokens: lastUsage.Candidates.TokenCount,
		ThoughtTokens:    lastUsage.Thoughts.TokenCount,
		CachedTokens:     lastUsage.Prompt.TokenCountCached,
		Duration:         elapsed,
		Summary:          summary,
	})
	return roundStats, contentIndex
}

// CreateHandoff summarizes truncated or failed generation output before
// retry, producing a self-contained handoff carried into the next round.
// The summarize generator, logger, and interaction recorder are bound from
// the dscope scope. See states.TheoryOfHandoff.
type CreateHandoff func(
	ctx context.Context,
	incompleteText string,
) (*states.Handoff, error)

func (Module) CreateHandoff(
	logger logs.Logger,
	recorder *records.Recorder,
	getHandoffGenerators states.GetHandoffGenerators,
	handoffWriter states.HandoffWriter,
	handoffObserver states.HandoffObserver,
) CreateHandoff {
	return func(
		ctx context.Context,
		incompleteText string,
	) (*states.Handoff, error) {
		generators, err := getHandoffGenerators()
		if err != nil {
			return nil, err
		}
		return states.CreateHandoff(ctx, logger, recorder, generators, incompleteText, handoffWriter, handoffObserver)
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
	createHandoff func(string) (*states.Handoff, error),
) (newState generators.State, contentCount int, summary string, err error) {
	partialText := loops.ExtractIncompleteOutput(errState, prevContentCount)
	if partialText != "" {
		if handoff, handoffErr := createHandoff(partialText); handoffErr != nil {
			fallbackState, fallbackCount, fallbackSummary := fallbackRetryState(errState, phaseErr)
			return fallbackState, fallbackCount, fallbackSummary, handoffErr
		} else if handoff != nil {
			prefix := fmt.Sprintf(
				"[System note: The previous generation attempt was interrupted by an error after producing partial output: %v. This is a retry. The failed attempt's output was discarded — its structured blocks were NOT applied. If the intended modifications are extensive, partition the work across multiple rounds using continue blocks rather than emitting all changes at once. Re-emit every block you intend to take effect, then correct the issue and continue.]\n\n",
				phaseErr,
			)
			msg := states.FormatHandoffPrompt(prefix, handoff.Prompt)
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
			generators.Text(states.FormatSummaryBlock("[Error: " + phaseErr.Error() + "]")),
		},
	})
	if err != nil {
		return errState, generators.CountContents(errState), "[Error: " + phaseErr.Error() + "]"
	}
	return newState, generators.CountContents(newState), "[Error: " + phaseErr.Error() + "]"
}

const TheoryOfSummaryCompletionRetry = `
The summary block serves as the completion signal for each generation round. When
a round ends without a summary block, or when the finish reason indicates abnormal
termination (e.g., "length" from max-token truncation), the model's output was
likely truncated mid-stream — the generation limit was reached before the model
could emit its closing summary block, or the model emitted a summary but continued
generating and was cut off. Truncation often occurs because the model attempted
too many changes in a single response. In both cases, the round is retried from the
original pre-generation State. State immutability (see TheoryOfStateImmutability in
generators/state.go) is the foundation for this retry: the pre-generation State is
unaffected by the failed attempt, so retrying starts from a clean snapshot rather
than corrupted partial state. The retry count is bounded to prevent infinite loops
when a model consistently truncates. Change blocks from a truncated attempt are
NOT applied: the retry discards the partial output entirely and regenerates from
the pre-round state, avoiding incomplete or malformed change blocks. This is
distinct from the generator-level retry (see TheoryOfRetry in generators/gemini.go
and TheoryOfGenerateRetry in phases/generate.go), which handles transient API
errors; this retry handles successful-but-incomplete output.

Completion is detected by checking the externally collected blocks for summary
kind and the finish reason in the state for abnormal termination. A round is
considered complete only when a summary block is present AND the finish reason is
not abnormal. Because blocks are collected by the BlockHandler during AppendContent
(not stored in ParserState), the check is a simple scan of the collected slice. The
finish reason is extracted from RoleLog content appended by the generator. On
retry, the collected blocks are reset alongside the MemoryStore in the onPhaseStart
callback, ensuring both external states are consistent with the rolled-back State
(see TheoryOfParserState in blocks/parser_state.go).

This retry uses handoff (TheoryOfHandoff in states/summarize_incomplete.go) to
carry forward established conclusions, attempted changes, and partitioning guidance
into the next round, directing the model to complete an initial subset of changes
first and use continue blocks for remaining work, without retaining unstructured
conversation history.
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
established — and presents them to the retry round with guidance on task
partitioning. The retry therefore continues from the model's conclusions and
completes a manageable subset of changes first, using continue blocks for
remaining work to prevent exceeding output limits again. See states.TheoryOfHandoff.

This handoff is transient error recovery. The condensed content is injected
into one retry request and does not persist as compressed history. The system does
not compress conversation. See TheoryOfContextPhilosophy in loops/run.go.
`

// Generate wraps GenerateWithResult, discarding the loops.Result so existing
// callers (go, any) see the same func(ctx, output) error signature.
func (Module) Generate(
	generateWithResult GenerateWithResult,
) Generate {
	return func(ctx context.Context, output io.Writer) error {
		_, err := generateWithResult(ctx, output)
		return err
	}
}

// GenerateWithResult wraps GenerateWithResultWithStats, discarding the round
// statistics so existing callers see the same (loops.Result, error)
// signature. Callers that need the statistics (e.g., goal command) should
// use GenerateWithResultWithStats directly. See TheoryOfRoundStatistics.
func (Module) GenerateWithResult(
	generateWithResultWithStats GenerateWithResultWithStats,
) GenerateWithResult {
	return func(ctx context.Context, output io.Writer) (loops.Result, error) {
		result, _, err := generateWithResultWithStats(ctx, output)
		return result, err
	}
}

func (Module) GenerateWithResultWithStats(
	codeProvider codetypes.CodeProvider,
	comps CodesComponents,
	systemPrompt SystemPrompt,
	logger logs.Logger,
	getDefaultGenerator generators.GetDefaultGenerator,
	getDefaultSummarizer states.GetDefaultSummarizer,
	getHandoffGenerator states.GetHandoffGenerator,
	buildGenerate phases.BuildGenerate,
	maxTokens flags.MaxTokens,
	buildChangeBlockHandler changes.BuildChangeBlockHandler,
	patterns Patterns,
	flagThoughts flags.Thoughts,
	summarizeThoughts states.SummarizeThoughts,
	httpClient nets.HTTPClient,
	flagChats flags.Chats,
	debug Debug,
	funcDecls generators.FuncDecls,
	apply flags.Apply,
	loopRun loops.Run,
	recorder *records.Recorder,
	writeTimes *changes.FileWriteTimes,
	thoughtSummaryWriter states.ThoughtSummaryWriter,
	createHandoff CreateHandoff,
) GenerateWithResultWithStats {
	return func(ctx context.Context, output io.Writer) (loops.Result, []RoundStat, error) {

		// Open a root on the current directory to restrict all file I/O
		// to the project tree. See TheoryOfRequestContext.
		root, err := os.OpenRoot(".")
		if err != nil {
			return loops.Result{}, nil, err
		}
		defer root.Close()

		// MemoryStore buffers change block modifications in memory during
		// streaming, deferring disk writes until the round succeeds.
		// The underlying root store enables write conflict detection: a
		// file modified externally since the last write is rejected at
		// flush time. See TheoryOfStreamingApply,
		// changes.TheoryOfInMemoryApply and
		// changes.TheoryOfWriteConflictDetection.
		memStore := changes.NewMemoryStore(changes.NewRootStoreWithWriteTimes(root, writeTimes))

		// generator
		generator, err := getDefaultGenerator()
		if err != nil {
			return loops.Result{}, nil, err
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
			return loops.Result{}, nil, err
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
			return loops.Result{}, nil, err
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
			return loops.Result{}, nil, err
		}

		// Calculate remaining budget for user content
		maxUserPromptTokens := maxInputTokens - systemPromptTokens - funcTokens - 1000
		if maxUserPromptTokens <= 0 {
			return loops.Result{}, nil, fmt.Errorf("token limit too low, need at least %d more", -maxUserPromptTokens)
		}
		logger.Info("token limits",
			"system", systemPromptTokens,
			"functions", funcTokens,
			"max user content", maxUserPromptTokens,
		)
		if recorder != nil && recorder.Enabled() {
			recorder.Event("decision", fmt.Sprintf("token limits computed: max_input=%d system=%d functions=%d user_capacity=%d", maxInputTokens, systemPromptTokens, funcTokens, maxUserPromptTokens))
		}

		// user prompt
		userPromptParts, err := codeProvider.Parts(maxUserPromptTokens, generator.CountTokens, patterns)
		if err != nil {
			return loops.Result{}, nil, err
		}

		// Component user prompt parts are appended after code provider parts.
		userPromptParts = append(userPromptParts, comps.UserPromptParts()...)

		// Concatenate the text parts with strings.Builder for token counting.
		userPromptText := buildUserPromptText(userPromptParts)
		userPromptTokens, err := generator.CountTokens(userPromptText)
		if err != nil {
			return loops.Result{}, nil, err
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
				return loops.Result{}, nil, err
			}
			state = generators.NewOutput(state, output, false)
			summaryWriter := output
			if thoughtSummaryWriter != nil {
				summaryWriter = thoughtSummaryWriter
			}
			state = states.NewThoughtsSummarize(ctx, state, summarizer, summaryWriter)
		} else {
			state = generators.NewOutput(state, output, showThoughts)
		}

		var roundStats []RoundStat
		defer func() {
			PrintRoundStats(output, roundStats)
		}()

		var roundStartTime time.Time

		var hasChats bool
		if chats := strings.Join(flagChats, "\n"); chats != "" {
			state, err = state.AppendContent(&generators.Content{
				Role: "user",
				Parts: []generators.Part{
					generators.Text(chats),
				},
			})
			if err != nil {
				return loops.Result{}, nil, err
			}
			hasChats = true
		}

		if !hasChats {
			return loops.Result{}, nil, nil
		}

		prevContentCount := generators.CountContents(state)

		var blockHandler loops.BlockHandler
		if bool(apply) {
			handler := buildChangeBlockHandler(memStore)
			blockHandler = loops.BlockHandler(handler)
		}

		runCtx, cancel := context.WithCancel(ctx)
		defer cancel()
		var fatalErr error

		var result loops.Result
		for e := range loopRun(runCtx, loops.RunOptions{
			Generator:    generator,
			InitialState: state,
			Components:   comps.ComponentSet,
			BlockHandler: blockHandler,
			PhaseBuilder: func(g generators.Generator) phases.Phase {
				return buildGenerate(g, nil)(nil)
			},
			Root:                root,
			HTTPClient:          httpClient,
			Command:             "codes",
			InteractionRecorder: recorder,

			OnRoundStart: func() {
				memStore.Reset()
				roundStartTime = time.Now()
			},

			OnRoundSuccess: func(roundState generators.State, summaries []string) error {
				if err := memStore.Flush(); err != nil {
					return err
				}
				if recorder != nil && recorder.Enabled() {
					recorder.Event("decision", "round succeeded: in-memory changes flushed to disk")
				}

				elapsed := time.Since(roundStartTime)

				summaryText := ""
				var handoffErr error
				if len(summaries) > 0 {
					summaryText = strings.Join(summaries, "\n")
				} else {
					if incompleteText := loops.ExtractIncompleteOutput(roundState, prevContentCount); incompleteText != "" {
						var handoff *states.Handoff
						handoff, handoffErr = createHandoff(runCtx, incompleteText)
						if handoffErr == nil && handoff != nil {
							summaryText = handoff.Summary
						}
					}
				}
				roundStats, prevContentCount = collectRoundStats(
					roundStats, roundState, prevContentCount, elapsed, summaryText,
				)

				if handoffErr != nil {
					fatalErr = handoffErr
					cancel()
					return nil
				}

				roundStartTime = time.Now()
				return nil
			},

			OnRoundTruncated: func(truncatedState generators.State, retryBaseState generators.State, summary string) error {
				elapsed := time.Since(roundStartTime)
				roundStats, _ = collectRoundStats(
					roundStats, truncatedState, prevContentCount, elapsed, summary,
				)
				prevContentCount = generators.CountContents(retryBaseState)
				return nil
			},

			OnPhaseError: func(errState generators.State, phaseErr error) generators.State {
				if fatalErr != nil {
					return errState
				}

				elapsed := time.Since(roundStartTime)
				newState, newContentCount, summary, handoffErr := handoffRetryState(
					errState,
					phaseErr,
					prevContentCount,
					func(text string) (*states.Handoff, error) {
						return createHandoff(runCtx, text)
					},
				)
				roundStats, _ = collectRoundStats(roundStats, errState, prevContentCount, elapsed, summary)
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
			Handoff: func(incompleteText string) (*states.Handoff, error) {
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
			err = e
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

		return result, roundStats, err
	}
}
