package generators

import (
	"context"
	"errors"
	"iter"
	"reflect"
	"slices"
	"testing"
)

type checkpointMockState struct {
	contents     []*Content
	systemPrompt string
	funcMap      map[string]*Function
	unwrapped    State
	flushErr     error
	appendErr    error
}

func (m *checkpointMockState) Contents() iter.Seq[*Content] {
	return func(yield func(*Content) bool) {
		for _, c := range m.contents {
			if !yield(c) {
				return
			}
		}
	}
}
func (m *checkpointMockState) AppendContent(c *Content) (State, error) {
	if m.appendErr != nil {
		return nil, m.appendErr
	}
	newContents := append(slices.Clone(m.contents), c)
	return &checkpointMockState{
		contents:     newContents,
		systemPrompt: m.systemPrompt,
		funcMap:      m.funcMap,
		unwrapped:    m.unwrapped,
		flushErr:     m.flushErr,
		appendErr:    m.appendErr,
	}, nil
}
func (m *checkpointMockState) SystemPrompt() string { return m.systemPrompt }
func (m *checkpointMockState) Functions() iter.Seq[*Function] {
	return func(yield func(*Function) bool) {
		for _, v := range m.funcMap {
			if !yield(v) {
				return
			}
		}
	}
}
func (m *checkpointMockState) Flush() (State, error) {
	if m.flushErr != nil {
		return nil, m.flushErr
	}
	return m, nil
}
func (m *checkpointMockState) Unwrap() State { return m.unwrapped }

type checkpointMockGenerator struct{}

func (m *checkpointMockGenerator) Spec() Spec { return Spec{} }
func (m *checkpointMockGenerator) CountTokens(string) (int, error) {
	return 0, nil
}
func (m *checkpointMockGenerator) Generate(ctx context.Context, state State, options *GenerateOptions) (State, error) {
	return state, nil
}

func TestRedoCheckpoint(t *testing.T) {
	upstream := &checkpointMockState{
		contents:     []*Content{{Role: "user"}},
		systemPrompt: "system",
		funcMap:      map[string]*Function{"foo": {}},
		unwrapped:    nil,
	}
	state0 := &checkpointMockState{
		contents: []*Content{{Role: "user", Parts: []Part{Text("state0")}}},
	}
	generator := &checkpointMockGenerator{}

	checkpoint := RedoCheckpoint{
		upstream:  upstream,
		State0:    state0,
		Generator: generator,
	}

	t.Run("Contents", func(t *testing.T) {
		var got, want []*Content
		for c := range checkpoint.Contents() {
			got = append(got, c)
		}
		for c := range upstream.Contents() {
			want = append(want, c)
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("Contents() did not delegate to upstream")
		}
	})

	t.Run("SystemPrompt", func(t *testing.T) {
		if checkpoint.SystemPrompt() != upstream.SystemPrompt() {
			t.Errorf("SystemPrompt() did not delegate to upstream")
		}
	})

	t.Run("FuncMap", func(t *testing.T) {
		got := make(map[string]*Function)
		for fn := range checkpoint.Functions() {
			got[fn.Decl.Name] = fn
		}
		want := make(map[string]*Function)
		for fn := range upstream.Functions() {
			want[fn.Decl.Name] = fn
		}
		if !reflect.DeepEqual(got, want) {
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

		var expectedContents []*Content
		for c := range upstream.Contents() {
			expectedContents = append(expectedContents, c)
		}
		expectedContents = append(expectedContents, newContent)
		var gotContents []*Content
		for c := range newCheckpoint.Contents() {
			gotContents = append(gotContents, c)
		}
		if !reflect.DeepEqual(gotContents, expectedContents) {
			t.Errorf("new checkpoint has wrong contents")
		}

		if newCheckpoint.State0 != state0 {
			t.Errorf("State0 was not preserved")
		}
		if newCheckpoint.Generator != generator {
			t.Errorf("Generator was not preserved")
		}
	})

	t.Run("AppendContent error", func(t *testing.T) {
		testErr := errors.New("append error")
		upstreamWithErr := &checkpointMockState{appendErr: testErr}
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

		if newCheckpoint.State0 != state0 {
			t.Errorf("State0 was not preserved")
		}
		if newCheckpoint.Generator != generator {
			t.Errorf("Generator was not preserved")
		}
	})

	t.Run("Flush error", func(t *testing.T) {
		testErr := errors.New("flush error")
		upstreamWithErr := &checkpointMockState{flushErr: testErr}
		checkpointWithErr := RedoCheckpoint{upstream: upstreamWithErr}
		_, err := checkpointWithErr.Flush()
		if !errors.Is(err, testErr) {
			t.Errorf("expected error %v, got %v", testErr, err)
		}
	})

}
