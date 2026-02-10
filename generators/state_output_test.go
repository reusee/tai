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
			FuncCall{Name: "myFunc", Args: map[string]any{"a": 1}},
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