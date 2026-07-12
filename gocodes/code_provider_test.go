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
		new(configs.NewLoader(nil, configs.LoaderConfig{})),
	)

	dir := filepath.Join(testdataDir, "main")
	scope.Fork(
		func() LoadDir {
			return LoadDir(dir)
		},
	).Call(func(
		provider CodeProvider,
	) {

		parts, err := provider.Parts(256, generators.DeepseekTokenCounterFn, nil)
		if err != nil {
			t.Fatal(err)
		}

		if len(parts) != 3 {
			t.Fatalf("got %v", len(parts))
		}

		var foundDep1, foundATxt, foundMain bool
		for _, part := range parts {
			text, ok := part.(generators.Text)
			if !ok {
				t.Fatalf("got %#v", part)
			}
			s := string(text)
			if strings.Contains(s, filepath.Join(dir, "..", "dep1", "dep1.go")) {
				foundDep1 = true
			}
			if strings.Contains(s, filepath.Join(dir, "a.txt")) {
				foundATxt = true
			}
			if strings.Contains(s, filepath.Join(dir, "main.go")) {
				foundMain = true
			}
		}
		if !foundDep1 {
			t.Errorf("dep1.go not found")
		}
		if !foundATxt {
			t.Errorf("a.txt not found")
		}
		if !foundMain {
			t.Errorf("main.go not found")
		}

	})

}

func TestExcludePatternDirectoryPrefix(t *testing.T) {
	scope := dscope.New(
		modes.ForTest(t),
		new(Module),
		new(configs.NewLoader(nil, configs.LoaderConfig{})),
	)

	dir := filepath.Join(testdataDir, "main")
	scope.Fork(
		func() LoadDir {
			return LoadDir(dir)
		},
	).Call(func(
		provider CodeProvider,
	) {
		// Exclude the dep1 directory. Before the fix, this pattern only
		// matched files exactly named "dep1", not files under the dep1
		// directory, so dep1.go would not be excluded.
		parts, err := provider.Parts(256, generators.DeepseekTokenCounterFn, []string{"!../dep1"})
		if err != nil {
			t.Fatal(err)
		}
		for _, part := range parts {
			if text, ok := part.(generators.Text); ok {
				if strings.Contains(string(text), "dep1.go") {
					t.Fatalf("dep1.go should be excluded by !../dep1 pattern")
				}
			}
		}
	})
}
