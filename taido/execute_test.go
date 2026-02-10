package taido

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/reusee/tai/generators"
	"github.com/reusee/tai/phases"
)

type mockGenerator struct {
	responses []generators.Part
}

func (m *mockGenerator) Args() generators.GeneratorArgs  { return generators.GeneratorArgs{} }
func (m *mockGenerator) CountTokens(string) (int, error) { return 0, nil }
func (m *mockGenerator) Generate(ctx context.Context, state generators.State, options *generators.GenerateOptions) (generators.State, error) {
	if len(m.responses) == 0 {
		return nil, fmt.Errorf("no more responses")
	}
	resp := m.responses[0]
	m.responses = m.responses[1:]
	return state.AppendContent(&generators.Content{
		Role:  generators.RoleAssistant,
		Parts: []generators.Part{resp},
	})
}

type mockState struct {
	generators.State
	contents []*generators.Content
}

func (m *mockState) Contents() []*generators.Content {
	return m.contents
}

func (m *mockState) AppendContent(c *generators.Content) (generators.State, error) {
	return &mockState{contents: append(m.contents, c)}, nil
}

func (m *mockState) SystemPrompt() string { return "" }

func (m *mockState) FuncMap() map[string]*generators.Func { return nil }

func (m *mockState) Flush() (generators.State, error) { return m, nil }

func (m *mockState) Unwrap() generators.State { return m }

func TestExecute(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	t.Run("direct success", func(t *testing.T) {
		gen := &mockGenerator{
			responses: []generators.Part{
				generators.Text("Task done. Goal achieved."),
			},
		}
		buildGenerate := func(generator generators.Generator, options *generators.GenerateOptions) phases.PhaseBuilder {
			return func(cont phases.Phase) phases.Phase {
				return func(ctx context.Context, state generators.State) (phases.Phase, generators.State, error) {
					newState, err := generator.Generate(ctx, state, options)
					return nil, newState, err
				}
			}
		}
		exec := (Module{}).Execute(buildGenerate, logger)
		state := &mockState{}
		err := exec(context.Background(), gen, state)
		if err != nil {
			t.Fatal(err)
		}
	})

	t.Run("tool call success", func(t *testing.T) {
		gen := &mockGenerator{
			responses: []generators.Part{
				generators.FuncCall{Name: "test_tool", Args: map[string]any{"v": 1}},
				generators.Text("Result received. Goal achieved."),
			},
		}

		buildGenerate := func(generator generators.Generator, options *generators.GenerateOptions) phases.PhaseBuilder {
			return func(cont phases.Phase) phases.Phase {
				return func(ctx context.Context, state generators.State) (phases.Phase, generators.State, error) {
					newState, err := generator.Generate(ctx, state, options)
					if err != nil {
						return nil, nil, err
					}
					contents := newState.Contents()
					last := contents[len(contents)-1]
					hasCall := false
					for _, part := range last.Parts {
						if _, ok := part.(generators.FuncCall); ok {
							hasCall = true
							break
						}
					}
					if hasCall {
						newState, err = newState.AppendContent(&generators.Content{
							Role: generators.RoleTool,
							Parts: []generators.Part{
								generators.CallResult{
									Name:    "test_tool",
									Results: map[string]any{"res": "ok"},
								},
							},
						})
						if err != nil {
							return nil, nil, err
						}
					}
					return nil, newState, nil
				}
			}
		}

		exec := (Module{}).Execute(buildGenerate, logger)
		state := &mockState{}
		err := exec(context.Background(), gen, state)
		if err != nil {
			t.Fatal(err)
		}
	})

	t.Run("Stop tool success", func(t *testing.T) {
		gen := &mockGenerator{
			responses: []generators.Part{
				generators.FuncCall{Name: "Stop"},
			},
		}

		buildGenerate := func(generator generators.Generator, options *generators.GenerateOptions) phases.PhaseBuilder {
			return func(cont phases.Phase) phases.Phase {
				return func(ctx context.Context, state generators.State) (phases.Phase, generators.State, error) {
					newState, err := generator.Generate(ctx, state, options)
					if err != nil {
						return nil, nil, err
					}
					// Simulate FuncMap execution
					contents := newState.Contents()
					last := contents[len(contents)-1]
					for _, part := range last.Parts {
						if call, ok := part.(generators.FuncCall); ok {
							if fn, ok := state.FuncMap()[call.Name]; ok {
								res, err := fn.Func(call.Args)
								if err != nil {
									return nil, nil, err
								}
								newState, err = newState.AppendContent(&generators.Content{
									Role: generators.RoleTool,
									Parts: []generators.Part{
										generators.CallResult{
											Name:    call.Name,
											Results: res,
										},
									},
								})
								if err != nil {
									return nil, nil, err
								}
							}
						}
					}
					return nil, newState, nil
				}
			}
		}

		exec := (Module{}).Execute(buildGenerate, logger)
		state := &mockState{}
		err := exec(context.Background(), gen, state)
		if err != nil {
			t.Fatal(err)
		}
	})

	t.Run("max iterations", func(t *testing.T) {
		gen := &mockGenerator{}
		for i := 0; i < 100; i++ {
			gen.responses = append(gen.responses, generators.FuncCall{Name: "loop"})
		}

		buildGenerate := func(generator generators.Generator, options *generators.GenerateOptions) phases.PhaseBuilder {
			return func(cont phases.Phase) phases.Phase {
				return func(ctx context.Context, state generators.State) (phases.Phase, generators.State, error) {
					newState, err := generator.Generate(ctx, state, options)
					if err != nil {
						return nil, nil, err
					}
					newState, err = newState.AppendContent(&generators.Content{
						Role:  generators.RoleTool,
						Parts: []generators.Part{generators.CallResult{Name: "loop"}},
					})
					return nil, newState, err
				}
			}
		}

		exec := (Module{}).Execute(buildGenerate, logger)
		state := &mockState{}
		err := exec(context.Background(), gen, state)
		if err == nil || !strings.Contains(err.Error(), "exceeded maximum iterations") {
			t.Fatalf("expected max iterations error, got %v", err)
		}
	})

	t.Run("no action no signal", func(t *testing.T) {
		gen := &mockGenerator{
			responses: []generators.Part{
				generators.Text("Just chatting, no goal achieved yet and no tools."),
			},
		}
		buildGenerate := func(generator generators.Generator, options *generators.GenerateOptions) phases.PhaseBuilder {
			return func(cont phases.Phase) phases.Phase {
				return func(ctx context.Context, state generators.State) (phases.Phase, generators.State, error) {
					newState, err := generator.Generate(ctx, state, options)
					return nil, newState, err
				}
			}
		}
		exec := (Module{}).Execute(buildGenerate, logger)
		state := &mockState{}
		err := exec(context.Background(), gen, state)
		if err != nil {
			t.Fatal(err)
		}
	})
}

func TestSystemPrompt(t *testing.T) {
	p := (Module{}).SystemPrompt()
	if !strings.Contains(string(p), "autonomous execution agent") {
		t.Errorf("unexpected prompt: %s", p)
	}
}