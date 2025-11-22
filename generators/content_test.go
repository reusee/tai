package generators

import (
	"reflect"
	"testing"
)

func TestContentMerge(t *testing.T) {
	t.Run("different roles", func(t *testing.T) {
		c1 := Content{Role: RoleUser}
		c2 := Content{Role: RoleModel}
		merged, ok := c1.Merge(&c2)
		if ok || merged != nil {
			t.Errorf("Expected merge to fail for different roles")
		}
	})

	t.Run("same role, empty parts", func(t *testing.T) {
		c1 := Content{Role: RoleUser}
		c2 := Content{Role: RoleUser}
		merged, ok := c1.Merge(&c2)
		if !ok {
			t.Fatal("merge failed")
		}
		if len(merged.Parts) != 0 {
			t.Errorf("Expected merge to succeed with empty parts")
		}
		if merged.Role != RoleUser {
			t.Errorf("wrong role")
		}
	})

	t.Run("merge text parts", func(t *testing.T) {
		c1 := Content{Role: RoleUser, Parts: []Part{Text("hello ")}}
		c2 := Content{Role: RoleUser, Parts: []Part{Text("world")}}
		merged, ok := c1.Merge(&c2)
		if !ok {
			t.Fatal("merge failed")
		}
		if len(merged.Parts) != 1 {
			t.Fatalf("expected 1 part, got %d", len(merged.Parts))
		}
		if text, isText := merged.Parts[0].(Text); !isText || text != "hello world" {
			t.Errorf("unexpected merged text: %v", merged.Parts)
		}
	})

	t.Run("merge thought parts", func(t *testing.T) {
		c1 := Content{Role: RoleModel, Parts: []Part{Thought("thinking... ")}}
		c2 := Content{Role: RoleModel, Parts: []Part{Thought("more.")}}
		merged, ok := c1.Merge(&c2)
		if !ok {
			t.Fatal("merge failed")
		}
		if len(merged.Parts) != 1 {
			t.Fatalf("expected 1 part, got %d", len(merged.Parts))
		}
		if thought, isThought := merged.Parts[0].(Thought); !isThought || thought != "thinking... more." {
			t.Errorf("unexpected merged thought: %v", merged.Parts)
		}
	})

	t.Run("no merge for different part types", func(t *testing.T) {
		c1 := Content{Role: RoleUser, Parts: []Part{Text("hello ")}}
		c2 := Content{Role: RoleUser, Parts: []Part{Thought("world")}}
		merged, ok := c1.Merge(&c2)
		if !ok {
			t.Fatal("merge failed")
		}
		if len(merged.Parts) != 2 {
			t.Fatalf("expected 2 parts, got %d", len(merged.Parts))
		}
		if text, isText := merged.Parts[0].(Text); !isText || text != "hello " {
			t.Errorf("unexpected part 0: %v", merged.Parts)
		}
		if thought, isThought := merged.Parts[1].(Thought); !isThought || thought != "world" {
			t.Errorf("unexpected part 1: %v", merged.Parts)
		}
	})

	t.Run("complex merge", func(t *testing.T) {
		c1 := Content{
			Role:  RoleUser,
			Parts: []Part{Text("a"), Text("b"), Thought("c")},
		}
		c2 := Content{
			Role:  RoleUser,
			Parts: []Part{Thought("d"), Text("e"), Text("f")},
		}
		merged, ok := c1.Merge(&c2)
		if !ok {
			t.Fatal("merge failed")
		}
		expectedParts := []Part{
			Text("ab"),
			Thought("cd"),
			Text("ef"),
		}
		if !reflect.DeepEqual(merged.Parts, expectedParts) {
			t.Errorf("unexpected merged parts. got %+v, want %+v", merged.Parts, expectedParts)
		}
	})

	t.Run("merge with non-mergeable types", func(t *testing.T) {
		c1 := Content{
			Role:  RoleUser,
			Parts: []Part{Text("a"), FileURL("foo.bar")},
		}
		c2 := Content{
			Role:  RoleUser,
			Parts: []Part{Text("b"), Text("c")},
		}
		merged, ok := c1.Merge(&c2)
		if !ok {
			t.Fatal("merge failed")
		}
		expectedParts := []Part{
			Text("a"),
			FileURL("foo.bar"),
			Text("bc"),
		}
		if !reflect.DeepEqual(merged.Parts, expectedParts) {
			t.Errorf("unexpected merged parts. got %+v, want %+v", merged.Parts, expectedParts)
		}
	})

	t.Run("merge with empty first content", func(t *testing.T) {
		c1 := Content{
			Role:  RoleUser,
			Parts: []Part{},
		}
		c2 := Content{
			Role:  RoleUser,
			Parts: []Part{Text("a"), Text("b")},
		}
		merged, ok := c1.Merge(&c2)
		if !ok {
			t.Fatal("merge failed")
		}
		expectedParts := []Part{
			Text("ab"),
		}
		if !reflect.DeepEqual(merged.Parts, expectedParts) {
			t.Errorf("unexpected merged parts. got %+v, want %+v", merged.Parts, expectedParts)
		}
	})

	t.Run("merge with empty second content", func(t *testing.T) {
		c1 := Content{
			Role:  RoleUser,
			Parts: []Part{Text("a"), Text("b")},
		}
		c2 := Content{
			Role:  RoleUser,
			Parts: []Part{},
		}
		merged, ok := c1.Merge(&c2)
		if !ok {
			t.Fatal("merge failed")
		}
		expectedParts := []Part{
			Text("ab"),
		}
		if !reflect.DeepEqual(merged.Parts, expectedParts) {
			t.Errorf("unexpected merged parts. got %+v, want %+v", merged.Parts, expectedParts)
		}
	})

}
