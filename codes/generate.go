package codes

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/reusee/tai/blocks"
	"github.com/reusee/tai/codes/codetypes"
	"github.com/reusee/tai/configs"
	"github.com/reusee/tai/debugs"
	"github.com/reusee/tai/flags"
	"github.com/reusee/tai/generators"
	"github.com/reusee/tai/logs"
	"github.com/reusee/tai/nets"
	"github.com/reusee/tai/phases"
	"github.com/reusee/tai/taiconfigs"
)

const maxRequestContextRounds = 5

const maxRetriesForMissingSummary = 3

type Generate func(ctx context.Context, output io.Writer) error

const TheoryOfTokenBudgetStability = `
Accurate token budgeting preserves the prefix cache by ensuring deterministic
file inclusion across requests. Function declarations from all sources — state
layers, code/diff providers, and configuration files — must be counted together
and sorted by name before measuring their token cost. Without config functions
in the count, the user-content budget is overestimated, which can cause context
window overflows that force file inclusion to change between requests,
invalidating the entire prefix cache.
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
cached) across the full generation session. Statistics are collected after
each successful phase execution by scanning newly appended contents for
Usage parts, and printed once at the end of the session via a deferred
call. Deferred printing avoids interleaving statistics with model output
during generation and ensures stats are reported even when the session
ends early due to an error.
`

type roundStat struct {
	Round            int
	PromptTokens     int
	CompletionTokens int
	ThoughtTokens    int
	CachedTokens     int
	Summary          string
}

