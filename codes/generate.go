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
Round statistics are collected per round to provide visibility into token usage
and duration. Each round produces a RoundStat entry with the 1-based round number;
prompt, completion, thought, and cached token counts; the duration (from
OnRoundStart to OnRoundSuccess); and the summary from the round's summary blocks.
Truncated rounds (no summary block) that are retried are recorded via
OnRoundTruncated with the summary synthesized by the retry process, so they appear
as separate loops in the statistics; the retry round itself is recorded by
OnRoundSuccess when it completes successfully. The statistics are printed at the
end of the session via a deferred call, so they are shown even when the session
ends early due to an error.
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

const retrySummarizationSystemPrompt = `You are a summarization assistant. The previous model output was truncated before completion. Produce exactly two blocks, and ONLY these two blocks:

1. A summary block (kind "summary") whose body is a concise summary of the truncated output: what the model was doing, what it had produced, and where it was interrupted.

2. A continue block (kind "continue") whose body is the retry prompt: the essence of the truncated output that the next round needs to continue from where the model left off. Truncation most often happens when thinking is too long, and the truncated thinking has already produced valuable results. The retry prompt must carry these results over — the conclusions, not the reasoning that led to them — so the next round adopts them instead of re-deriving them and needs less thinking.

Both blocks MUST have non-empty bodies. The continue block body MUST carry the valuable conclusions from the truncated thinking whenever any exist — discoveries, decisions, facts, completed work, next steps.

Prioritize the following valuable content in the retry prompt:
- Important discoveries and insights the model reached
- Important decisions the model made
- Important facts the model established about the codebase, the task, or the environment
- The state of the work: what was completed and what remains
- The next steps the model was about to take

**Block Format (complete example):**

<<黿鼍爩 <summary>
- The model was analyzing the parser and had identified the root cause
- It was interrupted before writing the fix
黿鼍爩

<<灪麤爨 <continue>
The root cause is the missing boundary check in the parser. Next: add the boundary check, then update the tests.
灪麤爨

The delimiters 黿鼍爩 and 灪麤爨 in the example are illustrative only: in every block emitted, choose exactly three uncommon Chinese characters as the delimiter, use a DIFFERENT trio for each block, and use the same delimiter on the closing line. The delimiter MUST NOT appear anywhere in the block body. The opening marker must start at the beginning of a line, and the closing line is the delimiter alone on its own line. Never write the placeholder text "DELIMITER" or reuse an example delimiter in a real marker.

Output ONLY these two blocks as your final text, with no other text before or after them.`

// maxSummarizeRetries bounds the number of attempts to summarize
// incomplete output when the summarize response cannot be parsed into
// summary and continue blocks. When all attempts fail, the summarization
// returns an error instead of falling back to the incomplete text. See
// TheoryOfIncompleteOutputSummarization.
const maxSummarizeRetries = 3

// SummarizeIncompleteOutput summarizes truncated or failed generation
// output before retry, producing both a summary of the truncated output
// and the retry prompt carried into the next round. The summarize
// generator, logger, and interaction recorder are bound from the dscope
// scope, so callers pass only the runtime values (context and the
// incomplete text). See TheoryOfIncompleteOutputSummarization.
type SummarizeIncompleteOutput func(
	ctx context.Context,
	incompleteText string,
) (*loops.RetrySummary, error)

