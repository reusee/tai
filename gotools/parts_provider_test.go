package gotools

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/reusee/dscope"
	"github.com/reusee/tai/generators"
	"github.com/reusee/tai/logs"
	"github.com/reusee/tai/modes"
)

func TestContextPrompt(t *testing.T) {
	scope := dscope.New(
		modes.ForTest(t),
		new(Module),
	)

	dir := filepath.Join(testdataDir, "main")
	scope.Fork(
		func() LoadDir {
			return LoadDir(dir)
		},
	).Call(func(
		provider PartsProvider,
	) {

		parts, err := provider.Parts(256, generators.DeepseekTokenCounterFn, nil)
		if err != nil {
			t.Fatal(err)
		}

		// Focus packages are pinned at documentation level: dep1.go is
		// loaded as a context file and the focus package appears as a
		// documentation block. main.go's full content must not appear,
		// and the non-Go focus file a.txt is present by name in the
		// documentation block's file list only.
		// See TheoryOfVisibilityAllocation and TheoryOfNonGoFiles in
		// module_root.go.
		if len(parts) < 2 {
			t.Fatalf("got %v", len(parts))
		}

		var foundDep1, foundATxtName, foundFocusDoc bool
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
			if strings.Contains(s, "begin of focus file "+filepath.Join(dir, "main.go")) {
				t.Fatalf("main.go must not appear at full content; focus packages are documentation-only:\n%s", s)
			}
			if strings.Contains(s, "begin of focus file "+filepath.Join(dir, "a.txt")) {
				t.Fatalf("a.txt must not appear at full content; non-Go focus files are listed by name only:\n%s", s)
			}
			if strings.Contains(s, "begin of focus package") {
				foundFocusDoc = true
				if strings.Contains(s, filepath.Join(dir, "a.txt")) {
					foundATxtName = true
				}
			}
		}
		if !foundDep1 {
			t.Errorf("dep1.go not found")
		}
		if !foundFocusDoc {
			t.Errorf("focus package documentation not found")
		}
		if !foundATxtName {
			t.Errorf("a.txt not listed in the focus package documentation")
		}

	})

}

func TestPartsIncludesWorkingDirectoryHint(t *testing.T) {
	// The working directory hint must be appended after all file contents
	// so the model can construct correct absolute paths for change block
	// file-path attributes. See
	// anytexts.TheoryOfWorkingDirectoryHint.
	root := t.TempDir()
	t.Setenv("GOWORK", "")

	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module test\n\ngo 1.21\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0644); err != nil {
		t.Fatal(err)
	}

	dscope.New(
		modes.ForTest(t),
		new(Module),
	).Fork(
		func() LoadDir { return LoadDir(root) },
	).Call(func(
		provider PartsProvider,
		countTokens generators.BPETokenCounter,
	) {
		parts, err := provider.Parts(1<<20, countTokens, nil)
		if err != nil {
			t.Fatal(err)
		}
		if len(parts) == 0 {
			t.Fatal("expected at least one part")
		}
		last, ok := parts[len(parts)-1].(generators.Text)
		if !ok {
			t.Fatalf("expected the last part to be Text, got %T", parts[len(parts)-1])
		}
		cwd, err := os.Getwd()
		if err != nil {
			t.Fatal(err)
		}
		want := "Working directory: " + cwd
		if !strings.Contains(string(last), want) {
			t.Fatalf("expected the last part to carry the working directory hint %q, got %q", want, string(last))
		}
	})
}