func printRoundStats(w io.Writer, stats []roundStat) {
	if len(stats) == 0 {
		return
	}
	fmt.Fprintf(w, "\n=== Generation Statistics ===\n")
	fmt.Fprintf(w, "Total rounds: %d\n\n", len(stats))
	fmt.Fprintf(w, "%-6s %12s %12s %12s %12s\n", "Round", "Prompt", "Completion", "Thoughts", "Cached")
	fmt.Fprintf(w, "%-6s %12s %12s %12s %12s\n", "-----", "------", "----------", "--------", "-------")
	var totalPrompt, totalCompletion, totalThoughts, totalCached int
	for _, s := range stats {
		fmt.Fprintf(w, "%-6d %12d %12d %12d %12d\n",
			s.Round, s.PromptTokens, s.CompletionTokens, s.ThoughtTokens, s.CachedTokens)
		totalPrompt += s.PromptTokens
		totalCompletion += s.CompletionTokens
		totalThoughts += s.ThoughtTokens
		totalCached += s.CachedTokens
	}
	fmt.Fprintf(w, "%-6s %12s %12s %12s %12s\n", "-----", "------", "----------", "--------", "-------")
	fmt.Fprintf(w, "%-6s %12d %12d %12d %12d\n", "Total", totalPrompt, totalCompletion, totalThoughts, totalCached)
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

func countContents(state generators.State) int {
	count := 0
	for range state.Contents() {
		count++
	}
	return count
}

// reconcileParserState updates the *ParserState inside state with the
// currentParserState (which has consumed blocks removed) while preserving
// the upstream from state (which may have new content appended during block
// processing). This ensures consumed blocks are not reprocessed in the next
// generation round. See TheoryOfParserState.
func reconcileParserState(state generators.State, currentParserState *blocks.ParserState) generators.State {
	if currentParserState == nil {
		return state
	}
	statePs, ok := generators.As[*blocks.ParserState](state)
	if !ok {
		return state
	}
	reconciled := currentParserState.WithUpstream(statePs.Unwrap())
	if rc, ok := state.(phases.RedoCheckpoint); ok {
		return rc.WithUpstream(reconciled)
	}
	return reconciled
}

const TheoryOfSummaryCompletionRetry = `
The summary block serves as a completion signal for each generation round. When
a round ends without a summary block, the model's output was likely truncated
mid-stream — the generation limit was reached before the model could emit its
closing summary. In that case, the round is retried from the original
pre-generation State. State immutability (see TheoryOfStateImmutability) is the
foundation for this retry: the pre-generation State is unaffected by the failed
attempt, so retrying starts from a clean snapshot rather than corrupted partial
state. The retry count is bounded to prevent infinite loops when a model
consistently truncates. Change blocks from a truncated attempt are NOT applied:
the retry discards the partial output entirely and regenerates from scratch,
avoiding incomplete or malformed hunks. This is distinct from the generator-
level retry (see TheoryOfRetry and TheoryOfGenerateRetry) which handles
transient API errors; this retry handles successful-but-incomplete output.
`

// runPhaseWithRetry executes a phase and retries if no summary block is
// produced (indicating truncated output), up to maxRetriesForMissingSummary
// times. It returns the final phase result, extracted summaries, and the
// currentParserState with summary blocks consumed.
// See TheoryOfSummaryCompletionRetry.
func runPhaseWithRetry(
	ctx context.Context,
	phase phases.Phase,
	stateBeforePhase generators.State,
	fallbackParserState *blocks.ParserState,
	logger logs.Logger,
) (
	newPhase phases.Phase,
	newState generators.State,
	phaseErr error,
	summaries []string,
	currentParserState *blocks.ParserState,
) {
	for retryCount := 0; ; retryCount++ {
		newPhase, newState, phaseErr = phase(ctx, stateBeforePhase)
		if phaseErr != nil {
			return
		}

		// Extract the current *ParserState from the state chain.
		// With immutable ParserState, the original parserState pointer
		// is not updated by AppendContent; the current *ParserState is
		// the one inside the state returned by the phase chain.
		// See TheoryOfParserState.
		ps, ok := generators.As[*blocks.ParserState](newState)
		if !ok {
			ps = fallbackParserState
		}

		// Collect summary blocks from model output.
		// See TheoryOfSummaryBlocks.
		summaries, currentParserState = blocks.ProcessSummaryBlocks(ps)

		if len(summaries) > 0 {
			return
		}
		if retryCount >= maxRetriesForMissingSummary {
			logger.Info("proceeding without summary block after max retries",
				"retries", retryCount+1)
			return
		}
		logger.Info("retrying generation round: no summary block detected (likely truncated output)",
			"retry", retryCount+1, "max", maxRetriesForMissingSummary)
	}
}

func (Module) Generate(
	codeProvider codetypes.CodeProvider,
	diffHandler codetypes.DiffHandler,
	systemPrompt SystemPrompt,
	logger logs.Logger,
	getDefaultGenerator generators.GetDefaultGenerator,
	buildGenerate phases.BuildGenerate,
	maxTokens taiconfigs.MaxTokens,
	buildChat phases.BuildChat,
	tap debugs.Tap,
	patterns Patterns,
	flagThoughts flags.Thoughts,
	loader configs.Loader,
	httpClient nets.HTTPClient,
	dynamicContext DynamicContext,
	apply Apply,
	flagShell flags.Shell,
	flagChats flags.Chats,
) Generate {

	return func(ctx context.Context, output io.Writer) error {

		// Open a root on the current directory to restrict all file I/O
		// to the project tree. See TheoryOfRequestContext.
		root, err := os.OpenRoot(".")
		if err != nil {
			return err
		}
		defer root.Close()

		// generator
		generator, err := getDefaultGenerator()
		if err != nil {
			return err
		}
		args := generator.Spec()
		logger.Info("initial generator",
			"model", args.Model,
			"type", fmt.Sprintf("%T", generator),
			"base_url", args.BaseURL,
		)

		// Calculate basic limits
		maxInputTokens := min(
			args.ContextTokens,
			int(maxTokens),
		)
		if args.MaxGenerateTokens != nil {
			// Reserve space for reasoning and completion
			maxInputTokens -= *args.MaxGenerateTokens * 2
		}

		// Count tokens for fixed parts
		systemPromptTokens, err := generator.CountTokens(string(systemPrompt))
		if err != nil {
			return err
		}

		// Collect function declarations from all sources for accurate token
		// counting. Functions from state providers AND configuration files are
		// merged and sorted by name to match the order used in API requests.
		// Without config functions in the count, the user-content budget is
		// overestimated, which can cause context window overflows that force
		// file inclusion to change between requests, invalidating the prefix
		// cache. See TheoryOfTokenBudgetStability for rationale.
		var allFuncDecls []generators.FuncDecl
		if args.DisableTools != nil && !*args.DisableTools {
			for _, fn := range codeProvider.Functions() {
				allFuncDecls = append(allFuncDecls, fn.Decl)
			}
			for _, fn := range diffHandler.Functions() {
				allFuncDecls = append(allFuncDecls, fn.Decl)
			}
			for set := range configs.All[[]generators.FuncDecl](loader, "functions") {
				allFuncDecls = append(allFuncDecls, set...)
			}
			sort.SliceStable(allFuncDecls, func(i, j int) bool {
				return allFuncDecls[i].Name < allFuncDecls[j].Name
			})
		}
		funcTokens, err := countFuncsTokens(allFuncDecls, generator.CountTokens)
		if err != nil {
			return err
		}

		// Calculate remaining budget for user content
		maxUserPromptTokens := maxInputTokens - systemPromptTokens - funcTokens - 1000 // 1000 for overhead
		if maxUserPromptTokens <= 0 {
			return fmt.Errorf("token limit too low, need at least %d more", -maxUserPromptTokens)
		}
		logger.Info("token limits",
			"system", systemPromptTokens,
			"functions", funcTokens,
			"max user content", maxUserPromptTokens,
		)

		// user prompt
		userPromptParts, err := codeProvider.Parts(maxUserPromptTokens, generator.CountTokens, patterns)
		if err != nil {
			return err
		}
		// A new repository may have no code to provide as context. Allow the
		// code provider to return no parts; the user's action argument
		// (appended below before the generation loop) is sufficient to drive
		// generation in that case.
		var userPromptText generators.Text
		for _, part := range userPromptParts {
			if text, ok := part.(generators.Text); ok {
				userPromptText += text
			}
		}
		userPromptTokens, err := generator.CountTokens(string(userPromptText))
		if err != nil {
			return err
		}
		logger.Info("user prompt ready",
			"tokens", userPromptTokens,
			"parts", len(userPromptParts),
		)

		if *debug {
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
		if flagThoughts != nil {
			showThoughts = *flagThoughts
		}
		state = generators.NewOutput(state, output, showThoughts)
		if args.DisableTools != nil && !*args.DisableTools {
			state = generators.NewFuncMap(state, codeProvider.Functions()...)
			state = generators.NewFuncMap(state, diffHandler.Functions()...)
		}

		// Wrap state with ParserState to parse structured blocks from model
		// output. ParserState is always activated to support continue blocks,
		// change blocks, and request-context blocks.
		parserState := blocks.NewParserState(state)
		state = parserState

		// run
		requestContextRounds := 0

		// Set up initial phase: if an action argument is present, append it
		// as user content and start generation; otherwise there is nothing
		// to do. This inlines the former ActionChat.InitialPhase logic.
		var phase phases.Phase

		if chats := strings.Join(flagChats, "\n"); chats != "" {
			state, err = state.AppendContent(&generators.Content{
				Role: "user",
				Parts: []generators.Part{
					generators.Text(chats),
				},
			})
			if err != nil {
				return err
			}
			phase = buildGenerate(generator, nil)(nil)
		}

		// Track per-round token statistics for end-of-session reporting.
		// See TheoryOfRoundStatistics.
		var roundStats []roundStat
		defer func() {
			printRoundStats(os.Stdout, roundStats)
		}()

		for phase != nil {
			stateBeforePhase := state
			prevContentCount := countContents(stateBeforePhase)

			// Execute the phase with retry on missing summary block.
			// If the model's output lacks a summary block, it was likely
			// truncated; retry from the original state (safe because State
			// is immutable). See TheoryOfSummaryCompletionRetry.
			newPhase, newState, phaseErr, summaries, currentParserState := runPhaseWithRetry(
				ctx, phase, stateBeforePhase, parserState, logger,
			)

			if phaseErr != nil {
				// append error part
				var err error
				state, err = state.AppendContent(&generators.Content{
					Role: generators.RoleLog,
					Parts: []generators.Part{
						generators.Error{
							Error: phaseErr,
						},
					},
				})
				if err != nil {
					return err
				}

				// tap to debug
				var contents []*generators.Content
				for c := range state.Contents() {
					contents = append(contents, c)
				}
				globals := map[string]any{
					"error":          phaseErr.Error(),
					"contents":       contents,
					"system_prompts": state.SystemPrompt(),
				}
				if openAIError, ok := errors.AsType[generators.OpenAIError](phaseErr); ok {
					globals["openai"] = openAIError
				}
				tap(ctx, "codes generate error", globals)

				return phaseErr

			} else {
				// ok
				phase = newPhase
				state = newState

				// Collect round statistics from newly appended contents.
				// See TheoryOfRoundStatistics.
				contentIndex := 0
				for c := range state.Contents() {
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

				// summaries and currentParserState are already computed by
				// runPhaseWithRetry. See TheoryOfSummaryCompletionRetry.
				if len(summaries) > 0 {
					summaryText := strings.Join(summaries, "\n")
					if len(roundStats) > 0 {
						roundStats[len(roundStats)-1].Summary = summaryText
					} else {
						roundStats = append(roundStats, roundStat{
							Round:   len(roundStats) + 1,
							Summary: summaryText,
						})
					}
				}

				// Apply change blocks immediately as they are parsed from
				// model output. An apply error aborts generation.
				// See TheoryOfImmediateApply.
				if bool(apply) {
					currentParserState, err = applyChangeBlocks(currentParserState, root)
					if err != nil {
						return err
					}
				}

				// Check for request-context blocks from model output.
				// If found, fetch the requested context, append it as user
				// content, and create a new generate phase.
				// See TheoryOfRequestContext and TheoryOfDynamicContext.
				if bool(dynamicContext) {
					var hasRequestContext bool
					state, currentParserState, hasRequestContext, err = blocks.ProcessRequestContextBlocks(currentParserState, ctx, root, httpClient, state)
					if err != nil {
						return err
					}
					if hasRequestContext {
						requestContextRounds++
						if requestContextRounds > maxRequestContextRounds {
							return fmt.Errorf("max request-context rounds (%d) exceeded", maxRequestContextRounds)
						}
						// Reconcile state with currentParserState so consumed
						// blocks are not reprocessed in the next round.
						state = reconcileParserState(state, currentParserState)
						phase = buildGenerate(generator, nil)(nil)
						continue
					}
				}

				// Collect next-round user parts from shell and continue blocks.
				// Both produce user content that triggers a new generation round.
				// They are processed together so that if both are present in the
				// same response, the combined output is fed as a single user
				// message. See TheoryOfShellBlocks and TheoryOfContinueBlocks.
				var nextUserParts []generators.Part
				if flagShell {
					var shellParts []generators.Part
					shellParts, currentParserState, err = blocks.ProcessShellBlocks(currentParserState)
					if err != nil {
						return err
					}
					nextUserParts = append(nextUserParts, shellParts...)
				}
				var continueParts []generators.Part
				continueParts, currentParserState = blocks.ProcessContinueBlocks(currentParserState)
				nextUserParts = append(nextUserParts, continueParts...)
				if len(nextUserParts) > 0 {
					// Reconcile state with currentParserState so consumed
					// blocks are not reprocessed in the next round.
					state = reconcileParserState(state, currentParserState)
					state, err = state.AppendContent(&generators.Content{
						Role:  "user",
						Parts: nextUserParts,
					})
					if err != nil {
						return err
					}
					phase = buildGenerate(generator, nil)(nil)
					continue
				}

				// No shell or continue blocks: reconcile to remove consumed
				// blocks (e.g., summary, change) before the next iteration
				// so they are not reprocessed. See TheoryOfParserState.
				state = reconcileParserState(state, currentParserState)
			}
		}

		return nil
	}
}
