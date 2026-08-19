package changes

import (
	"encoding/xml"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/reusee/dscope"
	"github.com/reusee/tai/modes"
)

func TestWriteErrorLogWritesXML(t *testing.T) {
	dir := t.TempDir()

	dscope.New(
		modes.ForTest(t),
		new(Module),
	).Fork(
		func() ErrorLogDir { return ErrorLogDir(dir) },
	).Call(func(
		writeErrorLog WriteErrorLog,
	) {
		ctx := ErrorLogContext{
			Operation:    "MODIFY",
			Target:       "Foo",
			FilePath:     "test.go",
			ChangeBlock:  "func Foo() { bar(",
			SourceFile:   "package x\n\nfunc Foo() {}\n",
			ModifiedFile: "func Foo() { bar(\n",
			Error:        "parse error: expected )",
		}

		if err := writeErrorLog(ctx); err != nil {
			t.Fatal(err)
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
	})
}

func TestErrorLogDirDefaultsToWorkingDir(t *testing.T) {
	dscope.New(
		modes.ForTest(t),
		new(Module),
	).Call(func(
		dir ErrorLogDir,
	) {
		cwd, err := os.Getwd()
		if err != nil {
			t.Fatal(err)
		}
		if string(dir) != cwd {
			t.Fatalf("expected %q, got %q", cwd, string(dir))
		}
	})
}

func TestWriteErrorLogHandlesSameSecondCollisions(t *testing.T) {
	dir := t.TempDir()

	dscope.New(
		modes.ForTest(t),
		new(Module),
	).Fork(
		func() ErrorLogDir { return ErrorLogDir(dir) },
	).Call(func(
		writeErrorLog WriteErrorLog,
	) {
		ctx := ErrorLogContext{
			Operation: "WRITE",
			FilePath:  "test.go",
			Error:     "error 1",
		}

		if err := writeErrorLog(ctx); err != nil {
			t.Fatal(err)
		}
		if err := writeErrorLog(ctx); err != nil {
			t.Fatal(err)
		}

		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		count := 0
		for _, e := range entries {
			if strings.HasPrefix(e.Name(), ".error-log.") && strings.HasSuffix(e.Name(), ".xml") {
				count++
			}
		}
		if count != 2 {
			t.Fatalf("expected 2 error log files, got %d", count)
		}
	})
}

func TestApplyChangeBlockParseErrorWritesErrorLog(t *testing.T) {
	rootDir := t.TempDir()

	root, err := os.OpenRoot(rootDir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()

	newTestScope(t).Call(func(applyChangeBlock ApplyChangeBlock, errorLogDir ErrorLogDir) {
		original := "package x\n\nfunc Foo() {}\n"
		if err := root.WriteFile("test.go", []byte(original), 0644); err != nil {
			t.Fatal(err)
		}

		h := ChangeBlock{
			Op:       "MODIFY",
			Target:   "Foo",
			FilePath: "test.go",
			Body:     "Foo() { bar(",
		}
		err := applyChangeBlock(root, h)
		if err == nil {
			t.Fatal("expected error for invalid Go body")
		}
		if !strings.Contains(err.Error(), "parse error") {
			t.Fatalf("expected parse error, got: %v", err)
		}

		entry := findAndParseErrorLog(t, string(errorLogDir))
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
	})
}

func TestApplyChangeBlockWriteParseErrorWritesErrorLog(t *testing.T) {
	rootDir := t.TempDir()

	root, err := os.OpenRoot(rootDir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()

	newTestScope(t).Call(func(applyChangeBlock ApplyChangeBlock, errorLogDir ErrorLogDir) {
		h := ChangeBlock{
			Op:       "WRITE",
			FilePath: "test.go",
			Body:     "package x\n\nfunc Foo() { bar(\n",
		}
		err := applyChangeBlock(root, h)
		if err == nil {
			t.Fatal("expected error for invalid Go WRITE body")
		}
		if !strings.Contains(err.Error(), "parse error") {
			t.Fatalf("expected parse error, got: %v", err)
		}

		entry := findAndParseErrorLog(t, string(errorLogDir))
		if entry.Operation != "WRITE" {
			t.Fatalf("expected operation WRITE, got %q", entry.Operation)
		}
		if !strings.Contains(entry.ModifiedFile, "bar(") {
			t.Fatalf("modified file should contain the invalid body: %q", entry.ModifiedFile)
		}
		if entry.Error == "" {
			t.Fatal("error message should not be empty")
		}
	})
}

type testErrorLogEntry struct {
	XMLName      xml.Name `xml:"error-log"`
	Timestamp    string   `xml:"timestamp"`
	Operation    string   `xml:"operation"`
	Target       string   `xml:"target"`
	FilePath     string   `xml:"file-path"`
	Find         string   `xml:"find,omitempty"`
	ChangeBlock  string   `xml:"change-block"`
	SourceFile   string   `xml:"source-file"`
	ModifiedFile string   `xml:"modified-file"`
	Error        string   `xml:"error"`
}

func findAndParseErrorLog(t *testing.T, dir string) testErrorLogEntry {
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
	var entry testErrorLogEntry
	if err := xml.Unmarshal(data, &entry); err != nil {
		t.Fatalf("failed to parse error log XML: %v", err)
	}
	return entry
}
