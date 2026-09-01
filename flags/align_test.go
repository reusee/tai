package flags

import (
	"testing"

	"github.com/reusee/dscope"
)

func TestParseAlignFlag(t *testing.T) {
	scope := dscope.New(Module{})
	result, err := Parse(scope, []string{"align"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	result.Call(func(chats Chats) {
		if len(chats) != 1 || chats[0] != alignPrompt {
			t.Fatalf("expected [%s], got %v", alignPrompt, chats)
		}
	})
}

func TestParseAlignAndClean(t *testing.T) {
	scope := dscope.New(Module{})
	result, err := Parse(scope, []string{"align", "clean"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	result.Call(func(chats Chats) {
		if len(chats) != 2 || chats[0] != alignPrompt || chats[1] != cleanPrompt {
			t.Fatalf("expected [%s %s], got %v", alignPrompt, cleanPrompt, chats)
		}
	})
}

func TestAlignFlagRegistered(t *testing.T) {
	scope := dscope.New(Module{})
	scope.Call(func(chats Chats) {
		keys := chats.Keys()
		if _, ok := keys["align"]; !ok {
			t.Fatal("align flag not registered in Chats.Keys()")
		}
	})
}
