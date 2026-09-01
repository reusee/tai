package flags

import (
	"testing"

	"github.com/reusee/dscope"
)

func TestParseDistillFlag(t *testing.T) {
	scope := dscope.New(Module{})
	result, err := Parse(scope, []string{"distill"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	result.Call(func(chats Chats) {
		if len(chats) != 1 || chats[0] != distillPrompt {
			t.Fatalf("expected [%s], got %v", distillPrompt, chats)
		}
	})
}

func TestParseDistillAndChat(t *testing.T) {
	scope := dscope.New(Module{})
	result, err := Parse(scope, []string{"chat", "hello", "distill"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	result.Call(func(chats Chats) {
		if len(chats) != 2 || chats[0] != "hello" || chats[1] != distillPrompt {
			t.Fatalf("expected [hello %s], got %v", distillPrompt, chats)
		}
	})
}

func TestDistillFlagRegistered(t *testing.T) {
	scope := dscope.New(Module{})
	scope.Call(func(chats Chats) {
		keys := chats.Keys()
		if _, ok := keys["distill"]; !ok {
			t.Fatal("distill flag not registered in Chats.Keys()")
		}
	})
}
