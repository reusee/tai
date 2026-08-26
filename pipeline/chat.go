package pipeline

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/peterh/liner"
	"github.com/reusee/tai/debugs"
	"github.com/reusee/tai/generators"
	"github.com/reusee/tai/logs"
)

// BuildChat builds the interactive chat phase: it prompts the user for
// input, handles the chat commands (/quit, /regen, /write, /tap), and
// chains generate -> chat so each user input triggers a generation round.
type BuildChat func(generator generators.Generator, options *generators.GenerateOptions) generators.PhaseBuilder

func (Module) BuildChatPhase(
	buildGen generators.BuildGenerate,
	logger logs.Logger,
	tap debugs.Tap,
) (buildChat BuildChat) {

	getHistoryPath := sync.OnceValues(func() (string, error) {
		dir, err := os.UserConfigDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(dir, "ai-chat-history.json"), nil
	})

	buildChat = func(generator generators.Generator, options *generators.GenerateOptions) generators.PhaseBuilder {
		return func(cont generators.Phase) generators.Phase {
			return func(ctx context.Context, state generators.State) (generators.Phase, generators.State, error) {

				line := liner.NewLiner()
				defer line.Close()
				line.SetCtrlCAborts(true)
				line.SetMultiLineMode(true)

				historyPath, err := getHistoryPath()
				if err != nil {
					logger.Warn("get history path error", "err", err)
				} else {
					if f, err := os.Open(historyPath); err == nil {
						line.ReadHistory(f)
						f.Close()
					}
				}

				var input string
				for input == "" {
					input, err = line.Prompt(">> ")
					if err != nil {
						switch err {
						case io.EOF, liner.ErrPromptAborted:
							return nil, nil, nil
						}
						return nil, nil, err
					}
					input = strings.TrimSpace(input)
				}
				line.AppendHistory(input)

				if historyPath != "" {
					if err := os.MkdirAll(filepath.Dir(historyPath), 0755); err != nil {
						logger.Warn("create history dir error", "err", err)
					} else {
						if f, err := os.Create(historyPath); err != nil {
							logger.Warn("create history file error", "err", err)
						} else {
							line.WriteHistory(f)
							f.Close()
						}
					}
				}

				switch input {

				case "/quit", "/exit":
					return cont, state, nil

				case "/regen":
					checkpoint, ok := generators.As[generators.RedoCheckpoint](state)
					if !ok {
						return nil, nil, fmt.Errorf("no redo checkpoint")
					}
					return buildGen(checkpoint.Generator, options)(
						buildChat(generator, options)(
							cont,
						),
					), checkpoint.State0, nil

				case "/write":
					out, err := os.Create(".AI")
					if err != nil {
						return nil, nil, err
					}
					output := generators.NewOutput(state, out, true)
					for content := range state.Contents() {
						next, err := output.AppendContent(content)
						if err != nil {
							return nil, nil, err
						}
						output = next.(generators.Output)
					}
					_, err = output.Flush()
					if err != nil {
						return nil, nil, err
					}
					err = out.Close()
					if err != nil {
						return nil, nil, err
					}
					return buildChat(generator, options)(cont), state, nil

				case "/tap":
					var contents []*generators.Content
					for c := range state.Contents() {
						contents = append(contents, c)
					}
					funcMap := make(map[string]*generators.Function)
					for fn := range state.Functions() {
						funcMap[fn.Decl.Name] = fn
					}
					tap(ctx, "tap on chat", map[string]any{
						"generator_args": generator.Spec(),
						"contents":       contents,
						"system_prompt":  state.SystemPrompt(),
						"func_map":       funcMap,
					})
					return buildChat(generator, options)(cont), state, nil

				}

				input += "\n\n"
				state, err = state.AppendContent(&generators.Content{
					Role: generators.RoleUser,
					Parts: []generators.Part{
						generators.Text(input),
					},
				})
				if err != nil {
					return nil, nil, err
				}

				return buildGen(generator, options)(
					buildChat(generator, options)(
						cont,
					),
				), state, nil
			}
		}
	}
	return
}

const TheoryOfIdleHandler = `
The IdleHandler mechanism separates automated action processing from
interactive user input. When a generation round ends and no component
(continue, shell, go-test, ingest) triggers a new round, the
loop invokes the IdleHandler to prompt the user for input. This ensures
that automated actions are always processed before the user is prompted:
the model can chain multiple rounds of shell execution, continue block
self-prompting, or test verification without user intervention, and the
user is only prompted when the model has no pending automated actions.

The IdleHandler loops internally for commands that do not produce new
content (/write, /tap), only returning when the user provides input that
should be sent to the model (normal text), requests a regeneration
(/regen), or exits the session (/quit, /exit, EOF, Ctrl+C). This keeps
the loop's round structure clean: each round is one generation cycle,
and the IdleHandler is the sole gateway for user input between rounds.
`

