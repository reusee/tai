package codes

import (
	"iter"
	"testing"

	"github.com/reusee/tai/generators"
)

// mockState is a minimal State implementation for testing BlockState.
type mockState struct {
	systemPrompt string
	contents     []*generators.Content
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

func (m *mockState) AppendContent(content *generators.Content) (generators.State, error) {
	m.contents = append(m.contents, content)
	return m, nil
}

func (m *mockState) SystemPrompt() string {
	return m.systemPrompt
}

func (m *mockState) Functions() iter.Seq[*generators.Function] {
	return func(yield func(*generators.Function) bool) {}
}

func (m *mockState) Flush() (generators.State, error) {
	return m, nil
}

func (m *mockState) Unwrap() generators.State {
	return nil
}

func TestBlockStateStreamParsing(t *testing.T) {
	upstream := &mockState{systemPrompt: "system prompt"}
	state := NewBlockState(upstream)

	// Fragment 1: prose only, no block marker
	if _, err := state.AppendContent(&generators.Content{
		Role:  generators.RoleAssistant,
		Parts: []generators.Part{generators.Text("I'll fix the issue.\n")},
	}); err != nil {
		t.Fatal(err)
	}
	if blocks := state.PopBlocks(); len(blocks) != 0 {
		t.Fatalf("expected 0 blocks before any block marker, got %d", len(blocks))
	}

	// Fragment 2: opening marker and partial body (no end marker yet)
	if _, err := state.AppendContent(&generators.Content{
		Role:  generators.RoleAssistant,
		Parts: []generators.Part{generators.Text(":::change 徕珑\n<change op=\"MODIFY\" target=\"Foo\" file-path=\"/test.go\" />\n\nfunc Foo() {}\n")},
	}); err != nil {
		t.Fatal(err)
	}
	if blocks := state.PopBlocks(); len(blocks) != 0 {
		t.Fatalf("expected 0 blocks for incomplete block, got %d", len(blocks))
	}

	// Fragment 3: end marker completes the block
	if _, err := state.AppendContent(&generators.Content{
		Role:  generators.RoleAssistant,
		Parts: []generators.Part{generators.Text(":::end 徕珑\n")},
	}); err != nil {
		t.Fatal(err)
	}
	blocks := state.PopBlocks()
	if len(blocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(blocks))
	}
	if blocks[0].Kind != "change" {
		t.Fatalf("expected kind change, got %s", blocks[0].Kind)
	}
	if blocks[0].Boundary != "徕珑" {
		t.Fatalf("expected boundary 徕珑, got %s", blocks[0].Boundary)
	}

	// PopBlocks should have cleared the buffer
	if blocks := state.PopBlocks(); len(blocks) != 0 {
		t.Fatalf("expected 0 blocks after pop, got %d", len(blocks))
	}
}

func TestBlockStateMultipleBlocks(t *testing.T) {
	upstream := &mockState{systemPrompt: "system prompt"}
	state := NewBlockState(upstream)

	text := ":::change 徕珑\n<change op=\"MODIFY\" target=\"Foo\" file-path=\"/test.go\" />\n\nfunc Foo() {}\n:::end 徕珑\n:::change 栢彣\n<change op=\"DELETE\" target=\"Bar\" file-path=\"/test.go\" />\n:::end 栢彣\n:::finish 桀骥\nDone.\n:::end 桀骥\n"
	if _, err := state.AppendContent(&generators.Content{
		Role:  generators.RoleAssistant,
		Parts: []generators.Part{generators.Text(text)},
	}); err != nil {
		t.Fatal(err)
	}

	blocks := state.PopBlocks()
	if len(blocks) != 3 {
		t.Fatalf("expected 3 blocks, got %d", len(blocks))
	}
	if blocks[0].Kind != "change" || blocks[0].Boundary != "徕珑" {
		t.Fatalf("unexpected first block: %+v", blocks[0])
	}
	if blocks[1].Kind != "change" || blocks[1].Boundary != "栢彣" {
		t.Fatalf("unexpected second block: %+v", blocks[1])
	}
	if blocks[2].Kind != "finish" || blocks[2].Boundary != "桀骥" {
		t.Fatalf("unexpected third block: %+v", blocks[2])
	}
}

func TestBlockStateUnwrapAndPassthrough(t *testing.T) {
	upstream := &mockState{systemPrompt: "system prompt"}
	state := NewBlockState(upstream)

	if state.Unwrap() != upstream {
		t.Fatal("Unwrap should return the upstream state")
	}
	if state.SystemPrompt() != "system prompt" {
		t.Fatalf("SystemPrompt should be %q, got %q", "system prompt", state.SystemPrompt())
	}
}

func TestBlockStateIgnoresUserRole(t *testing.T) {
	upstream := &mockState{systemPrompt: "system prompt"}
	state := NewBlockState(upstream)

	// User role content should not be parsed for blocks
	if _, err := state.AppendContent(&generators.Content{
		Role:  generators.RoleUser,
		Parts: []generators.Part{generators.Text(":::change 徕珑\n<change op=\"MODIFY\" target=\"Foo\" file-path=\"/test.go\" />\n\nfunc Foo() {}\n:::end 徕珑\n")},
	}); err != nil {
		t.Fatal(err)
	}

	if blocks := state.PopBlocks(); len(blocks) != 0 {
		t.Fatalf("user role content should not produce blocks, got %d", len(blocks))
	}
}

func TestBlockStatePendingText(t *testing.T) {
	upstream := &mockState{systemPrompt: "system prompt"}
	state := NewBlockState(upstream)

	// Append incomplete block
	if _, err := state.AppendContent(&generators.Content{
		Role:  generators.RoleAssistant,
		Parts: []generators.Part{generators.Text("prose before\n:::change 徕珑\nbody")},
	}); err != nil {
		t.Fatal(err)
	}

	pending := state.PendingText()
	if pending == "" {
		t.Fatal("PendingText should not be empty for incomplete block")
	}
	if !contains(pending, ":::change 徕珑") {
		t.Fatalf("PendingText should contain the opening marker: %q", pending)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsStr(s, substr))
}

func containsStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}