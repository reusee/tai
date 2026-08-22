package gocodes

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRenderGoSrcPackageDocFlags verifies the go doc flag selection:
// both focus and context packages get -all -cmd, and only a focus
// package adds -u to include unexported symbols.
func TestRenderGoSrcPackageDocFlags(t *testing.T) {
	dir := t.TempDir()
	write := func(relPath, content string) {
		t.Helper()
		path := filepath.Join(dir, relPath)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", "module example.com/docflags\n\ngo 1.22\n")
	write("docflags/docflags.go", `package docflags

// docmarkerzebra marks ExportedConst.
const ExportedConst = 1

// hiddenFunc is an unexported declaration.
func hiddenFunc() {}
`)

	pkgPath := "example.com/docflags/docflags"

	focusDoc, err := renderGoSrcPackageDoc(pkgPath, true, dir, os.Environ())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(focusDoc, "docmarkerzebra") {
		t.Errorf("focus doc should include declaration docs via -all, got:\n%s", focusDoc)
	}
	if !strings.Contains(focusDoc, "hiddenFunc") {
		t.Errorf("focus doc should include unexported symbols via -u, got:\n%s", focusDoc)
	}

	contextDoc, err := renderGoSrcPackageDoc(pkgPath, false, dir, os.Environ())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(contextDoc, "docmarkerzebra") {
		t.Errorf("context doc should include declaration docs via -all, got:\n%s", contextDoc)
	}
	if strings.Contains(contextDoc, "hiddenFunc") {
		t.Errorf("context doc should exclude unexported symbols, got:\n%s", contextDoc)
	}
}
