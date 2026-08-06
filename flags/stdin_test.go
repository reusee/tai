package flags

import (
	"os"
	"testing"

	"github.com/reusee/dscope"
)

func TestParseStdinFlag(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	if _, err := w.Write([]byte("stdin content")); err != nil {
		t.Fatal(err)
	}
	w.Close()

	oldStdin := os.Stdin
	os.Stdin = r
	defer func() { os.Stdin = oldStdin }()

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
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	if _, err := w.Write([]byte("stdin content")); err != nil {
		t.Fatal(err)
	}
	w.Close()

	oldStdin := os.Stdin
	os.Stdin = r
	defer func() { os.Stdin = oldStdin }()

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
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	if _, err := w.Write([]byte("stdin content")); err != nil {
		t.Fatal(err)
	}
	w.Close()

	oldStdin := os.Stdin
	os.Stdin = r
	defer func() { os.Stdin = oldStdin }()

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
