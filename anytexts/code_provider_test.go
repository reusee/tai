package anytexts

import (
	"math"
	"os"
	"strings"
	"testing"

	"github.com/reusee/dscope"
	"github.com/reusee/tai/configs"
	"github.com/reusee/tai/generators"
	"github.com/reusee/tai/modes"
)

func TestContextPrompt(t *testing.T) {
	dscope.New(
		new(Module),
		dscope.Provide(configs.NewLoader(nil, "")),
		modes.ForTest(t),
	).Fork(
		func() FileNameOK {
			return func(name string) bool {
				return strings.HasSuffix(strings.ToLower(name), ".py")
			}
		},
	).Call(func(
		provider CodeProvider,
		countTokens generators.BPETokenCounter,
	) {
		parts, err := provider.Parts(math.MaxInt, countTokens)
		if err != nil {
			t.Fatal(err)
		}
		if len(parts) != 1 {
			t.Fatalf("got %v", len(parts))
		}
		text, ok := parts[0].(generators.Text)
		if !ok {
			t.Fatalf("got %#v", parts[0])
		}
		if !strings.Contains(string(text), "hello, world!") {
			t.Fatalf("got %v", text)
		}
	})
}

func TestCodeProviderFromCurrentDir(t *testing.T) {
	dir := t.TempDir()
	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(oldWd)
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	content := "test content"
	if err := os.WriteFile("test.txt", []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	dscope.New(
		new(Module),
		dscope.Provide(configs.NewLoader(nil, "")),
		modes.ForTest(t),
	).Fork(
		func() Files {
			return []string{"."}
		},
	).Call(func(
		provider CodeProvider,
		countTokens generators.BPETokenCounter,
	) {
		parts, err := provider.Parts(math.MaxInt, countTokens)
		if err != nil {
			t.Fatal(err)
		}
		if len(parts) != 1 {
			t.Fatalf("expected 1 part, got %d", len(parts))
		}
		text, ok := parts[0].(generators.Text)
		if !ok {
			t.Fatalf("got %#v", parts)
		}
		if !strings.Contains(string(text), content) {
			t.Fatalf("got %q, want to contain %q", string(text), content)
		}
	})
}
