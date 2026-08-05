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
			t.Logf("%s\n", part)
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
		// Exclude the dep1 directory. The pattern "pkg" must match both
		// files exactly named "pkg" and all files under the "pkg/"
		// directory, so dep1.go must be excluded.
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

func TestIsExcludedPathMatchesBasename(t *testing.T) {
	// Slash-less exclusion patterns must match the path's basename at any
	// depth, including paths with ".." prefixes from workspace sibling
	// modules. Without this, -exclude "*.md" or -exclude README.md fails
	// to exclude automatically-included markdown files.
	// See TheoryOfExclusionPatterns.
	tests := []struct {
		name     string
		relPath  string
		patterns []string
		excluded bool
	}{
		{
			name:     "glob matches basename with dotdot prefix",
			relPath:  "../mod2/README.md",
			patterns: []string{"*.md"},
			excluded: true,
		},
		{
			name:     "plain name matches basename with dotdot prefix",
			relPath:  "../mod2/README.md",
			patterns: []string{"README.md"},
			excluded: true,
		},
		{
			name:     "glob matches basename in subdirectory",
			relPath:  "docs/README.md",
			patterns: []string{"*.md"},
			excluded: true,
		},
		{
			name:     "plain name matches basename in subdirectory",
			relPath:  "docs/README.md",
			patterns: []string{"README.md"},
			excluded: true,
		},
		{
			name:     "slash pattern matches dotdot-stripped path",
			relPath:  "../mod2/README.md",
			patterns: []string{"mod2/README.md"},
			excluded: true,
		},
		{
			name:     "directory prefix matches dotdot-stripped path",
			relPath:  "../mod2/sub/file.go",
			patterns: []string{"mod2"},
			excluded: true,
		},
		{
			name:     "unrelated file not excluded",
			relPath:  "../mod2/main.go",
			patterns: []string{"*.md"},
			excluded: false,
		},
		{
			name:     "go file not excluded by md pattern",
			relPath:  "docs/README.md",
			patterns: []string{"*.go"},
			excluded: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isExcludedPath(tt.relPath, tt.patterns)
			if got != tt.excluded {
				t.Fatalf("isExcludedPath(%q, %v) = %v, want %v", tt.relPath, tt.patterns, got, tt.excluded)
			}
		})
	}
}

func TestExcludePatternExcludesWorkspaceMarkdown(t *testing.T) {
	// In workspace mode, markdown files in sibling modules are
	// automatically included. An exclusion pattern like "!*.md" must
	// exclude them even though their relative path from the load
	// directory is "../mod2/README.md". See TheoryOfExclusionPatterns.
	root := t.TempDir()
	t.Setenv("GOWORK", "")

	if err := os.WriteFile(filepath.Join(root, "go.work"), []byte(`go 1.21

use (
	./mod1
	./mod2
)
`), 0644); err != nil {
		t.Fatal(err)
	}

	mod1Dir := filepath.Join(root, "mod1")
	if err := os.MkdirAll(mod1Dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mod1Dir, "go.mod"), []byte("module example.com/mod1\n\ngo 1.21\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mod1Dir, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0644); err != nil {
		t.Fatal(err)
	}

	mod2Dir := filepath.Join(root, "mod2")
	if err := os.MkdirAll(filepath.Join(mod2Dir, "sub"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mod2Dir, "go.mod"), []byte("module example.com/mod2\n\ngo 1.21\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mod2Dir, "sub", "mod2.go"), []byte("package sub\n\nfunc Foo() {}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	readmePath := filepath.Join(mod2Dir, "README.md")
	if err := os.WriteFile(readmePath, []byte("# mod2\n"), 0644); err != nil {
		t.Fatal(err)
	}

	scope := dscope.New(
		modes.ForTest(t),
		new(Module),
		new(configs.NewLoader(nil, configs.LoaderConfig{})),
	)

	scope.Fork(
		func() LoadDir {
			return LoadDir(mod1Dir)
		},
	).Call(func(
		provider CodeProvider,
	) {
		parts, err := provider.Parts(1<<20, generators.DeepseekTokenCounterFn, []string{"!*.md"})
		if err != nil {
			t.Fatal(err)
		}
		for _, part := range parts {
			if text, ok := part.(generators.Text); ok {
				if strings.Contains(string(text), "begin of focus file "+readmePath) {
					t.Fatal("workspace sibling README.md should be excluded by !*.md pattern")
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

func TestFocusFileOutsideWritableDirs(t *testing.T) {
	// When -pkg ../dep1 is used from a working directory under /var/tmp,
	// dep1 becomes a root package (focus file), but its files are outside
	// writable directories (CWD, /tmp, Go dirs, config dir, /dev/shm).
	// The files should be marked as read-only and included in the context
	// with "(read-only)" markers rather than rejected.
	// See TheoryOfFocusFileDirectoryCheck in anytexts/code_provider.go.
	//
	// The module root is placed under /var/tmp (not /tmp) so that
	// sibling directories like dep1 are outside writable dirs. If
	// /var/tmp is unavailable, skip the test.
	root, err := os.MkdirTemp("/var/tmp", "tai_test_")
	if err != nil {
		t.Skipf("cannot create temp dir in /var/tmp: %v", err)
	}
	defer os.RemoveAll(root)

	// Create go.mod at the module root so both main and dep1 are in the
	// same module. This lets the Go loader resolve ../dep1 from main.
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module test\n"), 0644); err != nil {
		t.Fatal(err)
	}

	mainDir := filepath.Join(root, "main")
	if err := os.MkdirAll(mainDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mainDir, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0644); err != nil {
		t.Fatal(err)
	}

	dep1Dir := filepath.Join(root, "dep1")
	if err := os.MkdirAll(dep1Dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dep1Dir, "dep1.go"), []byte("package dep1\n\nfunc Foo() {}\n"), 0644); err != nil {
		t.Fatal(err)
	}

	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(oldWd)
	if err := os.Chdir(mainDir); err != nil {
		t.Fatal(err)
	}

	scope := dscope.New(
		modes.ForTest(t),
		new(Module),
		new(configs.NewLoader(nil, configs.LoaderConfig{})),
	)

	scope.Fork(
		func() LoadDir {
			return LoadDir(mainDir)
		},
		func() LoadPatterns {
			return LoadPatterns{"../dep1"}
		},
	).Call(func(
		provider CodeProvider,
		countTokens generators.BPETokenCounter,
	) {
		parts, err := provider.Parts(1<<20, countTokens, nil)
		if err != nil {
			t.Fatalf("expected no error for focus file outside writable directories, got: %v", err)
		}
		foundReadOnly := false
		for _, part := range parts {
			if text, ok := part.(generators.Text); ok {
				if strings.Contains(string(text), "focus file") && strings.Contains(string(text), "(read-only)") {
					foundReadOnly = true
				}
			}
		}
		if !foundReadOnly {
			t.Fatal("expected focus file outside writable directories to be included with read-only marker")
		}
	})
}
