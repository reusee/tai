package codes

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/reusee/dscope"
	"github.com/reusee/tai/blocks"
	"github.com/reusee/tai/changes"
	"github.com/reusee/tai/codes/codetypes"
	"github.com/reusee/tai/configs"
	"github.com/reusee/tai/debugs"
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
Change blocks are applied to an in-memory store (MemoryStore) as they are parsed
from streamed model output, rather than directly to disk. This enables early
error detection: if a change block fails to apply (e.g., invalid target,
malformed code), the BlockHandler returns a *changes.ApplyError so the retry
loop can provide change-block-specific guidance — the retry discards all change
blocks from the failed attempt, so the model re-emits every intended change
block — and routes the error through OnPhaseError like any other phase error.
OnPhaseError summarizes any partial model output (thoughts or body text) before
the retry (see TheoryOfSummaryRetryOnError). The MemoryStore is reset by the
OnRoundStart callback on each retry, discarding the failed changes. When retries
are exhausted, generation stops. The in-memory store also ensures filesystem
consistency on retry: when a round is retried (missing completion block, a
phase error, or an apply error), the MemoryStore is reset, discarding all
changes without touching the disk. Only after a round succeeds are the
in-memory changes flushed to disk in a single batch, so the disk is never left
in a partially modified state by an interrupted round. Subsequent change
blocks targeting the same file within a round use the in-memory content as the
base, not the disk content, so multi-block edits to the same file are applied
correctly within the round. The streaming apply is implemented via a
BlockHandler callback on ParserState: when a complete change block is parsed
during AppendContent, the handler applies it via changes.ApplyChangeBlockStore
to the MemoryStore. The handler is built by changes.BuildChangeBlockHandler,
sharing the change-application logic with the next command. Non-change blocks
are collected by the handler into an external slice for post-phase processing
by ProcessComponents. During Flush, the handler is not called for unclosed
blocks, because they are incomplete (e.g., from truncated output) and applying
them would cause errors. Successfully applied change blocks are consumed by
the handler (not collected), so ProcessComponents finds no change blocks to
re-apply. When the apply flag is disabled, no handler is set and all blocks
are collected, preserving the no-apply behavior.
`

const maxRequestContextRounds = 5

const maxGoTestRounds = 10

const maxRetriesForMissingSummary = 3

const TheoryOfReviewLoop = `
The review loop runs after the main generation loop (or after the goal
command completes) when the -review flag is enabled. The review loop is
skipped when the session produced no applied changes — an empty diff set.
Without this, enabling -review on a session where the model emitted no
change blocks (or changes were not applied, e.g., with -no-apply) would
still initiate a wasteful review generation over an empty diff. The diff
set is derived from the in-memory store's session originals, so it is
empty exactly when no change blocks were applied to the working tree (see
changes.TheoryOfInMemoryApply). When diffs exist, the review loop opens a
fresh dscope scope so the latest filesystem state is loaded as context — the
same reset mechanism the goal command uses per loop — and runs one
generation session per configured review model, sequentially. Each review
session replaces the original chat input with a review instruction
("审核并修正这些改动") followed by the unified diff of all changes made
through the MemoryStore during the main generation session. The review
model works from an independent context and corrects potential errors in
the changes, improving accuracy. Session originals are retained across
round resets so the diff always reflects the full session delta, not only
the last round. See changes.TheoryOfInMemoryApply.
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
// plus the session diffs. See TheoryOfReviewLoop.
func (Module) RunReview(
	reset dscope.Reset,
	review Review,
	reviewModels ReviewModels,
	getDefaultGenerator generators.GetDefaultGenerator,
) RunReview {
	return func(ctx context.Context, output io.Writer, diffs []changes.FileDiff) error {
		if !bool(review) || len(diffs) == 0 {
			return nil
		}

		models := append([]string{}, reviewModels...)
		if len(models) == 0 {
			generator, err := getDefaultGenerator()
			if err != nil {
				return err
			}
			name := generator.Spec().Name
			if name == "" {
				name = generator.Spec().Model
			}
			if name != "" {
				models = append(models, name)
			}
		}

		prompt := buildReviewPrompt(diffs)
		for _, model := range models {
			scope := reset()
			scope = scope.Fork(func() flags.Chats {
				return flags.Chats([]string{prompt})
			})
			if model != "" {
				scope = scope.Fork(func() flags.ModelName {
					return flags.ModelName(model)
				})
			}
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
// When ReviewModels is empty, the default generator is used once. The
// diffs are passed to the model through Chats so they appear in the user
// prompt. See TheoryOfReviewLoop.
type RunReview func(ctx context.Context, output io.Writer, diffs []changes.FileDiff) error

const TheoryOfTokenBudgetStability = `
Accurate token budgeting preserves the prefix cache by ensuring deterministic
file inclusion across requests. Function declarations from all sources — state
layers, code/diff providers, and configuration files — are counted together
and sorted by name before measuring their token cost.
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
Round statistics are collected per round to provide visibility into token
usage and duration. Each round produces a RoundStat entry with:
- Round number (1-based)
- Prompt tokens, completion tokens, thought tokens, cached tokens
- Duration (from OnRoundStart to OnRoundSuccess)
- Summary (from summary blocks in the round)

Truncated rounds (no summary block) that are retried are recorded via
OnRoundTruncated with the summary synthesized by the retry process, so they
appear as separate loops in the statistics. The retry round itself is
recorded by OnRoundSuccess when it completes successfully.

The statistics are printed at the end of the session via a deferred call,
so they are shown even when the session ends early due to an error.
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

// collectRoundStats scans newly appended contents (after prevContentCount)
// for Usage parts and appends RoundStat entries, assigning the given
// duration and summary. It returns the updated stats and the new content
// count of the scanned state. Used by OnRoundSuccess and OnRoundTruncated
// to record round statistics. See TheoryOfRoundStatistics.
func collectRoundStats(
	roundStats []RoundStat,
	state generators.State,
	prevContentCount int,
	elapsed time.Duration,
	summary string,
) ([]RoundStat, int) {
	statsStartIdx := len(roundStats)
	contentIndex := 0
	for c := range state.Contents() {
		if contentIndex >= prevContentCount {
			for _, part := range c.Parts {
				if usage, ok := part.(generators.Usage); ok {
					roundStats = append(roundStats, RoundStat{
						Round:            len(roundStats) + 1,
						PromptTokens:     usage.Prompt.TokenCount,
						CompletionTokens: usage.Candidates.TokenCount,
						ThoughtTokens:    usage.Thoughts.TokenCount,
						CachedTokens:     usage.Prompt.TokenCountCached,
					})
				}
			}
		}
		contentIndex++
	}
	for i := statsStartIdx; i < len(roundStats); i++ {
		roundStats[i].Duration = elapsed
	}
	if summary != "" {
		if len(roundStats) > 0 {
			roundStats[len(roundStats)-1].Summary = summary
		} else {
			roundStats = append(roundStats, RoundStat{
				Round:    len(roundStats) + 1,
				Duration: elapsed,
				Summary:  summary,
			})
		}
	}
	return roundStats, contentIndex
}

// retrySummarizationSystemPrompt instructs the summarization model to
// extract the valuable conclusions of the truncated output — important
// discoveries, decisions, facts, the state of completed work, and next
// steps — rather than reproducing the reasoning that led to them.
// Truncation most often happens when thinking is too long; carrying the
// conclusions over lets the retry round adopt them instead of
// re-deriving them, reducing the thinking it needs and lowering the
// chance of truncating again. See TheoryOfIncompleteOutputSummarization.
const retrySummarizationSystemPrompt = `You are a summarization assistant. The previous model output was truncated before completion. Produce exactly two blocks:

1. A summary block (kind "summary") whose body is a concise summary of the truncated output: what the model was doing, what it had produced, and where it was interrupted.

2. A continue block (kind "continue") whose body is the retry prompt: the essence of the truncated output that the next round needs to continue from where the model left off. Truncation most often happens when thinking is too long, and the truncated thinking has already produced valuable results. The retry prompt must carry these results over — the conclusions, not the reasoning that led to them — so the next round adopts them instead of re-deriving them and needs less thinking.

Prioritize the following valuable content in the retry prompt:
- Important discoveries and insights the model reached
- Important decisions the model made
- Important facts the model established about the codebase, the task, or the environment
- The state of the work: what was completed and what remains
- The next steps the model was about to take

Output ONLY these two blocks, no other text.`

func summarizeIncompleteOutput(
	ctx context.Context,
	generator generators.Generator,
	incompleteText string,
) (*loops.RetrySummary, error) {
	if incompleteText == "" {
		return nil, nil
	}
	systemPrompt := retrySummarizationSystemPrompt
	var state generators.State
	state = generators.NewPrompts(systemPrompt, []*generators.Content{
		{
			Role: generators.RoleUser,
			Parts: []generators.Part{
				generators.Text(incompleteText),
			},
		},
	})
	var buf bytes.Buffer
	state = generators.NewOutput(state, &buf, false)
	options := &generators.GenerateOptions{
		NonStreaming: true,
	}
	_, err := generator.Generate(ctx, state, options)
	if err != nil {
		return nil, fmt.Errorf("summarization call failed: %w", err)
	}
	outputText := buf.String()
	parsedBlocks, err := blocks.ParseBlocks([]byte(outputText))
	if err != nil {
		// Fallback: use the entire output as both summary and retry prompt.
		return &loops.RetrySummary{
			Summary:     outputText,
			RetryPrompt: outputText,
		}, nil
	}
	var summary, continueContent string
	for _, block := range parsedBlocks {
		switch block.Kind {
		case "summary":
			summary = block.Body
		case "continue":
			continueContent = block.Body
		}
	}
	if summary == "" {
		summary = outputText
	}
	if continueContent == "" {
		continueContent = summary
	}
	return &loops.RetrySummary{
		Summary:     summary,
		RetryPrompt: continueContent,
	}, nil
}

func summarizeRetryState(
	errState generators.State,
	phaseErr error,
	prevContentCount int,
	summarize func(string) (*loops.RetrySummary, error),
) (newState generators.State, contentCount int, summarized bool) {
	partialText := loops.ExtractIncompleteOutput(errState, prevContentCount)
	if partialText != "" {
		if retrySummary, err := summarize(partialText); err == nil && retrySummary != nil {
			msg := "The previous generation attempt was interrupted by an error after producing partial output. " +
				"A summary is provided for context; this is a retry.\n\n" +
				"Summary of partial output:\n" + retrySummary.Summary + "\n\n" +
				"Error: " + phaseErr.Error() + "\n\n" +
				"The retry content below carries the valuable conclusions already reached in the partial output — discoveries, decisions, and facts. Adopt them; do not re-derive them, so this retry needs less thinking than the failed attempt:\n\n" +
				retrySummary.RetryPrompt
			newState, err := errState.AppendContent(&generators.Content{
				Role: generators.RoleUser,
				Parts: []generators.Part{
					generators.Text(msg),
				},
			})
			if err == nil {
				return newState, generators.CountContents(newState), true
			}
		}
	}
	newState, err := errState.AppendContent(&generators.Content{
		Role: generators.RoleLog,
		Parts: []generators.Part{
			generators.Error{Error: phaseErr},
		},
	})
	if err != nil {
		return errState, generators.CountContents(errState), false
	}
	return newState, generators.CountContents(newState), false
}

const TheoryOfSummaryCompletionRetry = `
The summary block serves as the completion signal for each generation round.
When a round ends without a summary block, or when the finish reason indicates
abnormal termination (e.g., "length" from max-token truncation), the model's
output was likely truncated mid-stream — the generation limit was reached
before the model could emit its closing summary block, or the model emitted a
summary but continued generating and was cut off. In both cases, the round is
retried from the original pre-generation State. State immutability (see
TheoryOfStateImmutability) is the foundation for this retry: the pre-generation
State is unaffected by the failed attempt, so retrying starts from a clean
snapshot rather than corrupted partial state. The retry count is bounded to
prevent infinite loops when a model consistently truncates. Change blocks from
a truncated attempt are NOT applied: the retry discards the partial output
entirely and regenerates from the pre-round state, avoiding incomplete or
malformed change blocks. This is distinct from the generator-level retry (see
TheoryOfRetry and TheoryOfGenerateRetry) which handles transient API errors;
this retry handles successful-but-incomplete output.

Completion is detected by checking the externally collected blocks for summary
kind and the finish reason in the state for abnormal termination. A round is
considered complete only when a summary block is present AND the finish reason
is not abnormal (e.g., not "length"). Because blocks are collected by the
BlockHandler during AppendContent (not stored in ParserState), the check is a
simple scan of the collected slice. The finish reason is extracted from
RoleLog content appended by the generator. On retry, the collected blocks are
reset alongside the MemoryStore in the onPhaseStart callback, ensuring both
external states are consistent with the rolled-back State. See
TheoryOfParserState in blocks/parser_state.go.

This retry is transient error recovery for truncated output. The summarized
content does not persist as compressed history. Each retry regenerates from
the original context, supplemented by the conclusions extracted from the
truncated thinking (see TheoryOfIncompleteOutputSummarization), not from
accumulated dialogue. See TheoryOfContextPhilosophy in loops/run.go.
`

const TheoryOfIncompleteOutputSummarization = `
When a round is truncated (no summary block) or errors after producing
partial output, the incomplete output is summarized before retrying. The
retry process has two tasks: producing a summary of the truncated output
(recorded as the truncated round's summary in round statistics) and
producing the content fed to the retry round as user input, framed as a
continue block. The summary provides context for the retry; the continue
block carries what the retry round should adopt.

Truncation most often happens when the model thinks too long. The
truncated reasoning is not wasted: it has already produced valuable
results — discoveries, decisions, and facts. Discarding these results and
letting the retry round re-derive them from scratch would spend the
thinking budget a second time, risking the same truncation. The retry
summarization therefore extracts the thinking results from the truncated
output — the conclusions, not the reasoning that led to them — and
carries them into the continue block fed to the retry round. The
extraction prioritizes the most valuable content: important discoveries
and insights, important decisions, important facts about the codebase or
task, the state of completed work, and the next steps the model was about
to take. The retry round adopts these pre-established conclusions and
continues from where the model left off, so it needs less thinking than
the truncated attempt.

The same extraction serves both retry paths: missing-completion retries
(truncated output) and error retries (partial output followed by an
error). See TheoryOfSummaryCompletionRetry and TheoryOfSummaryRetryOnError.

The summarization uses a fast model to minimize latency. The summary is
appended as user content with a system note explaining the retry.
`

const TheoryOfSummaryRetryOnError = `
Generation errors that occur after the model has already produced partial
output (thoughts or body text) are retried with a summarized version of that
output. Summarizing condenses the partial output into a compact user message
that preserves context while freeing budget, and changes the input so the retry
produces a different response. All generation-phase errors — including missing
completion and change-block apply errors — are routed through the same
OnPhaseError retry path with summarization, ensuring consistent retry behavior
regardless of the error type.

The summarization extracts the valuable content of the partial output —
the discoveries, decisions, and facts the model had already established —
and presents them to the retry round. The retry therefore continues from
the model's conclusions instead of re-deriving them, reducing the thinking
it needs and lowering the chance of failing again. The extraction is the
same one used for truncated output; see
TheoryOfIncompleteOutputSummarization.

This summarization is transient error recovery. The condensed content is
injected into one retry request and does not persist as compressed history.
The system does not compress conversation. See TheoryOfContextPhilosophy in
loops/run.go.
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
	getDefaultFastModel generators.GetDefaultFastModel,
	buildGenerate phases.BuildGenerate,
	maxTokens flags.MaxTokens,
	buildChat phases.BuildChat,
	tap debugs.Tap,
	buildChangeBlockHandler changes.BuildChangeBlockHandler,
	patterns Patterns,
	flagThoughts flags.Thoughts,
	summarizeThoughts states.SummarizeThoughts,
	loader configs.Loader,
	httpClient nets.HTTPClient,
	flagChats flags.Chats,
	debug Debug,
	funcDecls generators.FuncDecls,
	apply flags.Apply,
	loopRun loops.Run,
	recorder *records.Recorder,
	writeTimes *changes.FileWriteTimes,
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

		// Calculate basic limits
		maxInputTokens := min(
			spec.ContextTokens,
			int(maxTokens),
		)
		if spec.MaxGenerateTokens != nil {
			// Reserve space for reasoning and completion
			maxInputTokens -= *spec.MaxGenerateTokens * 2
		}

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

		// user prompt
		userPromptParts, err := codeProvider.Parts(maxUserPromptTokens, generator.CountTokens, patterns)
		if err != nil {
			return loops.Result{}, nil, err
		}

		// Component user prompt parts are appended after code provider parts.
		userPromptParts = append(userPromptParts, comps.UserPromptParts()...)

		// Concatenate the text parts with strings.Builder for token counting.
		// Repeated += over the file context parts is quadratic in the total
		// context size: each iteration copies the accumulated string. The
		// builder accumulates linearly. See buildUserPromptText.
		userPromptText := buildUserPromptText(userPromptParts)
		userPromptTokens, err := generator.CountTokens(userPromptText)
		if err != nil {
			return loops.Result{}, nil, err
		}
		logger.Info("user prompt ready",
			"tokens", userPromptTokens,
			"parts", len(userPromptParts),
		)

		if debug {
			fmt.Printf("system prompt: %s\n", systemPrompt)
			fmt.Printf("user prompt: %s\n", userPromptParts)
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

		// By default, raw thoughts are displayed to the user. The
		// -summarize-thoughts flag enables periodic summarization.
		// See states.TheoryOfThoughtsSummarize.
		if showThoughts && bool(summarizeThoughts) {
			summarizer, err := getDefaultSummarizer()
			if err != nil {
				return loops.Result{}, nil, err
			}
			state = generators.NewOutput(state, output, false)
			state = states.NewThoughtsSummarize(ctx, state, summarizer, output)
		} else {
			state = generators.NewOutput(state, output, showThoughts)
		}

		// The state is NOT wrapped with ParserState here; loops.Run wraps
		// it internally. See loops.TheoryOfLoops.

		// Track per-round token statistics for end-of-session reporting.
		// The stats are returned via GenerateWithResultWithStats so callers
		// that run multiple sessions (e.g., goal) can aggregate them, and
		// printed here via a deferred call so the per-session report is
		// shown even when the session ends early due to an error.
		// See TheoryOfRoundStatistics.
		var roundStats []RoundStat
		defer func() {
			PrintRoundStats(output, roundStats)
		}()

		// roundStartTime records the start of the current round, set by
		// OnRoundStart before generation and used to compute the round
		// duration in OnRoundSuccess.
		var roundStartTime time.Time

		// Get the fast model for summarization tasks.
		// See TheoryOfIncompleteOutputSummarization.
		fastModel, err := getDefaultFastModel()
		if err != nil {
			return loops.Result{}, nil, err
		}

		// Set up initial phase: if an action argument is present, append it
		// as user content and run generation; otherwise there is nothing to do.
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

		// Track content count for statistics collection in OnRoundSuccess.
		prevContentCount := generators.CountContents(state)

		// Build the change block handler when apply is enabled. The handler
		// applies change blocks immediately to the MemoryStore as they are
		// parsed during streaming, enabling early error detection. Apply
		// errors are returned as *changes.ApplyError so the loop can retry,
		// resetting the MemoryStore to discard failed changes. The handler
		// is built by changes.BuildChangeBlockHandler so the change-
		// application logic is shared with the next command. When apply is
		// disabled, no handler is set and all blocks are collected for
		// component processing. See changes.TheoryOfInMemoryApply and
		// loops.TheoryOfLoops.
		var blockHandler loops.BlockHandler
		if bool(apply) {
			handler := buildChangeBlockHandler(memStore)
			blockHandler = loops.BlockHandler(handler)
		}

		// Run the unified generation loop. See loops.TheoryOfLoops.
		// The loop handles ParserState wrapping, phase execution, retry
		// on missing completion and errors, and component processing
		// between rounds. The interaction recorder is passed explicitly
		// so every round, content, and block is captured when -record is
		// enabled.
		// See records.TheoryOfInteractionRecording.
		result, err := loopRun(ctx, loops.RunOptions{
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
				// Record the round start time for duration statistics.
				// See TheoryOfRoundStatistics.
				roundStartTime = time.Now()
			},

			OnRoundSuccess: func(roundState generators.State, summaries []string) error {
				// Flush in-memory changes to disk before the component loop
				// runs. This ensures go-test and other components see the
				// updated files on disk. See TheoryOfStreamingApply.
				if err := memStore.Flush(); err != nil {
					return err
				}

				// Compute the round duration from the start time recorded by
				// OnRoundStart. The duration covers the full round including
				// retries. See TheoryOfRoundStatistics.
				elapsed := time.Since(roundStartTime)

				// Collect round statistics from newly appended contents and
				// associate summary blocks with the current round.
				// See TheoryOfRoundStatistics.
				summaryText := ""
				if len(summaries) > 0 {
					summaryText = strings.Join(summaries, "\n")
				}
				roundStats, prevContentCount = collectRoundStats(
					roundStats, roundState, prevContentCount, elapsed, summaryText,
				)

				// If OnRoundStart is skipped for the next round (parse-error
				// correction rounds), the duration is measured from here,
				// approximating the next round's start time. See
				// TheoryOfParseErrorCollection in blocks/parser_state.go.
				roundStartTime = time.Now()
				return nil
			},

			OnRoundTruncated: func(truncatedState generators.State, retryBaseState generators.State, summary string) error {
				// Record the truncated round in round statistics so it
				// appears as a separate loop. The summary is synthesized
				// by the retry process. The retry base state's content
				// count becomes the new prevContentCount, so the retry
				// round's statistics collection starts after the retry
				// prompt. See TheoryOfRoundStatistics.
				elapsed := time.Since(roundStartTime)
				roundStats, _ = collectRoundStats(
					roundStats, truncatedState, prevContentCount, elapsed, summary,
				)
				prevContentCount = generators.CountContents(retryBaseState)
				return nil
			},

			OnPhaseError: func(errState generators.State, phaseErr error) generators.State {
				// Record the failed round in round statistics so it appears
				// as a separate loop. The failed round has no synthesized
				// summary because no retry follows. See TheoryOfRoundStatistics.
				elapsed := time.Since(roundStartTime)
				roundStats, _ = collectRoundStats(roundStats, errState, prevContentCount, elapsed, "")

				newState, newContentCount, _ := summarizeRetryState(
					errState,
					phaseErr,
					prevContentCount,
					func(text string) (*loops.RetrySummary, error) {
						return summarizeIncompleteOutput(ctx, fastModel, text)
					},
				)
				prevContentCount = newContentCount
				return newState
			},

			RetryOnMissingCompletion: true,
			RetryOnError:             true,
			MaxRetries:               maxRetriesForMissingSummary,
			SummarizeIncomplete: func(incompleteText string) (*loops.RetrySummary, error) {
				return summarizeIncompleteOutput(ctx, fastModel, incompleteText)
			},
		})

		// Surface uncorrected malformed blocks in unattended operation:
		// when the parse-error correction budget is exhausted, the
		// malformed blocks are silently not applied. Logging the count
		// makes the change loss visible. See TheoryOfLoops.
		if err == nil && len(result.ParseErrors) > 0 {
			logger.WarnContext(ctx, "uncorrected malformed blocks",
				"count", len(result.ParseErrors),
			)
		}

		// Session diffs are attached to the result for the review loop.
		// See TheoryOfReviewLoop.
		result.Diffs = memStore.Diffs()

		return result, roundStats, err
	}
}
