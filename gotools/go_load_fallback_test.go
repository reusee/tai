package gotools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/reusee/dscope"
	"github.com/reusee/tai/generators"
	"github.com/reusee/tai/modes"
)

const goLoadFallbackValidMain = "package main\n\nfunc main() {}\n"

// goLoadFallbackBrokenPkg carries an invalid package clause: the package load
// fails at the package-clause scan, before any declaration parsing. A
// function-body syntax error is invisible to go list, so a fallback fixture
// must fail at a level the loader sees. See TheoryOfGoLoadFallback.
const goLoadFallbackBrokenPkg = "packag main\n\n// go-load-fallback-marker\n"

// goLoadFallbackBrokenEmbed declares an embed pattern with no matching file,
// a second independent load-level error: the trigger stays robust across
// toolchain error reporting. See TheoryOfGoLoadFallback.
const goLoadFallbackBrokenEmbed = "package main\n\nimport _ \"embed\"\n\n//go:embed go-load-fallback-missing.txt\nvar goLoadFallbackMissing string\n"

const goLoadFallbackModulePath = "example.com/loadfallback"

// goLoadFallbackModule creates a module whose main.go holds mainGo and
// returns its directory.
func goLoadFallbackModule(t *testing.T, mainGo string) string {
	t.Helper()
	t.Setenv("GOWORK", "")
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module "+goLoadFallbackModulePath+"\n\ngo 1.21\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(mainGo), 0644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// goLoadFallbackBreak makes the module's package load fail; the companion
// repair removes the failure.
func goLoadFallbackBreak(t *testing.T, dir string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "broken.go"), []byte(goLoadFallbackBrokenPkg), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "embed.go"), []byte(goLoadFallbackBrokenEmbed), 0644); err != nil {
		t.Fatal(err)
	}
}

func goLoadFallbackRepair(t *testing.T, dir string) {
	t.Helper()
	if err := os.Remove(filepath.Join(dir, "broken.go")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, "embed.go")); err != nil {
		t.Fatal(err)
	}
}

// goLoadFallbackScope builds one loop's scope: the module graph with the load
// directory pinned and the working directory inside the module, so the
// raw-file fallback walks the project. The working directory is restored on
// cleanup. See TheoryOfGoLoadFallback.
func goLoadFallbackScope(t *testing.T, dir string) dscope.Scope {
	t.Helper()
	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		os.Chdir(oldWd)
	})
	return dscope.New(
		modes.ForTest(t),
		new(Module),
	).Fork(
		func() LoadDir {
			return LoadDir(dir)
		},
	)
}

// goLoadFallbackProvider resolves the context provider from a scope the way
// the generation pipeline does.
func goLoadFallbackProvider(scope dscope.Scope) (provider PartsProvider) {
	scope.Call(func(p PartsProvider) {
		provider = p
	})
	return
}

func goLoadFallbackParts(t *testing.T, provider PartsProvider) []generators.Part {
	t.Helper()
	parts, err := provider.Parts(1<<20, generators.DeepseekTokenCounterFn, nil)
	if err != nil {
		t.Fatal(err)
	}
	return parts
}

func goLoadFallbackHas(parts []generators.Part, sub string) bool {
	for _, part := range parts {
		if text, ok := part.(generators.Text); ok && strings.Contains(string(text), sub) {
			return true
		}
	}
	return false
}

