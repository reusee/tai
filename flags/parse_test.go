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

func (NilFlag) Key() string {
	return "nilflag"
}

func (NilFlag) Handle(args []string) (newValue any, remainArgs []string, err error) {
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
