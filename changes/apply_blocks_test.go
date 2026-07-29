package changes

import (
	"os"
	"strings"
	"testing"

	"github.com/reusee/dscope"
	"github.com/reusee/tai/blocks"
	"github.com/reusee/tai/configs"
	"github.com/reusee/tai/modes"
)

// newTestScope creates a dscope scope for testing the changes package.
// It is shared across all changes test files via Go's package-level
// test visibility. Tests use .Call(func(deps...) { ... }) to resolve
// dscope-provided types, following the pattern established in
// anytexts.TestContextPrompt.
func newTestScope(t *testing.T) dscope.Scope {
	t.Helper()
	loader := configs.NewLoader(nil, configs.LoaderConfig{})
	return dscope.New(
		modes.ForTest(t),
		&loader,
		new(Module),
	)
}

func TestApplyChangeBlocks(t *testing.T) {
	newTestScope(t).Call(func(applyChangeBlocks ApplyChangeBlocks) {
		dir := t.TempDir()
		root, err := os.OpenRoot(dir)
		if err != nil {
			t.Fatal(err)
		}
		defer root.Close()

		original := "package x\n\nfunc Old() {}\n"
		if err := root.WriteFile("test.go", []byte(original), 0644); err != nil {
			t.Fatal(err)
		}

		changeBlocks := []blocks.Block{
			{
				Kind:       "change",
				Boundary:   "徕珑",
				Attributes: map[string]string{"op": "MODIFY", "target": "Old", "file-path": "test.go"},
				Body:       "func New() {}",
			},
		}

		if err := applyChangeBlocks(changeBlocks, root); err != nil {
			t.Fatalf("ApplyChangeBlocks failed: %v", err)
		}

		result, err := root.ReadFile("test.go")
		if err != nil {
			t.Fatal(err)
		}
		resultStr := string(result)
		if strings.Contains(resultStr, "Old") {
			t.Fatalf("result should not contain Old:\n%s", resultStr)
		}
		if !strings.Contains(resultStr, "func New() {}") {
			t.Fatalf("result should contain New:\n%s", resultStr)
		}
	})
}

func TestApplyChangeBlocksUnparseable(t *testing.T) {
	newTestScope(t).Call(func(applyChangeBlocks ApplyChangeBlocks) {
		dir := t.TempDir()
		root, err := os.OpenRoot(dir)
		if err != nil {
			t.Fatal(err)
		}
		defer root.Close()

		// A change block missing the required "op" attribute is unparseable.
		changeBlocks := []blocks.Block{
			{
				Kind:       "change",
				Boundary:   "徕珑",
				Attributes: map[string]string{"target": "Foo", "file-path": "test.go"},
				Body:       "func Foo() {}",
			},
		}

		err = applyChangeBlocks(changeBlocks, root)
		if err == nil {
			t.Fatal("expected error for unparseable change block")
		}
		if !strings.Contains(err.Error(), "unparseable") {
			t.Fatalf("expected unparseable error, got: %v", err)
		}
	})
}

func TestApplyChangeBlocksApplyError(t *testing.T) {
	newTestScope(t).Call(func(applyChangeBlocks ApplyChangeBlocks) {
		dir := t.TempDir()
		root, err := os.OpenRoot(dir)
		if err != nil {
			t.Fatal(err)
		}
		defer root.Close()

		changeBlocks := []blocks.Block{
			{
				Kind:       "change",
				Boundary:   "徕珑",
				Attributes: map[string]string{"op": "WRITE", "file-path": "../../../etc/passwd"},
				Body:       "content",
			},
		}

		err = applyChangeBlocks(changeBlocks, root)
		if err == nil {
			t.Fatal("expected error for path escape")
		}
		if !strings.Contains(err.Error(), "apply change block") {
			t.Fatalf("expected apply change block error, got: %v", err)
		}
	})
}
