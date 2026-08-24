package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/reusee/tai/generators"
)

func TestFilePathToPartsTextFile(t *testing.T) {
	dir := t.TempDir()
	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(oldWd)
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(dir, "test.txt")
	content := "hello world"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	parts, err := filePathToParts(path)
	if err != nil {
		t.Fatal(err)
	}

	if len(parts) != 1 {
		t.Fatalf("expected 1 part for text file, got %d", len(parts))
	}

	text, ok := parts[0].(generators.Text)
	if !ok {
		t.Fatalf("expected Text part, got %T", parts[0])
	}

	s := string(text)
	if !strings.Contains(s, "``` begin of file") {
		t.Fatal("text file missing begin marker")
	}
	if !strings.Contains(s, "``` end of file") {
		t.Fatal("text file missing end marker")
	}
	if !strings.Contains(s, content) {
		t.Fatal("text file missing content")
	}
	if !strings.Contains(s, path) {
		t.Fatal("text file missing path in marker")
	}
	if !strings.HasSuffix(s, "\n\n") {
		t.Fatal("file part must end with a blank line so consecutive parts stay paragraph-separated")
	}
}

func TestFilePathToPartsBinaryFile(t *testing.T) {
	dir := t.TempDir()
	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(oldWd)
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(dir, "test.png")
	binContent := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}
	if err := os.WriteFile(path, binContent, 0644); err != nil {
		t.Fatal(err)
	}

	parts, err := filePathToParts(path)
	if err != nil {
		t.Fatal(err)
	}

	if len(parts) != 3 {
		t.Fatalf("expected 3 parts for binary file, got %d", len(parts))
	}

	beginText, ok := parts[0].(generators.Text)
	if !ok {
		t.Fatalf("expected Text part for begin marker, got %T", parts[0])
	}
	if !strings.Contains(string(beginText), "``` begin of file") {
		t.Fatal("binary file missing begin marker")
	}
	if !strings.Contains(string(beginText), "binary") {
		t.Fatal("binary file marker should indicate binary type")
	}

	fileContent, ok := parts[1].(generators.FileContent)
	if !ok {
		t.Fatalf("expected FileContent part, got %T", parts[1])
	}
	if string(fileContent.Content) != string(binContent) {
		t.Fatal("binary file content mismatch")
	}

	endText, ok := parts[2].(generators.Text)
	if !ok {
		t.Fatalf("expected Text part for end marker, got %T", parts[2])
	}
	if !strings.Contains(string(endText), "``` end of file") {
		t.Fatal("binary file missing end marker")
	}
	if !strings.Contains(string(endText), path) {
		t.Fatal("binary file end marker missing path")
	}
}

func TestFilePathToPartsMarksOutsideWritableDirsAsReadOnly(t *testing.T) {
	// A file outside all writable directories (CWD, /tmp, Go dirs, config
	// dir, /dev/shm) should be marked as read-only rather than rejected.
	// /var/tmp is not in the writable dirs list, so a file there is marked
	// read-only. If /var/tmp is unavailable, the test is skipped.
	nonWritableDir, err := os.MkdirTemp("/var/tmp", "tai_test_")
	if err != nil {
		t.Skipf("cannot create temp dir in /var/tmp: %v", err)
	}
	defer os.RemoveAll(nonWritableDir)

	path := filepath.Join(nonWritableDir, "external.txt")
	content := "external content"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	parts, err := filePathToParts(path)
	if err != nil {
		t.Fatalf("expected no error for file outside writable directories, got: %v", err)
	}

	if len(parts) != 1 {
		t.Fatalf("expected 1 part for text file, got %d", len(parts))
	}

	text, ok := parts[0].(generators.Text)
	if !ok {
		t.Fatalf("expected Text part, got %T", parts[0])
	}

	s := string(text)
	if !strings.Contains(s, "(read-only)") {
		t.Fatal("text file outside writable dirs should be marked as read-only")
	}
	if !strings.Contains(s, content) {
		t.Fatal("text file missing content")
	}
}
