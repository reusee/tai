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

type Generate func(ctx context.Context, output io.Writer) error

// GenerateWithResult runs the full codes generation pipeline and returns the
// loops.Result, which includes the final state and any remaining (unconsumed)
// blocks. Used by the goal command to detect goal completion via done blocks
// in Result.RemainingBlocks. See TheoryOfGoalCommand in cmd/tai/goal.go.
type GenerateWithResult func(ctx context.Context, output io.Writer) (loops.Result, error)

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

const TheoryOfRoundStatistics = `
Round statistics track per-round token usage (prompt, completion, thoughts,
cached) and running time across the full generation session. Statistics are
collected after each successful phase execution by scanning newly appended
contents for Usage parts, and printed once at the end of the session via a
deferred call. Deferred printing avoids interleaving statistics with model
output during generation and ensures stats are reported even when the session
ends early due to an error. The round duration is measured from the OnRoundStart
callback to OnRoundSuccess, covering the full round including retries; the
elapsed time is assigned to every stat entry produced by the round.
`

type roundStat struct {
	Round            int
	PromptTokens     int
	CompletionTokens int
	ThoughtTokens    int
	CachedTokens     int
	Duration         time.Duration
	Summary          string
}

func printRoundStats(w io.Writer, stats []roundStat) {
	if len(stats) == 0 {
		return
	}
	fmt.Fprintf(w, "\n=== Generation Statistics ===\n")
	fmt.Fprintf(w, "Total rounds: %d\n\n", len(stats))
	fmt.Fprintf(w, "%-6s %12s %12s %12s %12s %12s\n", "Round", "Prompt", "Completion", "Thoughts", "Cached", "Duration")
	fmt.Fprintf(w, "%-6s %12s %12s %12s %12s %12s\n", "-----", "------", "----------", "--------", "-------", "--------")
	var totalPrompt, totalCompletion, totalThoughts, totalCached int
	var totalDuration time.Duration
	for _, s := range stats {
		fmt.Fprintf(w, "%-6d %12d %12d %12d %12d %12s\n",
			s.Round, s.PromptTokens, s.CompletionTokens, s.ThoughtTokens, s.CachedTokens,
			s.Duration.Round(time.Millisecond).String())
		totalPrompt += s.PromptTokens
		totalCompletion += s.CompletionTokens
		totalThoughts += s.ThoughtTokens
		totalCached += s.CachedTokens
		totalDuration += s.Duration
	}
	fmt.Fprintf(w, "%-6s %12s %12s %12s %12s %12s\n", "-----", "------", "----------", "--------", "-------", "--------")
	fmt.Fprintf(w, "%-6s %12d %12d %12d %12d %12s\n", "Total", totalPrompt, totalCompletion, totalThoughts, totalCached,
		totalDuration.Round(time.Millisecond).String())
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
				fmt.Fprintf(w, "Round %d: %s\n", s.Round, s.Summary)
			}
		}
		fmt.Fprintf(w, "==============================\n")
	}
}

func summarizeIncompleteOutput(
	ctx context.Context,
	generator generators.Generator,
	incompleteText string,
) (string, error) {
	if incompleteText == "" {
		return "", nil
	}
	systemPrompt := "You are a summarization assistant. Summarize the following incomplete model output concisely. Output ONLY a summary block with your summary. Do not include any other text."
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
		return "", fmt.Errorf("summarization call failed: %w", err)
	}
	outputText := buf.String()
	block, _, _, ok, err := blocks.ParseFirstBlock([]byte(outputText))
	if err != nil || !ok || block.Kind != "summary" {
		// Fallback: use the entire output as summary
		return outputText, nil
	}
	return block.Body, nil
}

