package anytexts

import (
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/reusee/dscope"
	"github.com/reusee/tai/configs"
	"github.com/reusee/tai/generators"
	"github.com/reusee/tai/modes"
)

func TestContextPrompt(t *testing.T) {
	dscope.New(
		new(Module),
		new(configs.NewLoader(nil, configs.LoaderConfig{})),
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
		parts, err := provider.Parts(math.MaxInt, countTokens, []string{"."})
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
		new(configs.NewLoader(nil, configs.LoaderConfig{})),
		modes.ForTest(t),
	).Call(func(
		provider CodeProvider,
		countTokens generators.BPETokenCounter,
	) {
		parts, err := provider.Parts(math.MaxInt, countTokens, []string{"."})
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

func TestSymlinks(t *testing.T) {
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
			new(configs.NewLoader(nil, configs.LoaderConfig{})),
			modes.ForTest(t),
		).Call(func(
			provider CodeProvider,
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
			new(configs.NewLoader(nil, configs.LoaderConfig{})),
			modes.ForTest(t),
		).Call(func(
			provider CodeProvider,
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

		// Create an external directory with a file outside the current directory.
		externalDir := t.TempDir()
		if err := os.WriteFile(filepath.Join(externalDir, "external.txt"), []byte("external content"), 0644); err != nil {
			t.Fatal(err)
		}
		// Create a symlink in the current directory pointing to the external file.
		if err := os.Symlink(filepath.Join(externalDir, "external.txt"), "link.txt"); err != nil {
			t.Fatal(err)
		}

		dscope.New(
			new(Module),
			new(configs.NewLoader(nil, configs.LoaderConfig{})),
			modes.ForTest(t),
		).Call(func(
			provider CodeProvider,
			countTokens generators.BPETokenCounter,
		) {
			parts, err := provider.Parts(math.MaxInt, countTokens, []string{"link.txt"})
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, part := range parts {
				if text, ok := part.(generators.Text); ok {
					if strings.Contains(string(text), "external content") {
						found = true
						if !strings.Contains(string(text), "(read-only)") {
							t.Fatal("external symlink file should be marked as read-only")
						}
					}
				}
			}
			if !found {
				t.Fatal("external symlinked file content not found")
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
		externalDir := t.TempDir()
		if err := os.WriteFile(filepath.Join(externalDir, "nested.txt"), []byte("nested external content"), 0644); err != nil {
			t.Fatal(err)
		}
		// Create a symlink to the external directory.
		if err := os.Symlink(externalDir, "ext"); err != nil {
			t.Fatal(err)
		}

		dscope.New(
			new(Module),
			new(configs.NewLoader(nil, configs.LoaderConfig{})),
			modes.ForTest(t),
		).Call(func(
			provider CodeProvider,
			countTokens generators.BPETokenCounter,
		) {
			parts, err := provider.Parts(math.MaxInt, countTokens, []string{"ext"})
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, part := range parts {
				if text, ok := part.(generators.Text); ok {
					if strings.Contains(string(text), "nested external content") {
						found = true
						if !strings.Contains(string(text), "(read-only)") {
							t.Fatal("file under external symlink directory should be marked as read-only")
						}
					}
				}
			}
			if !found {
				t.Fatal("file under external symlink directory not found")
			}
		})
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
	// modification time and aaa.txt to a newer one. With the old modtime-
	// primary sort, zzz.txt would appear before aaa.txt. With path-based
	// sorting, aaa.txt should appear before zzz.txt regardless of timestamps.
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
		new(configs.NewLoader(nil, configs.LoaderConfig{})),
		modes.ForTest(t),
	).Call(func(
		provider CodeProvider,
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
		new(configs.NewLoader(nil, configs.LoaderConfig{})),
		modes.ForTest(t),
	).Call(func(
		provider CodeProvider,
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
		new(configs.NewLoader(nil, configs.LoaderConfig{})),
		modes.ForTest(t),
	).Fork(
		new(IncludeMimeTypes{
			"image/png": true,
		}),
	).Call(func(
		provider CodeProvider,
	) {
		// With DeepseekTokenCounterFn, the text file markers + content
		// ("``` begin of file a.txt\nhello\n``` end of file a.txt\n")
		// are ~52 runes * 0.3 = 15 tokens. The binary file markers are
		// ~65 runes * 0.3 = 19 tokens. With maxTokens=16, the text file
		// fits (15 <= 16) but the binary file markers push the total to
		// 34 > 16, so the binary file is skipped.
		// Before the fix, binary markers were not counted, so the binary
		// file would always be included regardless of the budget.
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
