package flags

import (
	"testing"

	"github.com/reusee/dscope"
)

func TestParseCleanFlag(t *testing.T) {
	scope := dscope.New(Module{})
	result, err := Parse(scope, []string{"-clean"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	result.Call(func(chats Chats) {
		if len(chats) != 1 || chats[0] != cleanPrompt {
			t.Fatalf("expected [%s], got %v", cleanPrompt, chats)
		}
	})
}

func TestParseCleanAndChat(t *testing.T) {
	scope := dscope.New(Module{})
	result, err := Parse(scope, []string{"chat", "hello", "-clean"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	result.Call(func(chats Chats) {
		if len(chats) != 2 || chats[0] != "hello" || chats[1] != cleanPrompt {
			t.Fatalf("expected [hello %s], got %v", cleanPrompt, chats)
		}
	})
}

func TestCleanFlagRegistered(t *testing.T) {
	scope := dscope.New(Module{})
	scope.Call(func(chats Chats) {
		keys := chats.Keys()
		if _, ok := keys["-clean"]; !ok {
			t.Fatal("-clean flag not registered in Chats.Keys()")
		}
	})
}
