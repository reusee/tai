package changes

import (
	"encoding/xml"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestApplyChangeBlockParseErrorWritesErrorLog(t *testing.T) {
	dir := t.TempDir()
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(origDir)
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()

	original := "package x\n\nfunc Foo() {}\n"
	if err := root.WriteFile("test.go", []byte(original), 0644); err != nil {
		t.Fatal(err)
	}

	// Body with a syntax error: missing closing parenthesis and brace.
	// getBodyInfo fails to parse it, so bodyInfo is nil and the raw body
	// is used. findTargetRange prepends "func ", producing invalid Go.
	// The parse check in parseAndFormat catches this before goimports.
	h := ChangeBlock{
		Op:       "MODIFY",
		Target:   "Foo",
		FilePath: "test.go",
		Body:     "Foo() { bar(",
	}
	err = ApplyChangeBlock(root, h)
	if err == nil {
		t.Fatal("expected error for invalid Go body")
	}
	if !strings.Contains(err.Error(), "parse error") {
		t.Fatalf("expected parse error, got: %v", err)
	}

	entry := findAndParseErrorLog(t, dir)
	if entry.Operation != "MODIFY" {
		t.Fatalf("expected operation MODIFY, got %q", entry.Operation)
	}
	if entry.Target != "Foo" {
		t.Fatalf("expected target Foo, got %q", entry.Target)
	}
	if !strings.Contains(entry.SourceFile, "func Foo() {}") {
		t.Fatalf("source file should contain original content: %q", entry.SourceFile)
	}
	if !strings.Contains(entry.ModifiedFile, "bar(") {
		t.Fatalf("modified file should contain the invalid body: %q", entry.ModifiedFile)
	}
	if entry.Error == "" {
		t.Fatal("error message should not be empty")
	}
	if _, err := time.Parse(time.RFC3339, entry.Timestamp); err != nil {
		t.Fatalf("invalid timestamp %q: %v", entry.Timestamp, err)
	}
}

func TestApplyChangeBlockWriteParseErrorWritesErrorLog(t *testing.T) {
	dir := t.TempDir()
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(origDir)
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()

	// WRITE with invalid Go body (missing closing parenthesis).
	h := ChangeBlock{
		Op:       "WRITE",
		FilePath: "test.go",
		Body:     "package x\n\nfunc Foo() { bar(\n",
	}
	err = ApplyChangeBlock(root, h)
	if err == nil {
		t.Fatal("expected error for invalid Go WRITE body")
	}
	if !strings.Contains(err.Error(), "parse error") {
		t.Fatalf("expected parse error, got: %v", err)
	}

	entry := findAndParseErrorLog(t, dir)
	if entry.Operation != "WRITE" {
		t.Fatalf("expected operation WRITE, got %q", entry.Operation)
	}
	if !strings.Contains(entry.ModifiedFile, "bar(") {
		t.Fatalf("modified file should contain the invalid body: %q", entry.ModifiedFile)
	}
	if entry.Error == "" {
		t.Fatal("error message should not be empty")
	}
}

// findAndParseErrorLog finds the .error-log.*.xml file in dir and parses it.
func findAndParseErrorLog(t *testing.T, dir string) errorLogEntry {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var logFile string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".error-log.") && strings.HasSuffix(e.Name(), ".xml") {
			logFile = filepath.Join(dir, e.Name())
			break
		}
	}
	if logFile == "" {
		t.Fatal("expected an error log XML file to be written")
	}
	data, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatal(err)
	}
	var entry errorLogEntry
	if err := xml.Unmarshal(data, &entry); err != nil {
		t.Fatalf("failed to parse error log XML: %v", err)
	}
	return entry
}
