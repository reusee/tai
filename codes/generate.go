package codes

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"

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

const TheoryOfContinueBlocks = `
Continue blocks allow the model to self-drive multi-turn generation by emitting
a continue block at the end of a response when the task is not yet complete.
The system parses the continue block, extracts its body as the next user message,
and automatically starts a new generation round. This enables the model to
produce arbitrarily long outputs by chaining multiple rounds. Each round must
end with either a finish block (task complete) or a continue block (more work
needed), but not both.

The primary trigger for using continue blocks is the number of expected change
blocks: when a task requires more than approximately 5-7 change blocks, the
model should decompose it into multiple rounds. Secondary triggers include
natural phase boundaries (e.g., interface refactoring followed by caller
updates) and dependency chains where later steps depend on earlier results.
Each round should produce a coherent, reviewable set of changes; prefer fewer,
larger rounds over many tiny rounds to minimize round-trip overhead.

For complex tasks, the model maintains a task list in the continue block body.
In each round, the model selects one or more tasks from the list to execute,
produces the corresponding change blocks, and ends with a continue block
containing the updated task list — marking completed tasks and listing
remaining tasks. This cycle repeats until all tasks are complete, at which
point a finish block is used instead. This avoids hitting the single-request
generation limit and keeps each round focused and reviewable.
Simple tasks that fit within a single response need not be decomposed.
`

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

func (Module) Generate(
	codeProvider codetypes.CodeProvider,
	diffHandler codetypes.DiffHandler,
	systemPrompt SystemPrompt,
	logger logs.Logger,
	actionChat ActionChat,
	maxTokens taiconfigs.MaxTokens,
	buildChat phases.BuildChat,
	tap debugs.Tap,
	patterns Patterns,
	flagThoughts flags.Thoughts,
	loader configs.Loader,
	httpClient nets.HTTPClient,
	dynamicContext DynamicContext,
	apply Apply,
	shell Shell,
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
		generator, err := actionChat.GetDefaultGenerator()()
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
		// (appended later by ActionChat.InitialPhase) is sufficient to drive
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

		// Wrap state with BlockState to parse structured blocks from model
		// output. BlockState is always activated to support continue blocks,
		// change blocks, and request-context blocks.
		blockState := blocks.NewBlockState(state)
		state = blockState

		// run
		requestContextRounds := 0
		phase := actionChat.InitialPhase(nil)
		for phase != nil {
			newPhase, newState, phaseErr := phase(ctx, state)

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

				// Apply change blocks immediately as they are parsed from
				// model output. An apply error aborts generation.
				// See TheoryOfImmediateApply.
				if bool(apply) {
					if err := applyChangeBlocks(blockState, root); err != nil {
						return err
					}
				}

				// Check for request-context blocks from model output.
				// If found, fetch the requested context, append it as user
				// content, and create a new generate phase.
				// See TheoryOfRequestContext and TheoryOfDynamicContext.
				if bool(dynamicContext) {
					var hasRequestContext bool
					state, hasRequestContext, err = blocks.ProcessRequestContextBlocks(blockState, ctx, root, httpClient, state)
					if err != nil {
						return err
					}
					if hasRequestContext {
						requestContextRounds++
						if requestContextRounds > maxRequestContextRounds {
							return fmt.Errorf("max request-context rounds (%d) exceeded", maxRequestContextRounds)
						}
						phase = actionChat.BuildGenerate()(generator, nil)(nil)
						continue
					}
				}

				// Collect next-round user parts from shell and continue blocks.
				// Both produce user content that triggers a new generation round.
				// They are processed together so that if both are present in the
				// same response, the combined output is fed as a single user
				// message. See TheoryOfShellBlocks and TheoryOfContinueBlocks.
				var nextUserParts []generators.Part
				if bool(shell) {
					parts, err := processShellBlocks(blockState)
					if err != nil {
						return err
					}
					nextUserParts = append(nextUserParts, parts...)
				}
				if continueBlocks := blockState.PopBlocksByKind("continue"); len(continueBlocks) > 0 {
					for _, block := range continueBlocks {
						nextUserParts = append(nextUserParts, generators.Text(block.Body))
					}
				}
				if len(nextUserParts) > 0 {
					var err error
					state, err = state.AppendContent(&generators.Content{
						Role:  "user",
						Parts: nextUserParts,
					})
					if err != nil {
						return err
					}
					phase = actionChat.BuildGenerate()(generator, nil)(nil)
					continue
				}
			}
		}

		return nil
	}
}

// processContinueBlocks checks for continue blocks in the block state and,
// if present, appends the first continue block's body as a user message to
// the state. It returns the new state, a boolean indicating whether a
// continue block was found, and any error.
func processContinueBlocks(blockState *blocks.BlockState, state generators.State) (generators.State, bool, error) {
	if blockState == nil {
		return state, false, nil
	}
	continueBlocks := blockState.PopBlocksByKind("continue")
	if len(continueBlocks) == 0 {
		return state, false, nil
	}
	// Use the first continue block's body as the next user content
	nextUserContent := continueBlocks[0].Body
	newState, err := state.AppendContent(&generators.Content{
		Role: "user",
		Parts: []generators.Part{
			generators.Text(nextUserContent),
		},
	})
	if err != nil {
		return state, false, err
	}
	return newState, true, nil
}
