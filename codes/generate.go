package codes

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/reusee/tai/cmds"
	"github.com/reusee/tai/codes/codetypes"
	"github.com/reusee/tai/debugs"
	"github.com/reusee/tai/generators"
	"github.com/reusee/tai/logs"
	"github.com/reusee/tai/phases"
	"github.com/reusee/tai/taiconfigs"
)

type Generate func(ctx context.Context, output io.Writer) error

var showThoughts = cmds.Switch("-thoughts")

func (Module) Generate(
	codeProvider codetypes.CodeProvider,
	diffHandler codetypes.DiffHandler,
	systemPrompt SystemPrompt,
	logger logs.Logger,
	action Action,
	maxTokens taiconfigs.MaxTokens,
	buildChat phases.BuildChat,
	tap debugs.Tap,
	patterns Patterns,
) Generate {

	return func(ctx context.Context, output io.Writer) error {

		// generator
		generator, err := action.InitialGenerator()
		if err != nil {
			return err
		}
		args := generator.Args()
		logger.Info("initial generator",
			"model", args.Model,
			"type", fmt.Sprintf("%T", generator),
			"base_url", args.BaseURL,
		)

		// tokens
		maxInputTokens := min(
			args.ContextTokens,
			int(maxTokens),
		)
		if args.MaxGenerateTokens != nil {
			maxInputTokens -= *args.MaxGenerateTokens * 2
		}
		systemPromptTokens, err := generator.CountTokens(string(systemPrompt))
		if err != nil {
			return err
		}
		maxUserPromptTokens := maxInputTokens - systemPromptTokens
		if maxUserPromptTokens <= 0 {
			return fmt.Errorf("token limit too low, need at least %d", -maxUserPromptTokens)
		}
		logger.Info("token limits",
			"max user prompt", maxUserPromptTokens,
		)

		// user prompt
		userPromptParts, err := codeProvider.Parts(maxUserPromptTokens, generator.CountTokens, patterns)
		if err != nil {
			return err
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
		state = generators.NewOutput(state, output, *showThoughts)
		if !args.DisableTools {
			state = generators.NewFuncMap(state, codeProvider.Functions()...)
			state = generators.NewFuncMap(state, diffHandler.Functions()...)
		}

		// run
		phase := action.InitialPhase(nil)
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
				globals := map[string]any{
					"error":          phaseErr.Error(),
					"contents":       state.Contents(),
					"system_prompts": state.SystemPrompt(),
				}
				var openAIError generators.OpenAIError
				if errors.As(phaseErr, &openAIError) {
					globals["openai"] = openAIError
				}
				tap(ctx, "codes generate error", globals)

				// let user determine what to do
				phase = buildChat(generator)(phase)

			} else {
				// ok
				phase = newPhase
				state = newState
			}
		}

		return nil
	}
}
