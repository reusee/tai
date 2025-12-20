package gocodes

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/reusee/dscope"
	"github.com/reusee/tai/configs"
	"github.com/reusee/tai/generators"
	"github.com/reusee/tai/modes"
)

func TestContextPrompt(t *testing.T) {
	scope := dscope.New(
		modes.ForTest(t),
		new(Module),
		dscope.Provide(configs.NewLoader(nil, "")),
	)

	dir := filepath.Join(testdataDir, "main")
	scope.Fork(
		func() LoadDir {
			return LoadDir(dir)
		},
	).Call(func(
		provider CodeProvider,
		count generators.GeminiTokenCounter,
	) {

		parts, err := provider.Parts(256, count("gemini-1.5-pro"), nil)
		if err != nil {
			t.Fatal(err)
		}

		if len(parts) != 3 {
			t.Fatalf("got %v", len(parts))
		}

		if text, ok := parts[0].(generators.Text); !ok {
			t.Fatalf("got %#v", parts[0])
		} else if !strings.Contains(string(text), filepath.Join(dir, "..", "dep1", "dep1.go")) {
			t.Fatalf("got %v", text)
		}

		if text, ok := parts[1].(generators.Text); !ok {
			t.Fatalf("got %#v", parts[1])
		} else if !strings.Contains(string(text), filepath.Join(dir, "a.txt")) {
			t.Fatalf("got %v", text)
		}

		if text, ok := parts[2].(generators.Text); !ok {
			t.Fatalf("got %#v", parts[2])
		} else if !strings.Contains(string(text), filepath.Join(dir, "main.go")) {
			t.Fatalf("got %v", text)
		}

	})

}
