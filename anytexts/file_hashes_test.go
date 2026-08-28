package anytexts

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/reusee/tai/changes"
	"github.com/reusee/tai/logs"
)

func TestPartsProviderRecordsFileHashes(t *testing.T) {
	dir := t.TempDir()
	original, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(original) })

	content := "hello world\n"
	if err := os.WriteFile("a.txt", []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	hashes := changes.NewFileHashes()
	logger := logs.Logger{Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	provider := PartsProvider{
		FileNameOK:       func() FileNameOK { return func(name string) bool { return true } },
		NameMatch:        func() NameMatch { return func(string) bool { return true } },
		Logger:           func() logs.Logger { return logger },
		Debug:            func() Debug { return false },
		IncludeMimeTypes: func() IncludeMimeTypes { return nil },
		FileHashes:       func() *changes.FileHashes { return hashes },
	}

	parts, err := provider.Parts(1<<20, func(string) (int, error) { return 1, nil }, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(parts) == 0 {
		t.Fatal("expected the file to be included as a part")
	}

	key, err := filepath.Abs("a.txt")
	if err != nil {
		t.Fatal(err)
	}
	if !hashes.Has(key) {
		t.Fatalf("expected the file %s to be baselined during context assembly", key)
	}
	if err := hashes.Verify(key, []byte(content)); err != nil {
		t.Fatalf("the baseline must match the assembled content: %v", err)
	}
	if err := hashes.Verify(key, []byte("different")); err == nil {
		t.Fatal("different content must fail verification")
	}
}
