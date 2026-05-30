package taido

import (
	"context"
	"fmt"
	"io"
	"iter"
	"log/slog"
	"strings"
	"testing"

	"github.com/reusee/tai/generators"
	"github.com/reusee/tai/phases"
)

type mockGenerator struct {
	responses []generators.Part
}

func (m *mockGenerator) Spec() generators.Spec           { return generators.Spec{} }
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

func (m *mockState) Contents() iter.Seq[*generators.Content] {
	return func(yield func(*generators.Content) bool) {
		for _, c := range m.contents {
			if !yield(c) {
				return
			}
		}
	}
}

func (m *mockState) AppendContent(c *generators.Content) (generators.State, error) {
	return &mockState{contents: append(m.contents, c)}, nil
}

func (m *mockState) SystemPrompt() string { return "" }

func (m *mockState) Functions() iter.Seq2[string, *generators.Function] {
	return func(yield func(string, *generators.Function) bool) {}
}

func (m *mockState) Flush() (generators.State, error) { return m, nil }

func (m *mockState) Unwrap() generators.State { return nil }

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
				generators.FuncCall{Name: "test_tool", Arguments: map[string]any{"v": 1}},
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
					// Simulate internal FuncMap handling for the test
					var contents []*generators.Content
					for c := range newState.Contents() {
						contents = append(contents, c)
					}
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
				generators.FuncCall{Name: "Stop", Arguments: map[string]any{"reason": "done"}},
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
					var contents []*generators.Content
					for c := range newState.Contents() {
						contents = append(contents, c)
					}
					last := contents[len(contents)-1]
					for _, part := range last.Parts {
						if call, ok := part.(generators.FuncCall); ok {
							var fn *generators.Function
							for k, v := range state.Functions() {
								if k == call.Name {
									fn = v
									break
								}
							}
							if fn != nil {
								res, err := fn.Func(call.Arguments)
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

	t.Run("Error tool failure", func(t *testing.T) {
		gen := &mockGenerator{
			responses: []generators.Part{
				generators.FuncCall{Name: "Error", Arguments: map[string]any{"reason": "environment incompatible"}},
			},
		}

		buildGenerate := func(generator generators.Generator, options *generators.GenerateOptions) phases.PhaseBuilder {
			return func(cont phases.Phase) phases.Phase {
				return func(ctx context.Context, state generators.State) (phases.Phase, generators.State, error) {
					newState, err := generator.Generate(ctx, state, options)
					if err != nil {
						return nil, nil, err
					}
					var contents []*generators.Content
					for c := range newState.Contents() {
						contents = append(contents, c)
					}
					last := contents[len(contents)-1]
					for _, part := range last.Parts {
						if call, ok := part.(generators.FuncCall); ok {
							var fn *generators.Function
							for k, v := range state.Functions() {
								if k == call.Name {
									fn = v
									break
								}
							}
							if fn != nil {
								res, err := fn.Func(call.Arguments)
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
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "environment incompatible") {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("Shell tool success", func(t *testing.T) {
		gen := &mockGenerator{
			responses: []generators.Part{
				generators.FuncCall{Name: "Shell", Arguments: map[string]any{"command": "echo hello"}},
				generators.FuncCall{Name: "Stop", Arguments: map[string]any{"reason": "done"}},
			},
		}

		buildGenerate := func(generator generators.Generator, options *generators.GenerateOptions) phases.PhaseBuilder {
			return func(cont phases.Phase) phases.Phase {
				return func(ctx context.Context, state generators.State) (phases.Phase, generators.State, error) {
					newState, err := generator.Generate(ctx, state, options)
					if err != nil {
						return nil, nil, err
					}
					var contents []*generators.Content
					for c := range newState.Contents() {
						contents = append(contents, c)
					}
					last := contents[len(contents)-1]
					for _, part := range last.Parts {
						if call, ok := part.(generators.FuncCall); ok {
							var fn *generators.Function
							for k, v := range state.Functions() {
								if k == call.Name {
									fn = v
									break
								}
							}
							if fn != nil {
								res, err := fn.Func(call.Arguments)
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

