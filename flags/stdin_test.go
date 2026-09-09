package flags

import (
	"os"
	"testing"

	"github.com/reusee/dscope"
)

func withStdinPipe(t *testing.T, content string) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { r.Close() })
	if _, err := w.Write([]byte(content)); err != nil {
		t.Fatal(err)
	}
	w.Close()
	oldStdin := os.Stdin
	os.Stdin = r
	t.Cleanup(func() { os.Stdin = oldStdin })
}

func TestParseStdinFlag(t *testing.T) {
	withStdinPipe(t, "stdin content")
	scope := dscope.New(Module{})
	result, err := Parse(scope, []string{"-stdin"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	result.Call(func(chats Chats) {
		if len(chats) != 1 || chats[0] != "stdin content" {
			t.Fatalf("expected [stdin content], got %v", chats)
		}
	})
}

func TestParseStdinAndChat(t *testing.T) {
	withStdinPipe(t, "stdin content")
	scope := dscope.New(Module{})
	result, err := Parse(scope, []string{"-stdin", "chat", "hello"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	result.Call(func(chats Chats) {
		if len(chats) != 2 || chats[0] != "stdin content" || chats[1] != "hello" {
			t.Fatalf("expected [stdin content hello], got %v", chats)
		}
	})
}

func TestParseChatAndStdin(t *testing.T) {
	withStdinPipe(t, "stdin content")
	scope := dscope.New(Module{})
	result, err := Parse(scope, []string{"chat", "hello", "-stdin"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	result.Call(func(chats Chats) {
		if len(chats) != 2 || chats[0] != "hello" || chats[1] != "stdin content" {
			t.Fatalf("expected [hello stdin content], got %v", chats)
		}
	})
}

func TestStdinFlagRegistered(t *testing.T) {
	scope := dscope.New(Module{})
	scope.Call(func(chats Chats) {
		keys := chats.Keys()
		if _, ok := keys["-stdin"]; !ok {
			t.Fatal("-stdin flag not registered in Chats.Keys()")
		}
	})
}
