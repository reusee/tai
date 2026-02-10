package taido

import (
	"context"
	"fmt"
	"strings"

	"github.com/reusee/tai/generators"
	"github.com/reusee/tai/logs"
	"github.com/reusee/tai/phases"
)

type Execute func(ctx context.Context, generator generators.Generator, state generators.State) error

func (Module) Execute(
	buildGenerate phases.BuildGenerate,
	logger logs.Logger,
) Execute {
	return func(ctx context.Context, generator generators.Generator, state generators.State) error {
		// Internal Stop tool to signal completion
		stopped := false
		stopFunc := &generators.Func{
			Decl: generators.FuncDecl{
				Name:        "Stop",
				Description: "Signal that the goal has been achieved and terminate execution.",
				Params: generators.Vars{
					{
						Name:        "reason",
						Type:        generators.TypeString,
						Description: "A brief summary of what was achieved.",
					},
				},
			},
			Func: func(args map[string]any) (map[string]any, error) {
				stopped = true
				reason, _ := args["reason"].(string)
				logger.Info("autonomous execution completed: Stop tool called", "reason", reason)
				return map[string]any{"status": "stopped", "reason": reason}, nil
			},
		}
		state = generators.NewFuncMap(state, stopFunc)

		for i := 0; ; i++ {
			// 1. Generation Phase
			// This handles tool execution internally via FuncMap state wrapper
			phase := buildGenerate(generator, nil)(nil)
			_, newState, err := phase(ctx, state)
			if err != nil {
				return fmt.Errorf("generation failed at iteration %d: %w", i, err)
			}
			state = newState

			// Check for Stop tool call
			if stopped {
				return nil
			}

			// 2. Analyze state for continuation
			contents := state.Contents()
			if len(contents) == 0 {
				break
			}
			last := contents[len(contents)-1]

			// If the last message is a tool result, we MUST continue to let the model see it.
			if last.Role == generators.RoleTool {
				continue
			}

			// If the last message is from the model/assistant
			if last.Role == generators.RoleModel || last.Role == generators.RoleAssistant {
				hasToolCall := false
				var textBuilder strings.Builder
				for _, part := range last.Parts {
					switch p := part.(type) {
					case generators.FuncCall:
						hasToolCall = true
					case generators.Text:
						textBuilder.WriteString(string(p))
					}
				}

				// Check for completion signal in text as a fallback
				if strings.Contains(textBuilder.String(), "Goal achieved.") {
					logger.Info("autonomous execution completed: goal achieved text signal")
					return nil
				}

				// If model called tools, the FuncMap wrapper already executed them and
				// appended the result to state (if correctly configured).
				// We check if the NEW last content is now a tool result.
				contents = state.Contents()
				if contents[len(contents)-1].Role == generators.RoleTool {
					continue
				}

				// If there are no tool calls and no completion signal, we stop to avoid
				// idling or infinite repetition.
				if !hasToolCall {
					logger.Info("autonomous execution stopped: no actions or completion signal")
					return nil
				}
			}
		}
		return nil
	}
}