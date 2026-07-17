package gocodes

import (
	"os"
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

func TestLargeEmbedFileFiltered(t *testing.T) {
	dir := t.TempDir()

	// Create go.mod
	err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module test\n"), 0644)
	if err != nil {
		t.Fatal(err)
	}

	// Create a large embed file (>64KB)
	largeContent := make([]byte, 65*1024)
	for i := range largeContent {
		largeContent[i] = 'a'
	}
	err = os.WriteFile(filepath.Join(dir, "large.txt"), largeContent, 0644)
	if err != nil {
		t.Fatal(err)
	}

	// Create a small embed file
	smallContent := []byte("small content")
	err = os.WriteFile(filepath.Join(dir, "small.txt"), smallContent, 0644)
	if err != nil {
		t.Fatal(err)
	}

	// Create main.go that embeds both files
	mainContent := `package main

import _ "embed"

//go:embed large.txt
var large string

//go:embed small.txt
var small string

func main() {}
`
	err = os.WriteFile(filepath.Join(dir, "main.go"), []byte(mainContent), 0644)
	if err != nil {
		t.Fatal(err)
	}

	scope := dscope.New(
		modes.ForTest(t),
		new(Module),
		new(configs.NewLoader(nil, configs.LoaderConfig{})),
	)

	// Full paths for checking file markers. main.go contains //go:embed
	// directives that reference both filenames, so checking for the bare
	// filename would always match. Instead, check for the begin marker
	// with the full path to determine whether the file is included as a
	// separate context/focus entry.
	largePath := filepath.Join(dir, "large.txt")
	smallPath := filepath.Join(dir, "small.txt")

	scope.Fork(
		func() LoadDir {
			return LoadDir(dir)
		},
	).Call(func(
		provider CodeProvider,
	) {
		// Without patterns: large embed file should be excluded, small should be included
		parts, err := provider.Parts(1<<20, generators.DeepseekTokenCounterFn, nil)
		if err != nil {
			t.Fatal(err)
		}

		var foundLarge, foundSmall bool
		for _, part := range parts {
			if text, ok := part.(generators.Text); ok {
				s := string(text)
				if strings.Contains(s, "begin of focus file "+largePath) ||
					strings.Contains(s, "begin of context file "+largePath) {
					foundLarge = true
				}
				if strings.Contains(s, "begin of focus file "+smallPath) ||
					strings.Contains(s, "begin of context file "+smallPath) {
					foundSmall = true
				}
			}
		}
		if foundLarge {
			t.Fatal("large embed file should be excluded by default")
		}
		if !foundSmall {
			t.Fatal("small embed file should be included")
		}

		// With -file pattern: large embed file should be included
		parts, err = provider.Parts(1<<20, generators.DeepseekTokenCounterFn, []string{"large.txt"})
		if err != nil {
			t.Fatal(err)
		}

		foundLarge = false
		for _, part := range parts {
			if text, ok := part.(generators.Text); ok {
				s := string(text)
				if strings.Contains(s, "begin of focus file "+largePath) ||
					strings.Contains(s, "begin of context file "+largePath) {
					foundLarge = true
				}
			}
		}
		if !foundLarge {
			t.Fatal("large embed file should be included when explicitly requested via pattern")
		}
	})
}
