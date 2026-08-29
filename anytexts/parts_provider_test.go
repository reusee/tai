package anytexts

import (
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/reusee/dscope"
	"github.com/reusee/tai/flags"
	"github.com/reusee/tai/generators"
	"github.com/reusee/tai/modes"
)

func TestContextPrompt(t *testing.T) {
	dscope.New(
		new(Module),
		modes.ForTest(t),
	).Fork(
		func() FileNameOK {
			return func(name string) bool {
				return strings.HasSuffix(strings.ToLower(name), ".py")
			}
		},
	).Call(func(
		provider PartsProvider,
		countTokens generators.BPETokenCounter,
	) {
		parts, err := provider.Parts(math.MaxInt, countTokens, []string{"."})
		if err != nil {
			t.Fatal(err)
		}
		// The parts are the file contents followed by the working
		// directory hint. See TheoryOfWorkingDirectoryHint.
		if len(parts) != 2 {
			t.Fatalf("expected 2 parts (file content, working directory hint), got %d", len(parts))
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

func TestPartsProviderFromCurrentDir(t *testing.T) {
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
		modes.ForTest(t),
	).Call(func(
		provider PartsProvider,
		countTokens generators.BPETokenCounter,
	) {
		parts, err := provider.Parts(math.MaxInt, countTokens, []string{"."})
		if err != nil {
			t.Fatal(err)
		}
		// The parts are the file contents followed by the working
		// directory hint. See TheoryOfWorkingDirectoryHint.
		if len(parts) != 2 {
			t.Fatalf("expected 2 parts (file content, working directory hint), got %d", len(parts))
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

func TestPartsSeparatesUnitsWithBlankLine(t *testing.T) {
	// Every content unit must end with a blank line so consecutive units
	// stay paragraph-separated after verbatim part concatenation. See
	// generators.TheoryOfContentUnitSeparation.
	dir := t.TempDir()
	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(oldWd)
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("test.txt", []byte("test content"), 0644); err != nil {
		t.Fatal(err)
	}

	dscope.New(
		new(Module),
		modes.ForTest(t),
	).Call(func(
		provider PartsProvider,
		countTokens generators.BPETokenCounter,
	) {
		parts, err := provider.Parts(math.MaxInt, countTokens, []string{"."})
		if err != nil {
			t.Fatal(err)
		}
		if len(parts) != 2 {
			t.Fatalf("expected 2 parts (file content, working directory hint), got %d", len(parts))
		}
		for i, part := range parts {
			text, ok := part.(generators.Text)
			if !ok {
				t.Fatalf("part %d: got %#v", i, part)
			}
			if !strings.HasSuffix(string(text), "\n\n") {
				t.Fatalf("part %d must end with a blank line so consecutive units stay separated, got %q", i, string(text))
			}
		}
	})
}

func TestPartsProviderIncludesWorkingDirectoryHint(t *testing.T) {
	// The working directory hint must be appended after all file contents
	// so the model can construct correct absolute paths for change block
	// file-path attributes. The path is dynamic — it changes per
	// invocation — so it is located at the end, keeping the file contents
	// byte-identical in the LLM prefix cache. See
	// TheoryOfWorkingDirectoryHint.
	dir := t.TempDir()
	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(oldWd)
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("test.txt", []byte("test content"), 0644); err != nil {
		t.Fatal(err)
	}

	dscope.New(
		new(Module),
		modes.ForTest(t),
	).Call(func(
		provider PartsProvider,
		countTokens generators.BPETokenCounter,
	) {
		parts, err := provider.Parts(math.MaxInt, countTokens, []string{"."})
		if err != nil {
			t.Fatal(err)
		}
		if len(parts) < 2 {
			t.Fatalf("expected the file content and the working directory hint, got %d parts", len(parts))
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

func TestSymlinks(t *testing.T) {
	// nonWritableTempDir creates a temp dir outside all writable
	// directories (not under CWD, /tmp, Go dirs, config dir, /dev/shm).
	// /var/tmp is not in the writable dirs list. If unavailable, the
	// calling subtest is skipped.
	nonWritableTempDir := func(t *testing.T) string {
		t.Helper()
		dir, err := os.MkdirTemp("/var/tmp", "tai_test_")
		if err != nil {
			t.Skipf("cannot create temp dir in /var/tmp: %v", err)
		}
		t.Cleanup(func() { os.RemoveAll(dir) })
		return dir
	}

	t.Run("Followed", func(t *testing.T) {
		dir := t.TempDir()
		oldWd, err := os.Getwd()
		if err != nil {
			t.Fatal(err)
		}
		defer os.Chdir(oldWd)
		if err := os.Chdir(dir); err != nil {
			t.Fatal(err)
		}

		// Create a target directory with a file and a symlink to it.
		if err := os.MkdirAll("target", 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile("target/file.txt", []byte("symlink content"), 0644); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink("target", "link"); err != nil {
			t.Fatal(err)
		}

		dscope.New(
			new(Module),
			modes.ForTest(t),
		).Call(func(
			provider PartsProvider,
			countTokens generators.BPETokenCounter,
		) {
			parts, err := provider.Parts(math.MaxInt, countTokens, []string{"link"})
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, part := range parts {
				if text, ok := part.(generators.Text); ok {
					if strings.Contains(string(text), "symlink content") {
						found = true
						if strings.Contains(string(text), "(read-only)") {
							t.Fatal("internal symlink file should not be marked as read-only")
						}
					}
				}
			}
			if !found {
				t.Fatal("symlinked file content not found")
			}
		})
	})

	t.Run("CycleDetection", func(t *testing.T) {
		dir := t.TempDir()
		oldWd, err := os.Getwd()
		if err != nil {
			t.Fatal(err)
		}
		defer os.Chdir(oldWd)
		if err := os.Chdir(dir); err != nil {
			t.Fatal(err)
		}

		// Create a directory with a file and a back-link symlink that
		// points to an ancestor, creating a cycle:
		//   . -> sub -> sub/backlink -> . -> sub -> ...
		if err := os.MkdirAll("sub", 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile("sub/file.txt", []byte("content"), 0644); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink("..", "sub/backlink"); err != nil {
			t.Fatal(err)
		}

		dscope.New(
			new(Module),
			modes.ForTest(t),
		).Call(func(
			provider PartsProvider,
			countTokens generators.BPETokenCounter,
		) {
			parts, err := provider.Parts(math.MaxInt, countTokens, []string{"."})
			if err != nil {
				t.Fatal(err)
			}
			// The traversal must terminate and find sub/file.txt exactly once.
			count := 0
			for _, part := range parts {
				if text, ok := part.(generators.Text); ok {
					if strings.Contains(string(text), "sub/file.txt") {
						count++
					}
				}
			}
			if count != 1 {
				t.Fatalf("expected sub/file.txt to appear once, got %d", count)
			}
		})
	})

	t.Run("ExternalSymlink", func(t *testing.T) {
		dir := t.TempDir()
		oldWd, err := os.Getwd()
		if err != nil {
			t.Fatal(err)
		}
		defer os.Chdir(oldWd)
		if err := os.Chdir(dir); err != nil {
			t.Fatal(err)
		}

		// Create an external directory with a file outside writable dirs.
		externalDir := nonWritableTempDir(t)
		if err := os.WriteFile(filepath.Join(externalDir, "external.txt"), []byte("external content"), 0644); err != nil {
			t.Fatal(err)
		}
		// Create a symlink in the current directory pointing to the external file.
		if err := os.Symlink(filepath.Join(externalDir, "external.txt"), "link.txt"); err != nil {
			t.Fatal(err)
		}

		dscope.New(
			new(Module),
			modes.ForTest(t),
		).Call(func(
			provider PartsProvider,
			countTokens generators.BPETokenCounter,
		) {
			// A directly-specified focus file that resolves outside
			// writable directories is marked as read-only and included
			// in the context. See TheoryOfFocusFileDirectoryCheck.
			parts, err := provider.Parts(math.MaxInt, countTokens, []string{"link.txt"})
			if err != nil {
				t.Fatalf("expected no error for external symlink as focus file, got: %v", err)
			}
			foundReadOnly := false
			for _, part := range parts {
				if text, ok := part.(generators.Text); ok {
					if strings.Contains(string(text), "(read-only)") && strings.Contains(string(text), "external content") {
						foundReadOnly = true
					}
				}
			}
			if !foundReadOnly {
				t.Fatal("expected external symlink file to be included with read-only marker")
			}
		})
	})

	t.Run("ExternalSymlinkDirectory", func(t *testing.T) {
		dir := t.TempDir()
		oldWd, err := os.Getwd()
		if err != nil {
			t.Fatal(err)
		}
		defer os.Chdir(oldWd)
		if err := os.Chdir(dir); err != nil {
			t.Fatal(err)
		}

		// Create an external directory with a file.
		externalDir := nonWritableTempDir(t)
		if err := os.WriteFile(filepath.Join(externalDir, "nested.txt"), []byte("nested external content"), 0644); err != nil {
			t.Fatal(err)
		}
		// Create a symlink to the external directory.
		if err := os.Symlink(externalDir, "ext"); err != nil {
			t.Fatal(err)
		}

		dscope.New(
			new(Module),
			modes.ForTest(t),
		).Call(func(
			provider PartsProvider,
			countTokens generators.BPETokenCounter,
		) {
			// A directly-specified focus directory that resolves outside
			// writable directories is marked as read-only and included
			// in the context. See TheoryOfFocusFileDirectoryCheck.
			parts, err := provider.Parts(math.MaxInt, countTokens, []string{"ext"})
			if err != nil {
				t.Fatalf("expected no error for external symlink directory as focus file, got: %v", err)
			}
			foundReadOnly := false
			for _, part := range parts {
				if text, ok := part.(generators.Text); ok {
					if strings.Contains(string(text), "(read-only)") && strings.Contains(string(text), "nested external content") {
						foundReadOnly = true
					}
				}
			}
			if !foundReadOnly {
				t.Fatal("expected external symlink directory file to be included with read-only marker")
			}
		})
	})
}

func TestFocusFileOutsideWritableDirs(t *testing.T) {
	// A non-symlink file directly specified via a pattern that resolves
	// outside all writable directories should be marked as read-only at
	// collection time, not rejected. /var/tmp is not in the writable dirs
	// list. See TheoryOfFocusFileDirectoryCheck.
	externalDir, err := os.MkdirTemp("/var/tmp", "tai_test_")
	if err != nil {
		t.Skipf("cannot create temp dir in /var/tmp: %v", err)
	}
	defer os.RemoveAll(externalDir)

	externalPath := filepath.Join(externalDir, "external.txt")
	if err := os.WriteFile(externalPath, []byte("external content"), 0644); err != nil {
		t.Fatal(err)
	}

	dscope.New(
		new(Module),
		modes.ForTest(t),
	).Call(func(
		provider PartsProvider,
		countTokens generators.BPETokenCounter,
	) {
		parts, err := provider.Parts(math.MaxInt, countTokens, []string{externalPath})
		if err != nil {
			t.Fatalf("expected no error for focus file outside writable directories, got: %v", err)
		}
		foundReadOnly := false
		for _, part := range parts {
			if text, ok := part.(generators.Text); ok {
				if strings.Contains(string(text), "(read-only)") && strings.Contains(string(text), "external content") {
					foundReadOnly = true
				}
			}
		}
		if !foundReadOnly {
			t.Fatal("expected focus file outside writable directories to be included with read-only marker")
		}
	})
}

func TestFileOrderingByPath(t *testing.T) {
	dir := t.TempDir()
	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(oldWd)
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	// Create zzz.txt first and aaa.txt second, then set zzz.txt to an older
	// modification time and aaa.txt to a newer one. Files should be sorted by
	// path, not modification time, so aaa.txt must appear before zzz.txt
	// regardless of timestamps.
	if err := os.WriteFile("zzz.txt", []byte("zzz"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("aaa.txt", []byte("aaa"), 0644); err != nil {
		t.Fatal(err)
	}
	oldTime := time.Now().Add(-time.Hour)
	newTime := time.Now()
	if err := os.Chtimes("zzz.txt", oldTime, oldTime); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes("aaa.txt", newTime, newTime); err != nil {
		t.Fatal(err)
	}

	dscope.New(
		new(Module),
		modes.ForTest(t),
	).Call(func(
		provider PartsProvider,
		countTokens generators.BPETokenCounter,
	) {
		parts, err := provider.Parts(math.MaxInt, countTokens, []string{"."})
		if err != nil {
			t.Fatal(err)
		}
		// Files should be sorted by path, not by modification time.
		// aaa.txt should appear before zzz.txt regardless of modification times.
		aaaIdx := -1
		zzzIdx := -1
		for i, part := range parts {
			if text, ok := part.(generators.Text); ok {
				if strings.Contains(string(text), "begin of file ") && strings.Contains(string(text), "aaa.txt") {
					aaaIdx = i
				}
				if strings.Contains(string(text), "begin of file ") && strings.Contains(string(text), "zzz.txt") {
					zzzIdx = i
				}
			}
		}
		if aaaIdx == -1 || zzzIdx == -1 {
			t.Fatalf("files not found in parts: aaa at %d, zzz at %d", aaaIdx, zzzIdx)
		}
		if aaaIdx > zzzIdx {
			t.Fatalf("aaa.txt should appear before zzz.txt (path-based ordering), got aaa at index %d, zzz at index %d", aaaIdx, zzzIdx)
		}
	})
}

func TestExcludePatternDirectoryPrefix(t *testing.T) {
	dir := t.TempDir()
	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(oldWd)
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile("keep.txt", []byte("keep content"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll("pkg", 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("pkg/file.go", []byte("package pkg"), 0644); err != nil {
		t.Fatal(err)
	}

	dscope.New(
		new(Module),
		modes.ForTest(t),
	).Call(func(
		provider PartsProvider,
		countTokens generators.BPETokenCounter,
	) {
		parts, err := provider.Parts(math.MaxInt, countTokens, []string{".", "!./pkg"})
		if err != nil {
			t.Fatal(err)
		}
		foundKeep := false
		for _, part := range parts {
			if text, ok := part.(generators.Text); ok {
				s := string(text)
				if strings.Contains(s, "keep content") {
					foundKeep = true
				}
				if strings.Contains(s, "pkg/file.go") {
					t.Fatal("pkg/file.go should be excluded by !./pkg pattern")
				}
			}
		}
		if !foundKeep {
			t.Fatal("keep.txt should be included")
		}
	})
}

func TestIsExcludedPathMatchesBasename(t *testing.T) {
	// Slash-less exclusion patterns must match the path's basename at any
	// depth, following gitignore-style semantics, including paths with
	// ".." prefixes from sibling workspace modules and directory-prefix
	// patterns against dotdot-stripped paths. See TheoryOfPatternMatching.
	tests := []struct {
		name     string
		path     string
		patterns []string
		excluded bool
	}{
		{
			name:     "glob matches basename in subdirectory",
			path:     "docs/README.md",
			patterns: []string{"*.md"},
			excluded: true,
		},
		{
			name:     "plain name matches basename in subdirectory",
			path:     "docs/README.md",
			patterns: []string{"README.md"},
			excluded: true,
		},
		{
			name:     "absolute path basename match",
			path:     "/home/user/project/README.md",
			patterns: []string{"*.md"},
			excluded: true,
		},
		{
			name:     "glob matches basename with dotdot prefix",
			path:     "../mod2/README.md",
			patterns: []string{"*.md"},
			excluded: true,
		},
		{
			name:     "plain name matches basename with dotdot prefix",
			path:     "../mod2/README.md",
			patterns: []string{"README.md"},
			excluded: true,
		},
		{
			name:     "slash pattern matches dotdot-stripped path",
			path:     "../mod2/README.md",
			patterns: []string{"mod2/README.md"},
			excluded: true,
		},
		{
			name:     "directory prefix matches dotdot-stripped path",
			path:     "../mod2/sub/file.go",
			patterns: []string{"mod2"},
			excluded: true,
		},
		{
			name:     "unrelated file not excluded",
			path:     "docs/main.go",
			patterns: []string{"*.md"},
			excluded: false,
		},
		{
			name:     "unrelated dotdot file not excluded",
			path:     "../mod2/main.go",
			patterns: []string{"*.md"},
			excluded: false,
		},
		{
			name:     "go file not excluded by md pattern",
			path:     "docs/README.md",
			patterns: []string{"*.go"},
			excluded: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsExcludedPath(tt.path, tt.patterns)
			if got != tt.excluded {
				t.Fatalf("IsExcludedPath(%q, %v) = %v, want %v", tt.path, tt.patterns, got, tt.excluded)
			}
		})
	}
}

func TestBinaryFileTokenBudget(t *testing.T) {
	dir := t.TempDir()
	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(oldWd)
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	// Create a text file and a binary PNG file. The text file sorts first
	// alphabetically (a.txt < b.png), so it is processed before the binary
	// file in the IterFiles loop.
	if err := os.WriteFile("a.txt", []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}
	pngContent := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}
	if err := os.WriteFile("b.png", pngContent, 0644); err != nil {
		t.Fatal(err)
	}

	dscope.New(
		new(Module),
		modes.ForTest(t),
	).Fork(
		new(IncludeMimeTypes{
			"image/png": true,
		}),
	).Call(func(
		provider PartsProvider,
	) {
		// With DeepseekTokenCounterFn, the text file markers + content
		// ("``` begin of file a.txt\nhello\n``` end of file a.txt\n")
		// are ~52 runes * 0.3 = 15 tokens. The binary file markers are
		// ~65 runes * 0.3 = 19 tokens. With maxTokens=16, the text file
		// fits (15 <= 16) but the binary file markers push the total to
		// 34 > 16, so the binary file is skipped.
		parts, err := provider.Parts(16, generators.DeepseekTokenCounterFn, []string{"."})
		if err != nil {
			t.Fatal(err)
		}

		foundText := false
		foundBinary := false
		for _, part := range parts {
			if text, ok := part.(generators.Text); ok {
				s := string(text)
				if strings.Contains(s, "a.txt") {
					foundText = true
				}
				if strings.Contains(s, "b.png") {
					foundBinary = true
				}
			}
		}
		if !foundText {
			t.Fatal("text file should be included within token budget")
		}
		if foundBinary {
			t.Fatal("binary file should be skipped due to token limit (markers now counted)")
		}
	})
}

func TestIterFilesHiddenFileDirectlyMatched(t *testing.T) {
	dir := t.TempDir()
	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(oldWd)
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	// Create a hidden file and a non-hidden file
	if err := os.WriteFile(".env", []byte("SECRET=abc123"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("config.txt", []byte("config content"), 0644); err != nil {
		t.Fatal(err)
	}

	dscope.New(
		new(Module),
		modes.ForTest(t),
	).Call(func(
		provider PartsProvider,
		countTokens generators.BPETokenCounter,
	) {
		// Directly specifying a hidden file via pattern should include it
		parts, err := provider.Parts(math.MaxInt, countTokens, []string{".env"})
		if err != nil {
			t.Fatal(err)
		}

		foundHidden := false
		for _, part := range parts {
			if text, ok := part.(generators.Text); ok {
				if strings.Contains(string(text), "SECRET=abc123") {
					foundHidden = true
				}
			}
		}
		if !foundHidden {
			t.Fatal("hidden file directly specified via pattern should be included")
		}

		// Directory traversal should still skip hidden files
		parts, err = provider.Parts(math.MaxInt, countTokens, []string{"."})
		if err != nil {
			t.Fatal(err)
		}

		for _, part := range parts {
			if text, ok := part.(generators.Text); ok {
				if strings.Contains(string(text), "SECRET=abc123") {
					t.Fatal("hidden file should be skipped during directory traversal")
				}
			}
		}

		// Verify the non-hidden file is found during traversal
		foundConfig := false
		for _, part := range parts {
			if text, ok := part.(generators.Text); ok {
				if strings.Contains(string(text), "config content") {
					foundConfig = true
				}
			}
		}
		if !foundConfig {
			t.Fatal("non-hidden file should be included during directory traversal")
		}
	})
}

func TestMatchFlagFiltersFiles(t *testing.T) {
	dir := t.TempDir()
	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(oldWd)
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("foo.py", []byte("py content"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("bar.txt", []byte("txt content"), 0644); err != nil {
		t.Fatal(err)
	}

	// The -match flag reaches the file filter through the same path as
	// the tai command: flags.Parse forks flags.Match into the scope, and
	// the PartsProvider's injected NameMatch builds its regex filter from
	// the forked value. See TheoryOfMatchFiltering.
	scope := dscope.New(
		new(Module),
		modes.ForTest(t),
	)
	scope, err = flags.Parse(scope, []string{"-match", `\.py$`})
	if err != nil {
		t.Fatal(err)
	}
	scope.Call(func(
		provider PartsProvider,
		countTokens generators.BPETokenCounter,
	) {
		parts, err := provider.Parts(math.MaxInt, countTokens, []string{"."})
		if err != nil {
			t.Fatal(err)
		}
		foundPy, foundTxt := false, false
		for _, part := range parts {
			if text, ok := part.(generators.Text); ok {
				if strings.Contains(string(text), "foo.py") {
					foundPy = true
				}
				if strings.Contains(string(text), "bar.txt") {
					foundTxt = true
				}
			}
		}
		if !foundPy {
			t.Fatal("expected foo.py to be included by -match")
		}
		if foundTxt {
			t.Fatal("expected bar.txt to be excluded by -match")
		}
	})
}

func TestPartsProviderDirectMatchKeepsFullContent(t *testing.T) {
	// A directly matched -file target is a work target the user named, so
	// it keeps full content even with SkeletonFiles enabled. See
	// TheoryOfContextSkeleton.
	dir := t.TempDir()
	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(oldWd)
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	pyContent := "class Widget:\n    def render(self):\n        secret_body_value = 1\n        return secret_body_value\n"
	if err := os.WriteFile("widget.py", []byte(pyContent), 0644); err != nil {
		t.Fatal(err)
	}

	dscope.New(
		new(Module),
		modes.ForTest(t),
	).Fork(
		func() SkeletonFiles {
			return true
		},
	).Call(func(
		provider PartsProvider,
		countTokens generators.BPETokenCounter,
	) {
		parts, err := provider.Parts(math.MaxInt, countTokens, []string{"widget.py"})
		if err != nil {
			t.Fatal(err)
		}
		foundFull := false
		for _, part := range parts {
			text, ok := part.(generators.Text)
			if !ok {
				continue
			}
			s := string(text)
			if strings.Contains(s, "secret_body_value") {
				foundFull = true
			}
			if strings.Contains(s, "begin of skeleton of file") {
				t.Fatalf("directly matched file must not render as a skeleton, got:\n%s", s)
			}
		}
		if !foundFull {
			t.Fatal("directly matched file must keep full content")
		}
	})
}

func TestPartsProviderSkeletonForTraversalFiles(t *testing.T) {
	// With SkeletonFiles enabled, files discovered by traversal render as
	// parsed skeletons: supported formats carry the "skeleton of file"
	// markers and the structural outline without bodies; unsupported
	// formats fall back to full content under the plain file marker. See
	// TheoryOfContextSkeleton.
	dir := t.TempDir()
	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(oldWd)
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	pyContent := "class Widget:\n    def render(self):\n        secret_body_value = 1\n        return secret_body_value\n"
	if err := os.WriteFile("widget.py", []byte(pyContent), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("notes.txt", []byte("plain notes content"), 0644); err != nil {
		t.Fatal(err)
	}

	dscope.New(
		new(Module),
		modes.ForTest(t),
	).Fork(
		func() SkeletonFiles {
			return true
		},
	).Call(func(
		provider PartsProvider,
		countTokens generators.BPETokenCounter,
	) {
		parts, err := provider.Parts(math.MaxInt, countTokens, []string{"."})
		if err != nil {
			t.Fatal(err)
		}
		foundSkeleton := false
		foundFallback := false
		for _, part := range parts {
			text, ok := part.(generators.Text)
			if !ok {
				continue
			}
			s := string(text)
			if strings.Contains(s, "begin of skeleton of file widget.py") {
				foundSkeleton = true
				if !strings.Contains(s, "end of skeleton of file widget.py") {
					t.Fatalf("skeleton end marker must mirror the begin marker, got:\n%s", s)
				}
				if strings.Contains(s, "The content below is a structural skeleton") {
					t.Fatalf("skeleton body must not carry a hint that can be mistaken for file content, got:\n%s", s)
				}
				if !strings.Contains(s, "Widget") {
					t.Fatalf("skeleton must list the class, got:\n%s", s)
				}
				if strings.Contains(s, "secret_body_value") {
					t.Fatalf("skeleton must omit function bodies, got:\n%s", s)
				}
			}
			if strings.Contains(s, "plain notes content") {
				foundFallback = true
			}
		}
		if !foundSkeleton {
			t.Fatal("widget.py skeleton block not found")
		}
		if !foundFallback {
			t.Fatal("unsupported format must fall back to full content")
		}
	})
}
