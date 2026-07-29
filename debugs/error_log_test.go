package debugs

import (
	"encoding/xml"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/reusee/dscope"
)

func TestWriteErrorLogWritesXML(t *testing.T) {
	dir := t.TempDir()

	dscope.New(
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