func TestGoLoadFallbackRedecidesPerScope(t *testing.T) {
	dir := goLoadFallbackModule(t, goLoadFallbackValidMain)

	// Each loop resolves the provider from a scope reset per loop, so the
	// probe re-runs the package load against the current filesystem state:
	// a loop that fixes the build returns to the Go context, a loop that
	// breaks it drops back to the raw context. See TheoryOfGoLoadFallback.
	scope := goLoadFallbackScope(t, dir)
	provider := goLoadFallbackProvider(scope)

	// A loading project: the Go context, with the focus package's
	// declaration surface.
	parts := goLoadFallbackParts(t, provider)
	if goLoadFallbackHas(parts, "begin of load error") {
		t.Fatal("a loading project must serve the Go context")
	}
	if !goLoadFallbackHas(parts, "begin of focus package") {
		t.Fatal("a loading project must serve the focus package documentation")
	}

	// A broken build: the next loop serves the raw-file context, led by the
	// load error note carrying the toolchain diagnostics. The toolchain's
	// choice of failing file varies, so the assertion binds the diagnostic
	// shape — a file position — not a specific file name.
	goLoadFallbackBreak(t, dir)
	scope = scope.Reset()
	provider = goLoadFallbackProvider(scope)
	parts = goLoadFallbackParts(t, provider)
	note, ok := parts[0].(generators.Text)
	if !ok || !strings.Contains(string(note), "begin of load error") {
		t.Fatalf("the fallback must lead with the load error note, got %#v", parts[0])
	}
	if noteText := string(note); !strings.Contains(noteText, ".go:") {
		t.Fatalf("the note must carry the load diagnostics naming the failing file, got %q", noteText)
	}
	if !goLoadFallbackHas(parts, "go-load-fallback-marker") {
		t.Fatal("the fallback must serve the raw file content")
	}
	if goLoadFallbackHas(parts, "begin of focus package") {
		t.Fatal("a broken build cannot serve the focus package documentation")
	}

	// A repaired build: the next loop returns to the Go context.
	goLoadFallbackRepair(t, dir)
	scope = scope.Reset()
	provider = goLoadFallbackProvider(scope)
	parts = goLoadFallbackParts(t, provider)
	if goLoadFallbackHas(parts, "begin of load error") {
		t.Fatal("a repaired build must return to the Go context")
	}
	if !goLoadFallbackHas(parts, "begin of focus package") {
		t.Fatal("a repaired build must serve the focus package documentation")
	}
}

func TestGoLoadFallbackChargesNoteAgainstBudget(t *testing.T) {
	dir := goLoadFallbackModule(t, goLoadFallbackValidMain)
	goLoadFallbackBreak(t, dir)
	provider := goLoadFallbackProvider(goLoadFallbackScope(t, dir))

	// The note is charged first and always served, so a budget consumed by
	// it still carries the load error while leaving no room for file
	// content. See TheoryOfGoLoadFallback.
	tiny, err := provider.Parts(1, generators.DeepseekTokenCounterFn, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !goLoadFallbackHas(tiny, "begin of load error") {
		t.Fatal("the load error note must be served even under a tiny budget")
	}
	if goLoadFallbackHas(tiny, "go-load-fallback-marker") {
		t.Fatal("a budget consumed by the note must leave no room for file content")
	}

	// A budget above the note's size serves the raw file content under the
	// remaining budget.
	if roomy := goLoadFallbackParts(t, provider); !goLoadFallbackHas(roomy, "go-load-fallback-marker") {
		t.Fatal("the raw file content must follow the note under the remaining budget")
	}
}

func TestGoLoadFallbackPropagatesNonLoadErrors(t *testing.T) {
	dir := goLoadFallbackModule(t, goLoadFallbackValidMain)

	// An existing in-module directory with no Go files: go doc fails on it,
	// an error after the load succeeded. It must propagate — substituting
	// raw files for such a fault would hide it instead of surfacing it in
	// the retry feedback. See TheoryOfGoLoadFallback.
	emptyDir := filepath.Join(dir, "empty")
	if err := os.MkdirAll(emptyDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(emptyDir, "README.md"), []byte("# empty\n"), 0644); err != nil {
		t.Fatal(err)
	}
	scope := goLoadFallbackScope(t, dir).Fork(
		func() DocPatterns {
			return DocPatterns{goLoadFallbackModulePath + "/empty"}
		},
	)
	provider := goLoadFallbackProvider(scope)
	_, err := provider.Parts(1<<20, generators.DeepseekTokenCounterFn, nil)
	if err == nil {
		t.Fatal("an error after a successful load must propagate, not fall back to raw files")
	}
	if !strings.Contains(err.Error(), "go doc") {
		t.Fatalf("expected the go doc failure to propagate, got %v", err)
	}
}
