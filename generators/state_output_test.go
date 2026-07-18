package generators

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestOutput(t *testing.T) {
	t.Run("basic text", func(t *testing.T) {
		buf := new(bytes.Buffer)
		upstream := NewPrompts("system prompt", nil)
		output := NewOutput(upstream, buf, true)
		state := State(output)

		var err error
		state, err = state.AppendContent(&Content{
			Role: RoleUser,
			Parts: []Part{
				Text("hello"),
			},
		})
		if err != nil {
			t.Fatal(err)
		}

		if buf.String() != "hello" {
			t.Fatalf("got %q", buf.String())
		}
	})

	t.Run("role separation", func(t *testing.T) {
		buf := new(bytes.Buffer)
		upstream := NewPrompts("", nil)
		output := NewOutput(upstream, buf, true)
		state := State(output)

		var err error
		state, err = state.AppendContent(&Content{
			Role:  RoleUser,
			Parts: []Part{Text("user msg")},
		})
		if err != nil {
			t.Fatal(err)
		}
		state, err = state.AppendContent(&Content{
			Role:  RoleModel,
			Parts: []Part{Text("model msg")},
		})
		if err != nil {
			t.Fatal(err)
		}

		got := buf.String()
		if !strings.Contains(got, "user msg\n\nmodel msg") {
			t.Fatalf("got %q", got)
		}
	})

	t.Run("thoughts visibility and tags", func(t *testing.T) {
		t.Run("show thoughts and wrap with tags", func(t *testing.T) {
			buf := new(bytes.Buffer)
			output := NewOutput(NewPrompts("", nil), buf, true)
			_, err := output.AppendContent(&Content{
				Role: RoleModel,
				Parts: []Part{
					Thought("thinking"),
					Text("answer"),
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			got := buf.String()
			expected := "<think>\nthinking\n</think>\nanswer"
			if got != expected {
				t.Fatalf("got %q, want %q", got, expected)
			}
		})

		t.Run("hide thoughts (global)", func(t *testing.T) {
			buf := new(bytes.Buffer)
			output := NewOutput(NewPrompts("", nil), buf, false)
			_, err := output.AppendContent(&Content{
				Role: RoleModel,
				Parts: []Part{
					Thought("thinking"),
					Text("answer"),
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			got := buf.String()
			if strings.Contains(got, "thinking") || strings.Contains(got, "<think>") {
				t.Fatalf("got %q", got)
			}
			if got != "answer" {
				t.Fatalf("got %q", got)
			}
		})

		t.Run("disable thoughts (WithThoughts)", func(t *testing.T) {
			buf := new(bytes.Buffer)
			output := NewOutput(NewPrompts("", nil), buf, true).WithThoughts(false)
			_, err := output.AppendContent(&Content{
				Role: RoleModel,
				Parts: []Part{
					Thought("thinking"),
					Text("answer"),
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			got := buf.String()
			if strings.Contains(got, "thinking") {
				t.Fatalf("got %q", got)
			}
		})
	})

	t.Run("cross-content thought tags", func(t *testing.T) {
		buf := new(bytes.Buffer)
		output := NewOutput(NewPrompts("", nil), buf, true)
		state := State(output)

		var err error
		// 1. Content with only thought
		state, err = state.AppendContent(&Content{
			Role: RoleModel,
			Parts: []Part{
				Thought("thinking deep"),
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(buf.String(), "<think>\nthinking deep") {
			t.Fatal("should open tag")
		}
		if strings.Contains(buf.String(), "</think>") {
			t.Fatal("should not close tag yet")
		}

		// 2. Subsequent content with text should close the tag
		state, err = state.AppendContent(&Content{
			Role: RoleModel,
			Parts: []Part{
				Text("final answer"),
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(buf.String(), "\n</think>\nfinal answer") {
			t.Fatalf("tag not closed correctly: %q", buf.String())
		}
	})

	t.Run("terminal colors", func(t *testing.T) {
		buf := new(bytes.Buffer)
		// Manually construct to set private isTerminal
		output := Output{
			upstream:   NewPrompts("", nil),
			w:          buf,
			isTerminal: true,
		}
		_, err := output.AppendContent(&Content{
			Role: RoleUser,
			Parts: []Part{
				Text("hello"),
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		got := buf.String()
		if !strings.Contains(got, ColorUser) {
			t.Fatalf("expected color %q, got %q", ColorUser, got)
		}
		if !strings.Contains(got, ColorReset) {
			t.Fatal("expected reset color")
		}
	})

	t.Run("tool and other parts", func(t *testing.T) {
		buf := new(bytes.Buffer)
		output := NewOutput(NewPrompts("", nil), buf, true)
		state := State(output)

		parts := []Part{
			FileURL("http://example.com/img.png"),
			FileContent{MimeType: "image/png"},
			FuncCall{Name: "myFunc", Arguments: map[string]any{"a": 1}},
			CallResult{Name: "myFunc", Results: map[string]any{"res": "ok"}},
			FinishReason("stop"),
			Error{Error: errors.New("fail")},
		}

		for _, p := range parts {
			var err error
			state, err = state.AppendContent(&Content{
				Role:  RoleLog,
				Parts: []Part{p},
			})
			if err != nil {
				t.Fatal(err)
			}
		}

		got := buf.String()
		expected := []string{
			"[File: http://example.com/img.png]",
			"[File Content: image/png]",
			"[Function Call: myFunc(map[a:1])]",
			"[Call Result: myFunc(map[res:ok])]",
			"[Finish: stop]",
			"[Error: fail]",
		}
		for _, e := range expected {
			if !strings.Contains(got, e) {
				t.Errorf("expected to contain %q, got %q", e, got)
			}
		}
	})

	t.Run("flush", func(t *testing.T) {
		buf := new(bytes.Buffer)
		output := NewOutput(NewPrompts("", nil), buf, true)
		_, err := output.Flush()
		if err != nil {
			t.Fatal(err)
		}
		if buf.String() != "\n\n" {
			t.Fatalf("got %q", buf.String())
		}
	})
}

func TestOutputUsage(t *testing.T) {
	t.Run("non-cumulative usage", func(t *testing.T) {
		buf := new(bytes.Buffer)
		output := NewOutput(NewPrompts("", nil), buf, true)
		state := State(output)

		// first call
		usage1 := Usage{}
		usage1.Prompt.TokenCount = 10
		state, _ = state.AppendContent(&Content{
			Role:  RoleLog,
			Parts: []Part{usage1},
		})
		state, _ = state.AppendContent(&Content{
			Role:  RoleLog,
			Parts: []Part{FinishReason("stop")},
		})
		if !strings.Contains(buf.String(), "prompt=10") {
			t.Fatal()
		}
		buf.Reset()
		state, _ = state.Flush()

		// second call
		usage2 := Usage{}
		usage2.Prompt.TokenCount = 20
		state, _ = state.AppendContent(&Content{
			Role:  RoleLog,
			Parts: []Part{usage2},
		})
		state, _ = state.AppendContent(&Content{
			Role:  RoleLog,
			Parts: []Part{FinishReason("stop")},
		})
		got := buf.String()
		if !strings.Contains(got, "prompt=20") {
			t.Fatalf("got %q", got)
		}
		if strings.Contains(got, "prompt=30") {
			t.Fatal("usage should not be cumulative")
		}
	})
}

func TestWithFunctions(t *testing.T) {
	fn1 := &Function{Decl: FuncDecl{Name: "foo"}}
	fn2 := &Function{Decl: FuncDecl{Name: "bar"}}

	base := NewPrompts("hello", nil)
	s := WithFunctions(base, fn1, fn2)

	if s.SystemPrompt() != "hello" {
		t.Fatal("bad system prompt")
	}

	// functions should be visible, globally sorted by name
	var names []string
	for fn := range s.Functions() {
		names = append(names, fn.Decl.Name)
	}
	if len(names) != 2 || names[0] != "bar" || names[1] != "foo" {
		t.Fatalf("unexpected functions: %v", names)
	}

	// AppendContent should work and preserve functions
	s2, err := s.AppendContent(&Content{
		Role:  RoleUser,
		Parts: []Part{Text("hi")},
	})
	if err != nil {
		t.Fatal(err)
	}

	names = nil
	for fn := range s2.Functions() {
		names = append(names, fn.Decl.Name)
	}
	if len(names) != 2 {
		t.Fatalf("functions lost after AppendContent: %v", names)
	}

	// contents should be propagated
	var contents []*Content
	for c := range s2.Contents() {
		contents = append(contents, c)
	}
	if len(contents) != 1 {
		t.Fatalf("expected 1 content, got %d", len(contents))
	}

	// Unwrap should return the upstream
	u := s2.Unwrap()
	if u == nil {
		t.Fatal("Unwrap returned nil")
	}
	if _, ok := u.(Prompts); !ok {
		t.Fatalf("expected Unwrap to return Prompts, got %T", u)
	}

	// Flush should propagate and preserve functions
	s3, err := s.Flush()
	if err != nil {
		t.Fatal(err)
	}
	names = nil
	for fn := range s3.Functions() {
		names = append(names, fn.Decl.Name)
	}
	if len(names) != 2 {
		t.Fatalf("functions lost after Flush: %v", names)
	}
}

func TestOutputThoughtColor(t *testing.T) {
	buf := new(bytes.Buffer)
	output := Output{
		upstream:     NewPrompts("", nil),
		w:            buf,
		isTerminal:   true,
		showThoughts: true,
	}
	_, err := output.AppendContent(&Content{
		Role: RoleModel,
		Parts: []Part{
			Thought("deep reasoning"),
			Text("answer"),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	got := buf.String()
	// Thoughts must use ColorThought, not the role color (ColorReset for model)
	if !strings.Contains(got, ColorThought) {
		t.Fatalf("expected thought color %q in output, got %q", ColorThought, got)
	}
	// ColorThought must differ from ColorLog (previously both were red)
	if ColorThought == ColorLog {
		t.Fatal("ColorThought must be distinct from ColorLog")
	}
}
