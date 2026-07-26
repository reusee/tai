package generators

import (
	"testing"
)

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