// summarizeRetryState appends a summarized retry message to errState when the
// model already produced partial output, so the retry has condensed context.
// It returns the new state, the new content count, and whether summarization
// was used. See TheoryOfSummaryRetryOnError.
func summarizeRetryState(
	errState generators.State,
	phaseErr error,
	prevContentCount int,
	summarize func(string) (string, error),
) (newState generators.State, contentCount int, summarized bool) {
	partialText := loops.ExtractIncompleteOutput(errState, prevContentCount)
	if partialText != "" {
		if summary, err := summarize(partialText); err == nil && summary != "" {
			msg := "The previous generation attempt was interrupted by an error after producing partial output. " +
				"A summary is provided for context; this is a retry.\n\n" +
				"Summary of partial output:\n" + summary + "\n\n" +
				"Error: " + phaseErr.Error()
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
entirely and regenerates from scratch, avoiding incomplete or malformed change
blocks. This is distinct from the generator-level retry (see TheoryOfRetry and
TheoryOfGenerateRetry) which handles transient API errors; this retry handles
successful-but-incomplete output.

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
the original context, not from accumulated dialogue. See
TheoryOfContextPhilosophy in loops/run.go.
`

const TheoryOfIncompleteOutputSummarization = `
When a generation round produces incomplete output (no summary block),
the partial output is summarized via a separate model call before retrying.
The fast model (configured via fast_model or fast_model_name in tai.cue) is
used for this summarization via GetDefaultFastModel, not the main generation
model, to minimize latency and cost. The summary provides context about what
was partially generated, and changes the input to the model so that the retry
attempt produces a different output. The summary is requested via a summary
block in the summarization prompt, and the parsed summary text is appended
as a user message to the original state before retrying. This keeps the main
conversation history clean while injecting the condensed context.
The summary is prefixed with an explanatory note informing the model that the
previous output was truncated and that this is a retry.

This summarization is transient error recovery, not conversation compression.
The summary is injected into one retry request and then discarded. The system
does not maintain or compress dialogue history. See TheoryOfContextPhilosophy
in loops/run.go.
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

This summarization is transient error recovery. The condensed content is
injected into one retry request and does not persist as compressed history.
The system does not compress conversation. See TheoryOfContextPhilosophy in
loops/run.go.
`

// Generate wraps GenerateWithResult, discarding the loops.Result so existing
// callers (go, any) see the same func(ctx, output) error signature.
// Callers that need the result (e.g., goal command) should use
// GenerateWithResult directly.
func (Module) Generate(
	generateWithResult GenerateWithResult,
) Generate {
	return func(ctx context.Context, output io.Writer) error {
		_, err := generateWithResult(ctx, output)
		return err
	}
}

func (Module) GenerateWithResult(
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
) GenerateWithResult {

	return func(ctx context.Context, output io.Writer) (loops.Result, error) {

		// Open a root on the current directory to restrict all file I/O
		// to the project tree. See TheoryOfRequestContext.
		root, err := os.OpenRoot(".")
		if err != nil {
			return loops.Result{}, err
		}
		defer root.Close()

		// MemoryStore buffers change block modifications in memory during
		// streaming, deferring disk writes until the round succeeds.
		// See TheoryOfStreamingApply and changes.TheoryOfInMemoryApply.
		memStore := changes.NewMemoryStore(changes.NewRootStore(root))

		// generator
		generator, err := getDefaultGenerator()
		if err != nil {
			return loops.Result{}, err
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
			return loops.Result{}, err
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
			return loops.Result{}, err
		}

		// Calculate remaining budget for user content
		maxUserPromptTokens := maxInputTokens - systemPromptTokens - funcTokens - 1000
		if maxUserPromptTokens <= 0 {
			return loops.Result{}, fmt.Errorf("token limit too low, need at least %d more", -maxUserPromptTokens)
		}
		logger.Info("token limits",
			"system", systemPromptTokens,
			"functions", funcTokens,
			"max user content", maxUserPromptTokens,
		)

		// user prompt
		userPromptParts, err := codeProvider.Parts(maxUserPromptTokens, generator.CountTokens, patterns)
		if err != nil {
			return loops.Result{}, err
		}

		// Component user prompt parts are appended after code provider parts.
		userPromptParts = append(userPromptParts, comps.UserPromptParts()...)

		var userPromptText generators.Text
		for _, part := range userPromptParts {
			if text, ok := part.(generators.Text); ok {
				userPromptText += text
			}
		}
		userPromptTokens, err := generator.CountTokens(string(userPromptText))
		if err != nil {
			return loops.Result{}, err
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
				return loops.Result{}, err
			}
			state = generators.NewOutput(state, output, false)
			state = states.NewThoughtsSummarize(ctx, state, summarizer, output)
		} else {
			state = generators.NewOutput(state, output, showThoughts)
		}

		// The state is NOT wrapped with ParserState here; loops.Run wraps
		// it internally. See loops.TheoryOfLoops.

		// Track per-round token statistics for end-of-session reporting.
		// See TheoryOfRoundStatistics.
		var roundStats []roundStat
		defer func() {
			printRoundStats(output, roundStats)
		}()

		// roundStartTime records the start of the current round, set by
		// OnRoundStart before generation and used to compute the round
		// duration in OnRoundSuccess.
		var roundStartTime time.Time

		// Get the fast model for summarization tasks.
		// See TheoryOfIncompleteOutputSummarization.
		fastModel, err := getDefaultFastModel()
		if err != nil {
			return loops.Result{}, err
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
				return loops.Result{}, err
			}
			hasChats = true
		}

		if !hasChats {
			return loops.Result{}, nil
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
		// between rounds.
		result, err := loopRun(ctx, loops.RunOptions{
			Generator:    generator,
			InitialState: state,
			Components:   comps.ComponentSet,
			BlockHandler: blockHandler,
			PhaseBuilder: func(g generators.Generator) phases.Phase {
				return buildGenerate(g, nil)(nil)
			},
			Root:       root,
			HTTPClient: httpClient,

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
				statsStartIdx := len(roundStats)

				// Collect round statistics from newly appended contents.
				// See TheoryOfRoundStatistics.
				contentIndex := 0
				for c := range roundState.Contents() {
					if contentIndex >= prevContentCount {
						for _, part := range c.Parts {
							if usage, ok := part.(generators.Usage); ok {
								roundStats = append(roundStats, roundStat{
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
				prevContentCount = contentIndex

				// Assign the round duration to all stat entries created in
				// this round. A round may produce multiple Usage parts (e.g.,
				// when retries occur); all share the same round duration.
				for i := statsStartIdx; i < len(roundStats); i++ {
					roundStats[i].Duration = elapsed
				}

				// Associate summary blocks with the current round.
				if len(summaries) > 0 {
					summaryText := strings.Join(summaries, "\n")
					if len(roundStats) > 0 {
						roundStats[len(roundStats)-1].Summary = summaryText
					} else {
						roundStats = append(roundStats, roundStat{
							Round:    len(roundStats) + 1,
							Duration: elapsed,
							Summary:  summaryText,
						})
					}
				}

				// If OnRoundStart is skipped for the next round (parse-error
				// correction rounds), the duration is measured from here,
				// approximating the next round's start time. See
				// TheoryOfParseErrorCollection in blocks/parser_state.go.
				roundStartTime = time.Now()
				return nil
			},

			OnPhaseError: func(errState generators.State, phaseErr error) generators.State {
				newState, newContentCount, _ := summarizeRetryState(
					errState,
					phaseErr,
					prevContentCount,
					func(text string) (string, error) {
						return summarizeIncompleteOutput(ctx, fastModel, text)
					},
				)
				prevContentCount = newContentCount
				return newState
			},

			RetryOnMissingCompletion: true,
			RetryOnError:             true,
			MaxRetries:               maxRetriesForMissingSummary,
			SummarizeIncomplete: func(incompleteText string) (string, error) {
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

		return result, err
	}
}