// IdleHandler is called when no component triggers after a generation round.
// It provides interactive input (e.g., chat prompt) and returns whether to
// continue with another round. When continue is false, the loop ends.
// See TheoryOfIdleHandler.
type IdleHandler func(ctx context.Context, state generators.State) (generators.State, bool, error)

// BuildChatIdle builds an IdleHandler that implements the chat prompt loop.
// Unlike BuildChatPhase which chains generate -> chat as a phase, BuildChatIdle
// returns an idle handler that is invoked by the loop when no component
// triggers, ensuring automated actions (continue, shell, etc.) are processed
// before prompting the user for input. See TheoryOfIdleHandler.
type BuildChatIdle func(generator generators.Generator, options *generators.GenerateOptions) IdleHandler

func (Module) BuildChatIdle(
	logger logs.Logger,
	tap debugs.Tap,
) BuildChatIdle {
	getHistoryPath := sync.OnceValues(func() (string, error) {
		dir, err := os.UserConfigDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(dir, "ai-chat-history.json"), nil
	})

	return func(generator generators.Generator, options *generators.GenerateOptions) IdleHandler {
		return func(ctx context.Context, state generators.State) (generators.State, bool, error) {
			line := liner.NewLiner()
			defer line.Close()
			line.SetCtrlCAborts(true)
			line.SetMultiLineMode(true)

			historyPath, err := getHistoryPath()
			if err != nil {
				logger.Warn("get history path error", "err", err)
			} else {
				if f, err := os.Open(historyPath); err == nil {
					line.ReadHistory(f)
					f.Close()
				}
			}

			saveHistory := func() {
				if historyPath == "" {
					return
				}
				if err := os.MkdirAll(filepath.Dir(historyPath), 0755); err != nil {
					logger.Warn("create history dir error", "err", err)
					return
				}
				if f, err := os.Create(historyPath); err != nil {
					logger.Warn("create history file error", "err", err)
				} else {
					line.WriteHistory(f)
					f.Close()
				}
			}

			for {
				input, err := line.Prompt(">> ")
				if err != nil {
					if err == io.EOF || err == liner.ErrPromptAborted {
						saveHistory()
						return state, false, nil
					}
					return state, false, err
				}
				input = strings.TrimSpace(input)
				if input == "" {
					continue
				}
				line.AppendHistory(input)
				saveHistory()

				switch input {

				case "/quit", "/exit":
					return state, false, nil

				case "/regen":
					checkpoint, ok := generators.As[generators.RedoCheckpoint](state)
					if !ok {
						return state, false, fmt.Errorf("no redo checkpoint")
					}
					// Return the pre-generation state so the next round
					// regenerates from the checkpoint. The loop's
					// PhaseBuilder (generate only) will call the model
					// with this state.
					return checkpoint.State0, true, nil

				case "/write":
					out, err := os.Create(".AI")
					if err != nil {
						return state, false, err
					}
					output := generators.NewOutput(state, out, true)
					for content := range state.Contents() {
						next, err := output.AppendContent(content)
						if err != nil {
							out.Close()
							return state, false, err
						}
						output = next.(generators.Output)
					}
					_, err = output.Flush()
					if err != nil {
						out.Close()
						return state, false, err
					}
					err = out.Close()
					if err != nil {
						return state, false, err
					}
					// /write does not produce new content; prompt again.
					continue

				case "/tap":
					var contents []*generators.Content
					for c := range state.Contents() {
						contents = append(contents, c)
					}
					funcMap := make(map[string]*generators.Function)
					for fn := range state.Functions() {
						funcMap[fn.Decl.Name] = fn
					}
					tap(ctx, "tap on chat", map[string]any{
						"generator_args": generator.Spec(),
						"contents":       contents,
						"system_prompt":  state.SystemPrompt(),
						"func_map":       funcMap,
					})
					// /tap does not produce new content; prompt again.
					continue

				}

				// Normal input: append to state and return to trigger
				// a new generation round.
				input += "\n\n"
				state, err = state.AppendContent(&generators.Content{
					Role: generators.RoleUser,
					Parts: []generators.Part{
						generators.Text(input),
					},
				})
				if err != nil {
					return state, false, err
				}
				return state, true, nil
			}
		}
	}
}
