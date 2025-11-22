package generators

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"testing"
)

type mockState struct {
	contents     []*Content
	systemPrompt string
	funcMap      map[string]*Func
	unwrapped    State
	flushErr     error
	appendErr    error
}

func (m *mockState) Contents() []*Content { return m.contents }
func (m *mockState) AppendContent(c *Content) (State, error) {
	if m.appendErr != nil {
		return nil, m.appendErr
	}
	newContents := append(slices.Clone(m.contents), c)
	return &mockState{
		contents:     newContents,
		systemPrompt: m.systemPrompt,
		funcMap:      m.funcMap,
		unwrapped:    m.unwrapped,
		flushErr:     m.flushErr,
		appendErr:    m.appendErr,
	}, nil
}
func (m *mockState) SystemPrompt() string      { return m.systemPrompt }
func (m *mockState) FuncMap() map[string]*Func { return m.funcMap }
func (m *mockState) Flush() (State, error) {
	if m.flushErr != nil {
		return nil, m.flushErr
	}
	return m, nil
}
func (m *mockState) Unwrap() State { return m.unwrapped }

type mockGenerator struct{}

func (m *mockGenerator) Args() GeneratorArgs                                      { return GeneratorArgs{} }
func (m *mockGenerator) CountTokens(string) (int, error)                          { return 0, nil }
func (m *mockGenerator) Generate(ctx context.Context, state State) (State, error) { return state, nil }

func TestRedoCheckpoint(t *testing.T) {
	upstream := &mockState{
		contents:     []*Content{{Role: "user"}},
		systemPrompt: "system",
		funcMap:      map[string]*Func{"foo": {}},
		unwrapped:    nil,
	}
	state0 := &mockState{
		contents: []*Content{{Role: "user", Parts: []Part{Text("state0")}}},
	}
	generator := &mockGenerator{}

	checkpoint := RedoCheckpoint{
		upstream:  upstream,
		state0:    state0,
		generator: generator,
	}

	t.Run("Contents", func(t *testing.T) {
		if !reflect.DeepEqual(checkpoint.Contents(), upstream.Contents()) {
			t.Errorf("Contents() did not delegate to upstream")
		}
	})

	t.Run("SystemPrompt", func(t *testing.T) {
		if checkpoint.SystemPrompt() != upstream.SystemPrompt() {
			t.Errorf("SystemPrompt() did not delegate to upstream")
		}
	})

	t.Run("FuncMap", func(t *testing.T) {
		if !reflect.DeepEqual(checkpoint.FuncMap(), upstream.FuncMap()) {
			t.Errorf("FuncMap() did not delegate to upstream")
		}
	})

	t.Run("Unwrap", func(t *testing.T) {
		if checkpoint.Unwrap() != upstream {
			t.Errorf("Unwrap() did not return upstream")
		}
	})

	t.Run("AppendContent", func(t *testing.T) {
		newContent := &Content{Role: "model"}
		newState, err := checkpoint.AppendContent(newContent)
		if err != nil {
			t.Fatalf("AppendContent() returned an error: %v", err)
		}

		newCheckpoint, ok := newState.(RedoCheckpoint)
		if !ok {
			t.Fatalf("AppendContent() did not return a RedoCheckpoint")
		}

		expectedContents := append(upstream.Contents(), newContent)
		if !reflect.DeepEqual(newCheckpoint.Contents(), expectedContents) {
			t.Errorf("new checkpoint has wrong contents")
		}

		if newCheckpoint.state0 != state0 {
			t.Errorf("state0 was not preserved")
		}
		if newCheckpoint.generator != generator {
			t.Errorf("generator was not preserved")
		}
	})

	t.Run("AppendContent error", func(t *testing.T) {
		testErr := errors.New("append error")
		upstreamWithErr := &mockState{appendErr: testErr}
		checkpointWithErr := RedoCheckpoint{upstream: upstreamWithErr}
		_, err := checkpointWithErr.AppendContent(&Content{})
		if !errors.Is(err, testErr) {
			t.Errorf("expected error %v, got %v", testErr, err)
		}
	})

	t.Run("Flush", func(t *testing.T) {
		newState, err := checkpoint.Flush()
		if err != nil {
			t.Fatalf("Flush() returned an error: %v", err)
		}

		newCheckpoint, ok := newState.(RedoCheckpoint)
		if !ok {
			t.Fatalf("Flush() did not return a RedoCheckpoint")
		}

		if newCheckpoint.upstream != upstream {
			t.Errorf("new checkpoint has wrong upstream")
		}

		if newCheckpoint.state0 != state0 {
			t.Errorf("state0 was not preserved")
		}
		if newCheckpoint.generator != generator {
			t.Errorf("generator was not preserved")
		}
	})

	t.Run("Flush error", func(t *testing.T) {
		testErr := errors.New("flush error")
		upstreamWithErr := &mockState{flushErr: testErr}
		checkpointWithErr := RedoCheckpoint{upstream: upstreamWithErr}
		_, err := checkpointWithErr.Flush()
		if !errors.Is(err, testErr) {
			t.Errorf("expected error %v, got %v", testErr, err)
		}
	})

}
