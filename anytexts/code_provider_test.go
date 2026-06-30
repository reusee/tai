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
}