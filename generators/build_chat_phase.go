package generators

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
	"github.com/reusee/tai/logs"
)

type BuildChatPhase func(generator Generator, next Phase) Phase

func (Module) BuildChatPhase(
	buildGeneratePhase BuildGeneratePhase,
	logger logs.Logger,
	tap debugs.Tap,
) (buildChat BuildChatPhase) {

	getHistoryPath := sync.OnceValues(func() (string, error) {
		dir, err := os.UserConfigDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(dir, "ai-chat-history.json"), nil
	})

	buildChat = func(generator Generator, cont Phase) Phase {
		return func(ctx context.Context, state State) (Phase, State, error) {

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
				checkpoint, ok := As[RedoCheckpoint](state)
				if !ok {
					return nil, nil, fmt.Errorf("no redo checkpoint")
				}
				return buildGeneratePhase(
					checkpoint.generator,
					buildChat(generator, cont),
				), checkpoint.state0, nil

			case "/write":
				out, err := os.Create(".AI")
				if err != nil {
					return nil, nil, err
				}
				output := NewOutput(state, out, true)
				for _, content := range state.Contents() {
					next, err := output.AppendContent(content)
					if err != nil {
						return nil, nil, err
					}
					output = next.(Output)
				}
				_, err = output.Flush()
				if err != nil {
					return nil, nil, err
				}
				err = out.Close()
				if err != nil {
					return nil, nil, err
				}
				return buildChat(generator, cont), state, nil

			case "/tap":
				tap(ctx, "tap on chat", map[string]any{
					"generator_args": generator.Args(),
					"contents":       state.Contents(),
					"system_prompt":  state.SystemPrompt(),
					"func_map":       state.FuncMap(),
				})
				return buildChat(generator, cont), state, nil

			}

			input += "\n\n"
			state, err = state.AppendContent(&Content{
				Role: RoleUser,
				Parts: []Part{
					Text(input),
				},
			})
			if err != nil {
				return nil, nil, err
			}

			return buildGeneratePhase(
				generator,
				buildChat(generator, cont),
			), state, nil
		}
	}
	return
}