func TestExcludePatternExcludesWorkspaceMarkdown(t *testing.T) {
	// In workspace mode, markdown files in sibling modules are listed by
	// the module-root listing part. An exclusion pattern like "!*.md"
	// must exclude the name from that listing too — its relative path
	// from the load directory is "../mod2/README.md", matched by the
	// basename rule. See TheoryOfExclusionPatterns and
	// TheoryOfNonGoFiles in module_root.go.
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
	)

	scope.Fork(
		func() LoadDir {
			return LoadDir(mod1Dir)
		},
	).Call(func(
		provider PartsProvider,
	) {
		parts, err := provider.Parts(1<<20, generators.DeepseekTokenCounterFn, []string{"!*.md"})
		if err != nil {
			t.Fatal(err)
		}
		for _, part := range parts {
			if text, ok := part.(generators.Text); ok {
				if strings.Contains(string(text), readmePath) {
					t.Fatalf("workspace sibling README.md should be excluded by !*.md pattern, including from the module-root listing")
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
	)

	// Full paths for checking file markers. main.go contains //go:embed
	// directives that reference both filenames, so checking for the bare
	// filename would always match. Instead, check for the begin marker
	// with the full path to determine whether the file is included at
	// full content.
	largePath := filepath.Join(dir, "large.txt")
	smallPath := filepath.Join(dir, "small.txt")

	scope.Fork(
		func() LoadDir {
			return LoadDir(dir)
		},
	).Call(func(
		provider PartsProvider,
	) {
		// Without patterns: the large embed file is excluded entirely
		// (not even listed), and the small embed file is present by name
		// in the focus documentation block's file list — neither is
		// emitted at full content. See TheoryOfNonGoFiles in
		// module_root.go.
		parts, err := provider.Parts(1<<20, generators.DeepseekTokenCounterFn, nil)
		if err != nil {
			t.Fatal(err)
		}

		var foundSmallName, foundLarge bool
		for _, part := range parts {
			if text, ok := part.(generators.Text); ok {
				s := string(text)
				if strings.Contains(s, "begin of focus file "+largePath) ||
					strings.Contains(s, "begin of context file "+largePath) {
					t.Fatal("large embed file must not be emitted at full content by default")
				}
				if strings.Contains(s, "begin of focus file "+smallPath) ||
					strings.Contains(s, "begin of context file "+smallPath) {
					t.Fatal("small embed file must not be emitted at full content; non-Go files are listed by name only")
				}
				if strings.Contains(s, smallPath) {
					foundSmallName = true
				}
				if strings.Contains(s, largePath) {
					foundLarge = true
				}
			}
		}
		if foundLarge {
			t.Fatal("large embed file should be excluded by default")
		}
		if !foundSmallName {
			t.Fatal("small embed file should be listed by name in the focus documentation")
		}

		// With an explicit -file pattern naming the file by its absolute
		// path: the file is requested, so it is emitted at full content
		// as an extra context file.
		parts, err = provider.Parts(1<<20, generators.DeepseekTokenCounterFn, []string{largePath})
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
	// dep1 becomes a root package (focus package), but its files are outside
	// writable directories (CWD, /tmp, Go dirs, config dir, /dev/shm).
	// The focus package is a documentation block whose begin marker
	// carries the "(read-only)" note, because focus Go files are no
	// longer emitted individually. The package's content still provides
	// useful reference context.
	// See TheoryOfFocusFileDirectoryCheck in anytexts/parts_provider.go.
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
	)

	scope.Fork(
		func() LoadDir {
			return LoadDir(mainDir)
		},
		func() LoadPatterns {
			return LoadPatterns{"../dep1"}
		},
	).Call(func(
		provider PartsProvider,
		countTokens generators.BPETokenCounter,
	) {
		parts, err := provider.Parts(1<<20, countTokens, nil)
		if err != nil {
			t.Fatalf("expected no error for focus file outside writable directories, got: %v", err)
		}
		foundReadOnly := false
		for _, part := range parts {
			if text, ok := part.(generators.Text); ok {
				if strings.Contains(string(text), "focus package") && strings.Contains(string(text), "(read-only)") {
					foundReadOnly = true
				}
			}
		}
		if !foundReadOnly {
			t.Fatal("expected focus package documentation outside writable directories to carry the read-only marker")
		}
	})
}

func TestPartsTokenCompositionLog(t *testing.T) {
	// The assembled token composition must appear in the logs: focus
	// project files, context project files, extra files, and package
	// documentation. This makes the context token budget observable
	// without enabling per-file token logs. See TheoryOfTokenComposition.
	root := t.TempDir()

	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module test\n\ngo 1.23\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n\nimport \"test/dep\"\n\nfunc main() { dep.Foo() }\n"), 0644); err != nil {
		t.Fatal(err)
	}
	depDir := filepath.Join(root, "dep")
	if err := os.MkdirAll(depDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(depDir, "dep.go"), []byte("package dep\n\n// Foo does something.\nfunc Foo() {}\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// The logger is forked directly so the test controls the output
	// sink; forking the logs.Writer would be ignored when the logger
	// provider detects a systemd service (which creates only a journal
	// handler). See TheoryOfUsageLogging in loops/run.go.
	var buf bytes.Buffer
	logger := logs.Logger{slog.New(slog.NewTextHandler(&buf, nil))}
	dscope.New(
		modes.ForTest(t),
		new(Module),
	).Fork(
		func() LoadDir { return LoadDir(root) },
		func() logs.Logger { return logger },
	).Call(func(
		provider PartsProvider,
		countTokens generators.BPETokenCounter,
	) {
		_, err := provider.Parts(1<<20, countTokens, nil)
		if err != nil {
			t.Fatal(err)
		}
	})

	output := buf.String()
	if !strings.Contains(output, `msg="token composition"`) {
		t.Fatalf("expected token composition log, got: %s", output)
	}
	for _, want := range []string{
		" focus=",
		" context=",
		" extra=",
		" doc=",
		" total=",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("expected key %q in token composition log, got: %s", want, output)
		}
	}
}

func TestModuleRootListingSummaryHint(t *testing.T) {
	// The module-root listing must announce that its content is summary
	// form: to modify or fully understand a listed file, the original
	// must be fetched with an ingest block. The listing carries each
	// listed file's parsed skeleton. See TheoryOfNonGoFiles in
	// module_root.go and anytexts.TheoryOfContextSkeleton.
	root := t.TempDir()
	t.Setenv("GOWORK", "")

	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module test\n\ngo 1.21\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "app.py"), []byte("def handler(request):\n    return request\n"), 0644); err != nil {
		t.Fatal(err)
	}
	subDir := filepath.Join(root, "pkg")
	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(subDir, "pkg.go"), []byte("package pkg\n\nfunc Foo() {}\n"), 0644); err != nil {
		t.Fatal(err)
	}

	dscope.New(
		modes.ForTest(t),
		new(Module),
	).Fork(
		func() LoadDir { return LoadDir(root) },
	).Call(func(
		provider PartsProvider,
	) {
		parts, err := provider.Parts(1<<20, generators.DeepseekTokenCounterFn, nil)
		if err != nil {
			t.Fatal(err)
		}
		foundListing := false
		for _, part := range parts {
			text, ok := part.(generators.Text)
			if !ok {
				continue
			}
			s := string(text)
			if !strings.Contains(s, "begin of module root files") {
				continue
			}
			foundListing = true
			if !strings.Contains(s, "app.py") {
				t.Errorf("listing must contain app.py, got:\n%s", s)
			}
			if !strings.Contains(s, "handler") {
				t.Errorf("listing must carry the skeleton of app.py, got:\n%s", s)
			}
			if !strings.Contains(s, "ingest block") {
				t.Errorf("listing must state that the content is summary form requiring ingest fetches, got:\n%s", s)
			}
		}
		if !foundListing {
			t.Fatal("expected a module root listing part")
		}
	})
}
