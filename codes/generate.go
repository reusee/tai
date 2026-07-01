package codes

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"

	"github.com/reusee/tai/codes/codetypes"
	"github.com/reusee/tai/configs"
	"github.com/reusee/tai/debugs"
	"github.com/reusee/tai/flags"
	"github.com/reusee/tai/generators"
	"github.com/reusee/tai/logs"
	"github.com/reusee/tai/phases"
	"github.com/reusee/tai/taiconfigs"
)

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
) Generate {

	return func(ctx context.Context, output io.Writer) error {

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
		if len(userPromptParts) == 0 {
			return fmt.Errorf("code provider returned no content, check patterns and file selection")
		}
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
		)

		if *debug {
			fmt.Printf("system prompt: %s\n", systemPrompt)
			fmt.Printf("user prompt: %s\n", userPromptParts)
		}

		// initial state
		var state generators.State
		state = generators.NewPrompts(
			string(systemPrompt),
			[]*generators.Content{
				{
					Role:  "user",
					Parts: userPromptParts,
				},
			},
		)
		state = generators.NewOutput(state, output, bool(flagThoughts))
		if args.DisableTools != nil && !*args.DisableTools {
			state = generators.NewFuncMap(state, codeProvider.Functions()...)
			state = generators.NewFuncMap(state, diffHandler.Functions()...)
		}

		// run
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

				// let user determine what to do
				if *noChat {
					return phaseErr
				}
				phase = buildChat(generator, nil)(phase)

			} else {
				// ok
				phase = newPhase
				state = newState
			}
		}

		return nil
	}
}