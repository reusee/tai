package generators

import (
	"testing"
)

func TestCountContents(t *testing.T) {
	t.Run("Empty", func(t *testing.T) {
		state := NewPrompts("", nil)
		if count := CountContents(state); count != 0 {
			t.Fatalf("expected 0 contents, got %d", count)
		}
	})

	t.Run("Multiple", func(t *testing.T) {
		state := NewPrompts("", []*Content{
			{Role: RoleUser, Parts: []Part{Text("hello")}},
			{Role: RoleAssistant, Parts: []Part{Text("hi")}},
			{Role: RoleUser, Parts: []Part{Text("bye")}},
		})
		if count := CountContents(state); count != 3 {
			t.Fatalf("expected 3 contents, got %d", count)
		}
	})
}
