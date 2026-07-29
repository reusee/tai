package changes

import (
	"encoding/xml"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/reusee/dscope"
	"github.com/reusee/tai/configs"
	"github.com/reusee/tai/debugs"
	"github.com/reusee/tai/modes"
)

func newTestApplyChangeBlockWithLogDir(t *testing.T, dir string) ApplyChangeBlock {
	t.Helper()
	loader := configs.NewLoader(nil, configs.LoaderConfig{})
	scope := dscope.New(
		modes.ForTest(t),
		&loader,
		new(Module),
	)
	scope = scope.Fork(func() debugs.ErrorLogDir {
		return debugs.ErrorLogDir(dir)
	})
	return dscope.Get[ApplyChangeBlock](scope)
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

func TestApplyChangeBlockParseErrorWritesErrorLog(t *testing.T) {
	dir := t.TempDir()

	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()

	applyChangeBlock := newTestApplyChangeBlockWithLogDir(t, dir)

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
	err = applyChangeBlock(root, h)
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
}

func TestApplyChangeBlockWriteParseErrorWritesErrorLog(t *testing.T) {
	dir := t.TempDir()

	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()

	applyChangeBlock := newTestApplyChangeBlockWithLogDir(t, dir)

	// WRITE with invalid Go body (missing closing parenthesis).
	h := ChangeBlock{
		Op:       "WRITE",
		FilePath: "test.go",
		Body:     "package x\n\nfunc Foo() { bar(\n",
	}
	err = applyChangeBlock(root, h)
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