func (Module) SummarizeIncompleteOutput(
	logger logs.Logger,
	recorder *records.Recorder,
	getSummarizeGenerator states.GetSummarizeGenerator,
) SummarizeIncompleteOutput {
	return func(
		ctx context.Context,
		incompleteText string,
	) (*loops.RetrySummary, error) {
		generator, err := getSummarizeGenerator()
		if err != nil {
			return nil, err
		}
		return summarizeIncompleteOutput(ctx, logger, recorder, generator, incompleteText)
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

func summarizeIncompleteOutput(
	ctx context.Context,
	logger logs.Logger,
	recorder loops.InteractionRecorder,
	generator generators.Generator,
	incompleteText string,
) (*loops.RetrySummary, error) {
	if incompleteText == "" {
		return nil, nil
	}

	systemPrompt := retrySummarizationSystemPrompt
	var lastErr error
	for attempt := range maxSummarizeRetries {
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

		// Record the summarize request. The input is recorded per attempt so
		// the interaction transcript shows each retry of the summarization.
		if recorder != nil && recorder.Enabled() {
			recorder.Content(&generators.Content{
				Role: generators.RoleUser,
				Parts: []generators.Part{
					generators.Text(fmt.Sprintf("Summarize request (attempt %d):\n\n%s", attempt+1, incompleteText)),
				},
			})
		}

		_, err := generator.Generate(ctx, state, options)
		if err != nil {
			lastErr = err
			logger.WarnContext(ctx, "summarize incomplete output: generation failed",
				"attempt", attempt+1,
				"max_attempts", maxSummarizeRetries,
				"err", err,
			)
			// Record the failure so the transcript shows why the attempt failed.
			if recorder != nil && recorder.Enabled() {
				recorder.Content(&generators.Content{
					Role: generators.RoleLog,
					Parts: []generators.Part{
						generators.Error{Error: err},
					},
				})
			}
			continue
		}
		outputText := buf.String()

		// Record the summarize response. The raw output is recorded before
		// parsing, so even a malformed response is visible in the transcript.
		if recorder != nil && recorder.Enabled() {
			recorder.Content(&generators.Content{
				Role: generators.RoleModel,
				Parts: []generators.Part{
					generators.Text(fmt.Sprintf("Summarize response (attempt %d):\n\n%s", attempt+1, outputText)),
				},
			})
		}

		parsedBlocks, err := blocks.ParseBlocks([]byte(outputText))
		if err != nil {
			lastErr = err
			logger.WarnContext(ctx, "summarize incomplete output: parse failed",
				"attempt", attempt+1,
				"max_attempts", maxSummarizeRetries,
				"err", err,
			)
			continue
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
		if summary != "" && continueContent != "" {
			return &loops.RetrySummary{
				Summary:     summary,
				RetryPrompt: continueContent,
			}, nil
		}
		lastErr = fmt.Errorf("summarize response missing summary or continue block")
		logger.WarnContext(ctx, "summarize incomplete output: response missing summary or continue block",
			"attempt", attempt+1,
			"max_attempts", maxSummarizeRetries,
		)
	}
	if lastErr != nil {
		err := fmt.Errorf("summarize incomplete output failed after %d attempts: %w", maxSummarizeRetries, lastErr)
		logger.ErrorContext(ctx, "summarize incomplete output failed",
			"max_attempts", maxSummarizeRetries,
			"err", err,
		)
		return nil, err
	}
	return nil, fmt.Errorf("summarize incomplete output failed after %d attempts", maxSummarizeRetries)
}

// summarizeRetryState prepares the retry state for a phase error. When the
// partial output can be summarized, the summary and retry prompt are appended
// as user content. When summarization fails, the error is propagated so the
// caller aborts the run; a fallback state is still returned for validity.
// See TheoryOfIncompleteOutputSummarization.
func summarizeRetryState(
	errState generators.State,
	phaseErr error,
	prevContentCount int,
	summarize func(string) (*loops.RetrySummary, error),
) (newState generators.State, contentCount int, summary string, err error) {
	partialText := loops.ExtractIncompleteOutput(errState, prevContentCount)
	if partialText != "" {
		if retrySummary, summarizeErr := summarize(partialText); summarizeErr != nil {
			// The summarization failure is a serious error: propagate it so
			// the caller aborts the run instead of continuing without a
			// synthesized summary. The fallback state is still returned so
			// the caller has a valid state while aborting. See
			// TheoryOfIncompleteOutputSummarization.
			fallbackState, fallbackCount, fallbackSummary := fallbackRetryState(errState, phaseErr)
			return fallbackState, fallbackCount, fallbackSummary, summarizeErr
		} else if retrySummary != nil {
			msg := "The previous generation attempt was interrupted by an error after producing partial output. " +
				"A summary is provided for context; this is a retry.\n\n" +
				loops.FormatSummaryBlock(retrySummary.Summary) + "\n\n" +
				"Error: " + phaseErr.Error() + "\n\n" +
				"The retry content below carries the valuable conclusions already reached in the partial output — discoveries, decisions, and facts. Adopt them; do not re-derive them, so this retry needs less thinking than the failed attempt:\n\n" +
				retrySummary.RetryPrompt
			newState, appendErr := errState.AppendContent(&generators.Content{
				Role: generators.RoleUser,
				Parts: []generators.Part{
					generators.Text(msg),
				},
			})
			if appendErr == nil {
				return newState, generators.CountContents(newState), retrySummary.Summary, nil
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
			generators.Text(loops.FormatSummaryBlock("[Error: " + phaseErr.Error() + "]")),
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
generating and was cut off. In both cases, the round is retried from the original
pre-generation State. State immutability (see TheoryOfStateImmutability in
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

This retry is transient error recovery for truncated output. The summarized
content does not persist as compressed history. Each retry regenerates from the
original context, supplemented by the conclusions extracted from the truncated
thinking (see TheoryOfIncompleteOutputSummarization), not from accumulated
dialogue. See TheoryOfContextPhilosophy in loops/run.go.
`

const TheoryOfIncompleteOutputSummarization = `
When a round is truncated (no summary block) or errors after producing partial
output, the incomplete output is summarized before retrying. The retry process has
two tasks: producing a summary of the truncated output (recorded as the truncated
round's summary in round statistics) and producing the content fed to the retry
round as user input, framed as a continue block. The summary provides context for
the retry; the continue block carries what the retry round should adopt.

Truncation most often happens when the model thinks too long. The truncated
reasoning is not wasted: it has already produced valuable results — discoveries,
decisions, and facts. Discarding these results and letting the retry round
re-derive them from scratch would spend the thinking budget a second time, risking
the same truncation. The retry summarization therefore extracts the thinking
results from the truncated output — the conclusions, not the reasoning that led to
them — and carries them into the continue block fed to the retry round. The
extraction prioritizes the most valuable content: important discoveries and
insights, important decisions, important facts about the codebase or task, the
state of completed work, and the next steps the model was about to take. The retry
round adopts these pre-established conclusions and continues from where the model
left off, so it needs less thinking than the truncated attempt.

The same extraction serves both retry paths: missing-completion retries (truncated
output) and error retries (partial output followed by an error). See
TheoryOfSummaryCompletionRetry and TheoryOfSummaryRetryOnError.

The summarization model follows SummarizeModel, falling back to the fast model
and then the default model (see states.TheoryOfSummarizeModel). The summary is
appended as user content with a system note explaining the retry.

The summarization system prompt (retrySummarizationSystemPrompt) shows a complete
example of the summary and continue block format with concrete delimiters, so
the model emits parseable blocks rather than plain text.

The summarization itself is retried when its response cannot be parsed into
summary and continue blocks, or when the summarize generation fails: a malformed
or incomplete summarize response would otherwise leave the retry round without a
summary or with a degraded retry prompt, and a transient API error would leave
the round without any summary at all. The retry is bounded by maxSummarizeRetries;
when all attempts fail, the summarization returns an error instead of falling
back to the incomplete text. A fallback that substitutes the raw incomplete text
as both the summary and the retry prompt would feed the model unstructured,
possibly truncated reasoning as if it were a distilled summary, degrading the
retry prompt's quality and masking the summarization failure. The summarization
failure is a serious error, not a soft "no summary available" condition: it
propagates as a generation error and aborts the run. Continuing without a
synthesized summary would hide the truncation, leave the retry round without the
distilled conclusions it needs, and degrade the retry prompt's quality; the
operator must see the failure and intervene.

Each failed summarize attempt is logged with the attempt number and the error,
and the final failure is logged as an error, so the operator can diagnose why a
round aborted.
`

const TheoryOfSummaryRetryOnError = `
Generation errors that occur after the model has already produced partial output
(thoughts or body text) are retried with a summarized version of that output.
Summarizing condenses the partial output into a compact user message that
preserves context while freeing budget, and changes the input so the retry produces
a different response. All generation-phase errors — including missing completion
and change-block apply errors — are routed through the same OnPhaseError retry path
with summarization, ensuring consistent retry behavior regardless of the error
type.

The summarization extracts the valuable content of the partial output — the
discoveries, decisions, and facts the model had already established — and presents
them to the retry round. The retry therefore continues from the model's conclusions
instead of re-deriving them, reducing the thinking it needs and lowering the chance
of failing again. The extraction is the same one used for truncated output; see
TheoryOfIncompleteOutputSummarization.

This summarization is transient error recovery. The condensed content is injected
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
	getSummarizeGenerator states.GetSummarizeGenerator,
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
	summarizeIncompleteOutput SummarizeIncompleteOutput,
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

		// summarize generator
		summarizeGenerator, err := getSummarizeGenerator()
		if err != nil {
			return loops.Result{}, nil, err
		}
		if recorder != nil && recorder.Enabled() {
			recorder.Event("decision", fmt.Sprintf("summarize generator selected: model=%s", summarizeGenerator.Spec().Model))
		}

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
		if recorder != nil && recorder.Enabled() {
			recorder.Event("decision", fmt.Sprintf("user prompt assembled: parts=%d tokens=%d", len(userPromptParts), userPromptTokens))
		}

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
			// The summary writer defaults to the generation output
			// stream — the same stream the raw thoughts would have
			// used. A display front-end (e.g., the TUI) routes the
			// summaries to its own display by forking
			// states.ThoughtSummaryWriter. See
			// states.TheoryOfThoughtsSummarize.
			summaryWriter := output
			if thoughtSummaryWriter != nil {
				summaryWriter = thoughtSummaryWriter
			}
			state = states.NewThoughtsSummarize(ctx, state, summarizer, summaryWriter)
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

		// Set up initial phase: if an action argument is present, append it
		// as user content and run generation; otherwise there is nothing to do.
		var hasChats bool
		if chats := strings.Join(flagChats, "\n"); chats != "" {
			// Wrap the chat in user-input markers so the TUI can extract
			// it from the merged user prompt (stripFileContext), matching
			// the ai command's structure. Without the markers, the chat
			// is a bare Text part that Prompts.AppendContent may merge
			// with the preceding file-context or restate-prompt text,
			// and the TUI cannot distinguish it from context, so the
			// user's input never appears in the Output tab.
			// See TheoryOfTUI.
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
		// enabled. The result is filled into result as the run
		// progresses; the iterator yields the terminal error, if any.
		// See records.TheoryOfInteractionRecording.
		//
		// runCtx is cancellable so a serious error — a failure to
		// summarize incomplete output — can abort the run from within a
		// callback that cannot return an error (OnPhaseError). fatalErr
		// records the serious error; after the loop it overrides the
		// loop's terminal error. See TheoryOfIncompleteOutputSummarization.
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
				if recorder != nil && recorder.Enabled() {
					recorder.Event("decision", "round succeeded: in-memory changes flushed to disk")
				}

				// Compute the round duration from the start time recorded by
				// OnRoundStart. The duration covers the full round including
				// retries. See TheoryOfRoundStatistics.
				elapsed := time.Since(roundStartTime)

				// Collect round statistics from newly appended contents and
				// associate summary blocks with the current round.
				// See TheoryOfRoundStatistics.
				summaryText := ""
				var summarizeErr error
				if len(summaries) > 0 {
					summaryText = strings.Join(summaries, "\n")
				} else {
					// The round produced no summary blocks (e.g., retries
					// were exhausted and the final attempt still lacked a
					// summary). Summarize the round's output so every round
					// has a summary for the round statistics and the TUI's
					// Round tab. See TheoryOfIncompleteOutputSummarization.
					if incompleteText := loops.ExtractIncompleteOutput(roundState, prevContentCount); incompleteText != "" {
						var retrySummary *loops.RetrySummary
						retrySummary, summarizeErr = summarizeIncompleteOutput(runCtx, incompleteText)
						if summarizeErr == nil && retrySummary != nil {
							summaryText = retrySummary.Summary
						}
					}
				}
				roundStats, prevContentCount = collectRoundStats(
					roundStats, roundState, prevContentCount, elapsed, summaryText,
				)

				if summarizeErr != nil {
					// A failure to summarize incomplete output is a serious
					// error: abort the run instead of continuing without a
					// synthesized summary. See
					// TheoryOfIncompleteOutputSummarization.
					fatalErr = summarizeErr
					cancel()
					return nil
				}

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
				// If a serious error already aborted the run, do not
				// attempt further summarization or retry preparation.
				if fatalErr != nil {
					return errState
				}

				// Record the failed round in round statistics so it appears
				// as a separate loop. The failed round's summary is
				// synthesized by the retry process. See TheoryOfRoundStatistics.
				elapsed := time.Since(roundStartTime)
				newState, newContentCount, summary, summarizeErr := summarizeRetryState(
					errState,
					phaseErr,
					prevContentCount,
					func(text string) (*loops.RetrySummary, error) {
						return summarizeIncompleteOutput(runCtx, text)
					},
				)
				roundStats, _ = collectRoundStats(roundStats, errState, prevContentCount, elapsed, summary)
				prevContentCount = newContentCount

				if summarizeErr != nil {
					// A failure to summarize the partial output is a serious
					// error: abort the run instead of retrying without a
					// synthesized summary. See
					// TheoryOfIncompleteOutputSummarization.
					fatalErr = summarizeErr
					cancel()
				}

				return newState
			},

			RetryOnMissingCompletion: true,
			RetryOnError:             true,
			MaxRetries:               maxRetriesForMissingSummary,
			SummarizeIncomplete: func(incompleteText string) (*loops.RetrySummary, error) {
				if fatalErr != nil {
					return nil, fatalErr
				}
				retrySummary, err := summarizeIncompleteOutput(runCtx, incompleteText)
				if err != nil {
					// A failure to summarize incomplete output is a serious
					// error: abort the run instead of continuing without a
					// synthesized summary. See
					// TheoryOfIncompleteOutputSummarization.
					fatalErr = err
					cancel()
				}
				return retrySummary, err
			},
		}, &result) {
			err = e
		}

		// A serious error that aborted the run (e.g., a failure to
		// summarize incomplete output) overrides the loop's terminal
		// error, which may be a context cancellation caused by the abort.
		// See TheoryOfIncompleteOutputSummarization.
		if fatalErr != nil {
			err = fatalErr
		}

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
		// On error, the failed round's in-memory changes were never flushed
		// to disk; resetting the store keeps the diffs limited to changes
		// actually written, so the review loop never sees phantom changes.
		// See TheoryOfReviewLoop and changes.TheoryOfInMemoryApply.
		if err != nil {
			memStore.Reset()
		}
		result.Diffs = memStore.Diffs()

		return result, roundStats, err
	}
}
