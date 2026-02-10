package taido

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/reusee/tai/generators"
	"github.com/reusee/tai/logs"
	"github.com/reusee/tai/phases"
	"github.com/reusee/tai/taigo"
)

type Execute func(ctx context.Context, generator generators.Generator, state generators.State) error

func (Module) Execute(
	buildGenerate phases.BuildGenerate,
	logger logs.Logger,
) Execute {
	return func(ctx context.Context, generator generators.Generator, state generators.State) error {
		// Apply sandbox to restrict filesystem access
		if err := applySandbox(logger); err != nil {
			return fmt.Errorf("failed to apply sandbox: %w", err)
		}

		// Ensure we start with a clean state by unwrapping display/tool wrappers
		// that might have been applied by the caller. Taido wants to manage its own
		// output and tool mappings.
		for {
			if s, ok := generators.As[generators.Output](state); ok {
				state = s.Unwrap()
				continue
			}
			if s, ok := generators.As[generators.FuncMap](state); ok {
				state = s.Unwrap()
				continue
			}
			break
		}

		// Wrap state with Output to show progress to user.
		// We show thoughts (reasoning) but suppress tool calls/results in the primary
		// output to keep the dialogue clean. Mechanical progress is handled by the logger.
		state = generators.NewOutput(state, os.Stdout, true).
			WithTools(false)

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

		// EvalTaigo tool for internal Go execution
		evalTaigoFunc := &generators.Func{
			Decl: generators.FuncDecl{
				Name:        "EvalTaigo",
				Description: "Execute Go code using the internal Taigo VM. Use this for logic, data processing, or when a shell is not required.",
				Params: generators.Vars{
					{
						Name:        "code",
						Type:        generators.TypeString,
						Description: "The Go source code to execute.",
					},
				},
			},
			Func: func(args map[string]any) (map[string]any, error) {
				code, _ := args["code"].(string)
				if code == "" {
					return nil, fmt.Errorf("code is required")
				}
				var stdout, stderr bytes.Buffer
				env := new(taigo.Env)
				env.Source = code
				env.Stdout = &stdout
				env.Stderr = &stderr
				_, err := env.RunVM()
				res := map[string]any{
					"stdout": stdout.String(),
					"stderr": stderr.String(),
				}
				if err != nil {
					res["error"] = err.Error()
				}
				return res, nil
			},
		}

		// Shell tool for environment interaction
		shellFunc := &generators.Func{
			Decl: generators.FuncDecl{
				Name:        "Shell",
				Description: "Execute a shell command in /bin/sh and return the output.",
				Params: generators.Vars{
					{
						Name:        "command",
						Type:        generators.TypeString,
						Description: "The command string to execute.",
					},
				},
			},
			Func: func(args map[string]any) (map[string]any, error) {
				command, _ := args["command"].(string)
				if command == "" {
					return nil, fmt.Errorf("command is required")
				}
				cmd := exec.CommandContext(ctx, "/bin/sh", "-c", command)
				var stdout, stderr bytes.Buffer
				cmd.Stdout = &stdout
				cmd.Stderr = &stderr
				err := cmd.Run()
				exitCode := 0
				if err != nil {
					var exitErr *exec.ExitError
					if errors.As(err, &exitErr) {
						exitCode = exitErr.ExitCode()
					} else {
						return nil, err
					}
				}
				return map[string]any{
					"stdout":    stdout.String(),
					"stderr":    stderr.String(),
					"exit_code": exitCode,
				}, nil
			},
		}

		// Taido tool for delegation
		taidoFunc := &generators.Func{
			Decl: generators.FuncDecl{
				Name:        "Taido",
				Description: "Delegate an independent sub-task to another autonomous agent. The sub-agent will run in a separate process and return its results. Use this for isolation or to break down complex goals.",
				Params: generators.Vars{
					{
						Name:        "goal",
						Type:        generators.TypeString,
						Description: "The specific sub-goal for the sub-agent to achieve.",
					},
				},
			},
			Func: func(args map[string]any) (map[string]any, error) {
				goal, _ := args["goal"].(string)
				if goal == "" {
					return nil, fmt.Errorf("goal is required")
				}
				// Use current executable if possible, fallback to "tai"
				exe, _ := os.Executable()
				if exe == "" {
					exe = "tai"
				}
				cmd := exec.CommandContext(ctx, exe, "do", goal)
				var stdout, stderr bytes.Buffer
				cmd.Stdout = &stdout
				cmd.Stderr = &stderr
				err := cmd.Run()
				res := map[string]any{
					"stdout": stdout.String(),
					"stderr": stderr.String(),
				}
				if err != nil {
					res["error"] = err.Error()
				}
				return res, nil
			},
		}

		// Tool wrapper for logging progress.
		// We avoid manual ANSI hacks and instead rely on the logger for mechanical status.
		wrapFunc := func(f *generators.Func) *generators.Func {
			original := f.Func
			f.Func = func(args map[string]any) (map[string]any, error) {
				logger.Info("executing tool", "tool", f.Decl.Name)
				res, err := original(args)
				if err != nil {
					logger.Error("tool execution failed", "tool", f.Decl.Name, "error", err)
				} else {
					logger.Info("tool execution completed", "tool", f.Decl.Name)
				}
				return res, err
			}
			return f
		}

		state = generators.NewFuncMap(state, wrapFunc(stopFunc), wrapFunc(shellFunc), wrapFunc(evalTaigoFunc), wrapFunc(taidoFunc))

		for i := 0; ; i++ {
			// 1. Generation Phase
			// This handles tool execution internally via FuncMap state wrapper
			phase := buildGenerate(generator, nil)(nil)
			_, newState, err := phase(ctx, state)
			if err != nil {
				return fmt.Errorf("generation failed at iteration %d: %w", i, err)
			}
			state = newState

			// Flush output to ensure the user sees current progress
			if s, err := state.Flush(); err == nil {
				state = s
			}

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

				// If the FuncMap wrapper executed tools, the new last message will be RoleTool.
				// We already checked this at the top of the continuation logic, but since
				// state was updated via newState (which includes tool results), we need to
				// re-check if we should continue.
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