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

func (NilFlag) Handle(key string, args []string) (newDef any, remainArgs []string, err error) {
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
	result.Call(func(chats Chats) {
		if len(chats) != 0 {
			t.Fatalf("expected empty chats, got %v", chats)
		}
	})
}

func TestParseSingleChat(t *testing.T) {
	scope := dscope.New(Module{})
	result, err := Parse(scope, []string{"chat", "hello"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	result.Call(func(chats Chats) {
		if len(chats) != 1 || chats[0] != "hello" {
			t.Fatalf("expected [hello], got %v", chats)
		}
	})
}

// TestParseMultipleChatsAccumulate verifies that repeated chat flags
// accumulate values rather than overwriting the previous one.
func TestParseMultipleChatsAccumulate(t *testing.T) {
	scope := dscope.New(Module{})
	result, err := Parse(scope, []string{"chat", "a", "chat", "b"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	result.Call(func(chats Chats) {
		if len(chats) != 2 || chats[0] != "a" || chats[1] != "b" {
			t.Fatalf("expected [a b], got %v", chats)
		}
	})
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
	scope.Call(func(chats Chats) {
		if len(chats) != 0 {
			t.Fatalf("original scope should be unchanged, got %v", chats)
		}
	})
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
	result.Call(func(effort Effort) {
		if effort != "high" {
			t.Fatalf("expected high, got %v", effort)
		}
	})
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
	result.Call(func(files Files) {
		if !files["a.go"] || !files["b.go"] {
			t.Fatalf("expected a.go and b.go, got %v", files)
		}
	})
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
	result.Call(func(focus Focus) {
		if len(focus) != 2 || focus[0] != "foo" || focus[1] != "bar" {
			t.Fatalf("expected [foo bar], got %v", focus)
		}
	})
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
	result.Call(func(ignore Ignore) {
		if !ignore["a"] || !ignore["b"] || !ignore["c"] {
			t.Fatalf("expected a, b, c, got %v", ignore)
		}
	})
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
	result.Call(func(match Match) {
		if !match["a"] || !match["b"] {
			t.Fatalf("expected a, b, got %v", match)
		}
	})
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
	result.Call(func(name ModelName) {
		if name != "gpt-4" {
			t.Fatalf("expected gpt-4, got %v", name)
		}
	})
}

func TestParseModelNameNoArg(t *testing.T) {
	scope := dscope.New(Module{})
	_, err := Parse(scope, []string{"-model"})
	if err == nil {
		t.Fatal("expected error for model with no argument, got nil")
	}
}

func TestParseFastModelName(t *testing.T) {
	scope := dscope.New(Module{})
	result, err := Parse(scope, []string{"-fast-model", "gpt-4o-mini"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	result.Call(func(name FastModelName) {
		if name != "gpt-4o-mini" {
			t.Fatalf("expected gpt-4o-mini, got %v", name)
		}
	})
}

func TestParseFastModelNameNoArg(t *testing.T) {
	scope := dscope.New(Module{})
	_, err := Parse(scope, []string{"-fast-model"})
	if err == nil {
		t.Fatal("expected error for fast-model with no argument, got nil")
	}
}

func TestParseShellTrue(t *testing.T) {
	scope := dscope.New(Module{})
	result, err := Parse(scope, []string{"-shell"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	result.Call(func(shell Shell) {
		if !bool(shell) {
			t.Fatalf("expected true, got %v", shell)
		}
	})
}

func TestParseShellFalse(t *testing.T) {
	scope := dscope.New(Module{})
	result, err := Parse(scope, []string{"-no-shell"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	result.Call(func(shell Shell) {
		if bool(shell) {
			t.Fatalf("expected false, got %v", shell)
		}
	})
}

func TestParseShellToggle(t *testing.T) {
	scope := dscope.New(Module{})
	result, err := Parse(scope, []string{"-shell", "-no-shell"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	result.Call(func(shell Shell) {
		if bool(shell) {
			t.Fatalf("expected false after toggle, got %v", shell)
		}
	})
}

func TestParseThoughtsTrue(t *testing.T) {
	scope := dscope.New(Module{})
	result, err := Parse(scope, []string{"-thoughts"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	result.Call(func(thoughts Thoughts) {
		if thoughts.Value == nil || !*thoughts.Value {
			t.Fatalf("expected true, got %v", thoughts.Value)
		}
	})
}

func TestParseThoughtsFalse(t *testing.T) {
	scope := dscope.New(Module{})
	result, err := Parse(scope, []string{"-no-thoughts"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	result.Call(func(thoughts Thoughts) {
		if thoughts.Value == nil || *thoughts.Value {
			t.Fatalf("expected false, got %v", thoughts.Value)
		}
	})
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
	result.Call(func(
		name ModelName,
		effort Effort,
		shell Shell,
		chats Chats,
		focus Focus,
	) {
		if name != "gpt-4" {
			t.Fatalf("expected model gpt-4, got %v", name)
		}
		if effort != "high" {
			t.Fatalf("expected effort high, got %v", effort)
		}
		if !bool(shell) {
			t.Fatalf("expected shell true, got %v", shell)
		}
		if len(chats) != 1 || chats[0] != "hello" {
			t.Fatalf("expected chats [hello], got %v", chats)
		}
		if len(focus) != 1 || focus[0] != "target" {
			t.Fatalf("expected focus [target], got %v", focus)
		}
	})
}
