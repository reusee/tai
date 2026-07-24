package flags

import (
	"testing"

	"github.com/reusee/dscope"
)

// nilFlagModule is a test-only module providing a flag whose Handle returns
// nil without error, used to verify Parse's defensive nil check.
type nilFlagModule struct {
	dscope.Module
}

// NilFlag is a test-only Flag whose Handle returns nil without error.
type NilFlag string

var _ Flag = NilFlag("")

func (NilFlag) Keys() map[string]string {
	return map[string]string{"nilflag": "test flag that returns nil"}
}

func (NilFlag) Handle(key string, args []string) (newValue any, remainArgs []string, err error) {
	return nil, args, nil
}

func (nilFlagModule) NilFlag() NilFlag {
	return NilFlag("")
}

func TestParseEmptyArgs(t *testing.T) {
	scope := dscope.New(Module{})
	result, err := Parse(scope, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := dscope.Get[Chats](result)
	if len(got) != 0 {
		t.Fatalf("expected empty chats, got %v", got)
	}
}

func TestParseSingleChat(t *testing.T) {
	scope := dscope.New(Module{})
	result, err := Parse(scope, []string{"chat", "hello"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := dscope.Get[Chats](result)
	if len(got) != 1 || got[0] != "hello" {
		t.Fatalf("expected [hello], got %v", got)
	}
}

// TestParseMultipleChatsAccumulate is the reproduction test for the stale
// flagMap bug: repeated chat flags must accumulate, not overwrite.
func TestParseMultipleChatsAccumulate(t *testing.T) {
	scope := dscope.New(Module{})
	result, err := Parse(scope, []string{"chat", "a", "chat", "b"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := dscope.Get[Chats](result)
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("expected [a b], got %v", got)
	}
}

func TestParseUnknownFlag(t *testing.T) {
	scope := dscope.New(Module{})
	_, err := Parse(scope, []string{"unknown", "value"})
	if err == nil {
		t.Fatal("expected error for unknown flag, got nil")
	}
}

func TestParseChatHandleError(t *testing.T) {
	scope := dscope.New(Module{})
	_, err := Parse(scope, []string{"chat"})
	if err == nil {
		t.Fatal("expected error for chat with no argument, got nil")
	}
}

func TestParseNoFlagsInScope(t *testing.T) {
	scope := dscope.New()
	_, err := Parse(scope, []string{"chat", "hello"})
	if err == nil {
		t.Fatal("expected error for unknown flag in empty scope, got nil")
	}
}

func TestParseDoesNotMutateOriginalScope(t *testing.T) {
	scope := dscope.New(Module{})
	_, err := Parse(scope, []string{"chat", "hello"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	originalChats := dscope.Get[Chats](scope)
	if len(originalChats) != 0 {
		t.Fatalf("original scope should be unchanged, got %v", originalChats)
	}
}

func TestParseNilNewValue(t *testing.T) {
	scope := dscope.New(nilFlagModule{})
	_, err := Parse(scope, []string{"nilflag"})
	if err == nil {
		t.Fatal("expected error for nil return value, got nil")
	}
}

func TestParseEffort(t *testing.T) {
	scope := dscope.New(Module{})
	result, err := Parse(scope, []string{"-effort", "high"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := dscope.Get[Effort](result)
	if got != "high" {
		t.Fatalf("expected high, got %v", got)
	}
}

func TestParseEffortNoArg(t *testing.T) {
	scope := dscope.New(Module{})
	_, err := Parse(scope, []string{"-effort"})
	if err == nil {
		t.Fatal("expected error for effort with no argument, got nil")
	}
}

func TestParseFiles(t *testing.T) {
	scope := dscope.New(Module{})
	result, err := Parse(scope, []string{"-file", "a.go", "-file", "b.go"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := dscope.Get[Files](result)
	if !got["a.go"] || !got["b.go"] {
		t.Fatalf("expected a.go and b.go, got %v", got)
	}
}

func TestParseFilesNoArg(t *testing.T) {
	scope := dscope.New(Module{})
	_, err := Parse(scope, []string{"-file"})
	if err == nil {
		t.Fatal("expected error for file with no argument, got nil")
	}
}

func TestParseFocus(t *testing.T) {
	scope := dscope.New(Module{})
	result, err := Parse(scope, []string{"-focus", "foo", "-focus", "bar"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := dscope.Get[Focus](result)
	if len(got) != 2 || got[0] != "foo" || got[1] != "bar" {
		t.Fatalf("expected [foo bar], got %v", got)
	}
}

func TestParseFocusNoArg(t *testing.T) {
	scope := dscope.New(Module{})
	_, err := Parse(scope, []string{"-focus"})
	if err == nil {
		t.Fatal("expected error for focus with no argument, got nil")
	}
}

func TestParseIgnoreWithAlias(t *testing.T) {
	scope := dscope.New(Module{})
	result, err := Parse(scope, []string{"-ignore", "a", "-skip", "b", "-exclude", "c"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := dscope.Get[Ignore](result)
	if !got["a"] || !got["b"] || !got["c"] {
		t.Fatalf("expected a, b, c, got %v", got)
	}
}

func TestParseIgnoreNoArg(t *testing.T) {
	scope := dscope.New(Module{})
	_, err := Parse(scope, []string{"-ignore"})
	if err == nil {
		t.Fatal("expected error for ignore with no argument, got nil")
	}
}

func TestParseMatchWithAlias(t *testing.T) {
	scope := dscope.New(Module{})
	result, err := Parse(scope, []string{"-match", "a", "-include", "b"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := dscope.Get[Match](result)
	if !got["a"] || !got["b"] {
		t.Fatalf("expected a, b, got %v", got)
	}
}

func TestParseMatchNoArg(t *testing.T) {
	scope := dscope.New(Module{})
	_, err := Parse(scope, []string{"-match"})
	if err == nil {
		t.Fatal("expected error for match with no argument, got nil")
	}
}

func TestParseModelName(t *testing.T) {
	scope := dscope.New(Module{})
	result, err := Parse(scope, []string{"-model", "gpt-4"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := dscope.Get[ModelName](result)
	if got != "gpt-4" {
		t.Fatalf("expected gpt-4, got %v", got)
	}
}

func TestParseModelNameNoArg(t *testing.T) {
	scope := dscope.New(Module{})
	_, err := Parse(scope, []string{"-model"})
	if err == nil {
		t.Fatal("expected error for model with no argument, got nil")
	}
}

func TestParseShellTrue(t *testing.T) {
	scope := dscope.New(Module{})
	result, err := Parse(scope, []string{"-shell"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := dscope.Get[Shell](result)
	if !bool(got) {
		t.Fatalf("expected true, got %v", got)
	}
}

func TestParseShellFalse(t *testing.T) {
	scope := dscope.New(Module{})
	result, err := Parse(scope, []string{"-no-shell"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := dscope.Get[Shell](result)
	if bool(got) {
		t.Fatalf("expected false, got %v", got)
	}
}

func TestParseShellToggle(t *testing.T) {
	scope := dscope.New(Module{})
	result, err := Parse(scope, []string{"-shell", "-no-shell"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := dscope.Get[Shell](result)
	if bool(got) {
		t.Fatalf("expected false after toggle, got %v", got)
	}
}

func TestParseThoughtsTrue(t *testing.T) {
	scope := dscope.New(Module{})
	result, err := Parse(scope, []string{"-thoughts"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := dscope.Get[Thoughts](result)
	if got.Value == nil || !*got.Value {
		t.Fatalf("expected true, got %v", got.Value)
	}
}

func TestParseThoughtsFalse(t *testing.T) {
	scope := dscope.New(Module{})
	result, err := Parse(scope, []string{"-no-thoughts"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := dscope.Get[Thoughts](result)
	if got.Value == nil || *got.Value {
		t.Fatalf("expected false, got %v", got.Value)
	}
}

func TestParseMixedFlags(t *testing.T) {
	scope := dscope.New(Module{})
	result, err := Parse(scope, []string{
		"-model", "gpt-4",
		"-effort", "high",
		"-shell",
		"chat", "hello",
		"-focus", "target",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := dscope.Get[ModelName](result); got != "gpt-4" {
		t.Fatalf("expected model gpt-4, got %v", got)
	}
	if got := dscope.Get[Effort](result); got != "high" {
		t.Fatalf("expected effort high, got %v", got)
	}
	if got := dscope.Get[Shell](result); !bool(got) {
		t.Fatalf("expected shell true, got %v", got)
	}
	if got := dscope.Get[Chats](result); len(got) != 1 || got[0] != "hello" {
		t.Fatalf("expected chats [hello], got %v", got)
	}
	if got := dscope.Get[Focus](result); len(got) != 1 || got[0] != "target" {
		t.Fatalf("expected focus [target], got %v", got)
	}
}
