package changes

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

const pythonTreeEditSample = `def alpha():
    return 1


def beta():
    def inner():
        return 2
    return inner()


def holder1():
    def save():
        return 10
    return save()


def holder2():
    def save():
        return 20
    return save()
`

func writePythonSample(t *testing.T, root *os.Root) {
	t.Helper()
	if err := root.WriteFile("sample.py", []byte(pythonTreeEditSample), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestApplyChangeBlockTreeStructuredEdits(t *testing.T) {
	newTestScope(t).Call(func(applyChangeBlock ApplyChangeBlock) {
		openRoot := func(t *testing.T) *os.Root {
			t.Helper()
			root, err := os.OpenRoot(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			return root
		}

		t.Run("ModifyTopLevelFunction", func(t *testing.T) {
			root := openRoot(t)
			defer root.Close()
			writePythonSample(t, root)

			h := ChangeBlock{
				Op:       "MODIFY",
				Target:   "alpha",
				FilePath: "sample.py",
				Body:     "def alpha():\n    return 42",
			}
			if err := applyChangeBlock(root, h); err != nil {
				t.Fatalf("ApplyChangeBlock failed: %v", err)
			}
			result, err := root.ReadFile("sample.py")
			if err != nil {
				t.Fatal(err)
			}
			resultStr := string(result)
			if !strings.Contains(resultStr, "return 42") {
				t.Fatalf("result should contain the modified body:\n%s", resultStr)
			}
			if strings.Contains(resultStr, "return 1\n") {
				t.Fatalf("result should not contain the old body:\n%s", resultStr)
			}
			if !strings.Contains(resultStr, "def beta():") {
				t.Fatalf("result should preserve beta:\n%s", resultStr)
			}
		})

		t.Run("AddAfterTopLevelFunction", func(t *testing.T) {
			root := openRoot(t)
			defer root.Close()
			writePythonSample(t, root)

			h := ChangeBlock{
				Op:       "ADD_AFTER",
				Target:   "alpha",
				FilePath: "sample.py",
				Body:     "def gamma():\n    return 3",
			}
			if err := applyChangeBlock(root, h); err != nil {
				t.Fatalf("ApplyChangeBlock failed: %v", err)
			}
			result, err := root.ReadFile("sample.py")
			if err != nil {
				t.Fatal(err)
			}
			resultStr := string(result)
			alphaIdx := strings.Index(resultStr, "def alpha():")
			gammaIdx := strings.Index(resultStr, "def gamma():")
			betaIdx := strings.Index(resultStr, "def beta():")
			if alphaIdx == -1 || gammaIdx == -1 || betaIdx == -1 {
				t.Fatalf("all functions should be present:\n%s", resultStr)
			}
			if !(alphaIdx < gammaIdx && gammaIdx < betaIdx) {
				t.Fatalf("gamma should sit between alpha and beta:\n%s", resultStr)
			}
		})

		t.Run("AddBeforeNestedPreservesIndentation", func(t *testing.T) {
			root := openRoot(t)
			defer root.Close()
			writePythonSample(t, root)

			h := ChangeBlock{
				Op:       "ADD_BEFORE",
				Target:   "beta.inner",
				FilePath: "sample.py",
				Body:     "def helper():\n    return 0",
			}
			if err := applyChangeBlock(root, h); err != nil {
				t.Fatalf("ApplyChangeBlock failed: %v", err)
			}
			result, err := root.ReadFile("sample.py")
			if err != nil {
				t.Fatal(err)
			}
			resultStr := string(result)
			if !strings.Contains(resultStr, "    def helper():\n        return 0") {
				t.Fatalf("helper should be inserted at the nested target's indentation:\n%s", resultStr)
			}
		})

		t.Run("ModifyNestedWithPath", func(t *testing.T) {
			root := openRoot(t)
			defer root.Close()
			writePythonSample(t, root)

			h := ChangeBlock{
				Op:       "MODIFY",
				Target:   "beta.inner",
				FilePath: "sample.py",
				Body:     "def inner():\n    return 42",
			}
			if err := applyChangeBlock(root, h); err != nil {
				t.Fatalf("ApplyChangeBlock failed: %v", err)
			}
			result, err := root.ReadFile("sample.py")
			if err != nil {
				t.Fatal(err)
			}
			resultStr := string(result)
			if !strings.Contains(resultStr, "return 42") {
				t.Fatalf("result should contain the modified body:\n%s", resultStr)
			}
			if strings.Contains(resultStr, "return 2\n") {
				t.Fatalf("result should not contain the old body:\n%s", resultStr)
			}
		})

		t.Run("DeleteTarget", func(t *testing.T) {
			root := openRoot(t)
			defer root.Close()
			writePythonSample(t, root)

			h := ChangeBlock{
				Op:       "DELETE",
				Target:   "alpha",
				FilePath: "sample.py",
			}
			if err := applyChangeBlock(root, h); err != nil {
				t.Fatalf("ApplyChangeBlock failed: %v", err)
			}
			result, err := root.ReadFile("sample.py")
			if err != nil {
				t.Fatal(err)
			}
			resultStr := string(result)
			if strings.Contains(resultStr, "def alpha():") {
				t.Fatalf("alpha should be deleted:\n%s", resultStr)
			}
			if !strings.Contains(resultStr, "def beta():") {
				t.Fatalf("beta should be preserved:\n%s", resultStr)
			}
		})

		t.Run("AmbiguousTarget", func(t *testing.T) {
			root := openRoot(t)
			defer root.Close()
			writePythonSample(t, root)

			h := ChangeBlock{
				Op:       "MODIFY",
				Target:   "save",
				FilePath: "sample.py",
				Body:     "def save():\n    return 30",
			}
			err := applyChangeBlock(root, h)
			if err == nil {
				t.Fatal("expected error for ambiguous target")
			}
			if !strings.Contains(err.Error(), "matches 2") {
				t.Fatalf("expected ambiguity error, got: %v", err)
			}
		})

		t.Run("DisambiguatedByPath", func(t *testing.T) {
			root := openRoot(t)
			defer root.Close()
			writePythonSample(t, root)

			h := ChangeBlock{
				Op:       "MODIFY",
				Target:   "holder2.save",
				FilePath: "sample.py",
				Body:     "def save():\n    return 21",
			}
			if err := applyChangeBlock(root, h); err != nil {
				t.Fatalf("ApplyChangeBlock failed: %v", err)
			}
			result, err := root.ReadFile("sample.py")
			if err != nil {
				t.Fatal(err)
			}
			resultStr := string(result)
			if !strings.Contains(resultStr, "return 21") {
				t.Fatalf("result should contain the modified body:\n%s", resultStr)
			}
			if !strings.Contains(resultStr, "return 10") {
				t.Fatalf("holder1's save should be untouched:\n%s", resultStr)
			}
			if strings.Count(resultStr, "def save():") != 2 {
				t.Fatalf("both save definitions should remain:\n%s", resultStr)
			}
		})

		t.Run("TargetNotFound", func(t *testing.T) {
			root := openRoot(t)
			defer root.Close()
			writePythonSample(t, root)

			h := ChangeBlock{
				Op:       "MODIFY",
				Target:   "missing",
				FilePath: "sample.py",
				Body:     "def missing():\n    pass",
			}
			err := applyChangeBlock(root, h)
			if err == nil {
				t.Fatal("expected error for missing target")
			}
			if !strings.Contains(err.Error(), "not found") {
				t.Fatalf("expected not-found error, got: %v", err)
			}
			if !strings.Contains(err.Error(), "top-level definitions") {
				t.Fatalf("error should list top-level definitions, got: %v", err)
			}
		})

		t.Run("ModifyBodyCorruptingSyntaxRejected", func(t *testing.T) {
			root := openRoot(t)
			defer root.Close()
			writePythonSample(t, root)

			h := ChangeBlock{
				Op:       "MODIFY",
				Target:   "alpha",
				FilePath: "sample.py",
				Body:     "def alpha(:\n    return (42",
			}
			err := applyChangeBlock(root, h)
			if err == nil {
				t.Fatal("expected error for a body that corrupts the syntax")
			}
			if !strings.Contains(err.Error(), "no longer parses") && !strings.Contains(err.Error(), "unparseable") {
				t.Fatalf("expected a re-parse validation error, got: %v", err)
			}
			result, err := root.ReadFile("sample.py")
			if err != nil {
				t.Fatal(err)
			}
			if string(result) != pythonTreeEditSample {
				t.Fatalf("the file must be unchanged after a rejected edit:\n%s", result)
			}
		})

		t.Run("AddAfterLastLineWithoutTrailingNewline", func(t *testing.T) {
			root := openRoot(t)
			defer root.Close()
			// The last line has no terminator: ADD_AFTER must open a fresh
			// line instead of gluing the body onto it.
			if err := root.WriteFile("sample.py", []byte("def alpha():\n    return 1"), 0644); err != nil {
				t.Fatal(err)
			}

			h := ChangeBlock{
				Op:       "ADD_AFTER",
				Target:   "alpha",
				FilePath: "sample.py",
				Body:     "def gamma():\n    return 3",
			}
			if err := applyChangeBlock(root, h); err != nil {
				t.Fatalf("ApplyChangeBlock failed: %v", err)
			}
			result, err := root.ReadFile("sample.py")
			if err != nil {
				t.Fatal(err)
			}
			resultStr := string(result)
			if !strings.Contains(resultStr, "return 1\ndef gamma():") {
				t.Fatalf("inserted body should start on a fresh line:\n%s", resultStr)
			}
			if strings.Contains(resultStr, "return 1def gamma()") {
				t.Fatalf("inserted body must not be glued onto the last line:\n%s", resultStr)
			}
		})

		t.Run("AmbiguityHintIsCapped", func(t *testing.T) {
			root := openRoot(t)
			defer root.Close()
			var sample strings.Builder
			for range maxOutlineHintSymbols + 2 {
				sample.WriteString("def same():\n    return 0\n\n")
			}
			if err := root.WriteFile("sample.py", []byte(sample.String()), 0644); err != nil {
				t.Fatal(err)
			}

			h := ChangeBlock{
				Op:       "MODIFY",
				Target:   "same",
				FilePath: "sample.py",
				Body:     "def same():\n    return 0",
			}
			err := applyChangeBlock(root, h)
			if err == nil {
				t.Fatal("expected error for ambiguous target")
			}
			if !strings.Contains(err.Error(), fmt.Sprintf("matches %d definitions", maxOutlineHintSymbols+2)) {
				t.Fatalf("expected the full match count in the error, got: %v", err)
			}
			if strings.Count(err.Error(), "/same") > maxOutlineHintSymbols {
				t.Fatalf("ambiguity hint should cap the listed paths, got: %v", err)
			}
			if !strings.Contains(err.Error(), "more)") {
				t.Fatalf("capped hint should mention the omitted count, got: %v", err)
			}
		})
	})
}

func TestValidateChangeBlockTreeStructuredTargets(t *testing.T) {
	t.Run("RegisteredFileAllowsStructuralOps", func(t *testing.T) {
		for _, op := range []string{"MODIFY", "ADD_BEFORE", "ADD_AFTER"} {
			h := ChangeBlock{Op: op, Target: "someHeading", FilePath: "notes.md", Body: "content"}
			if err := ValidateChangeBlock(h); err != nil {
				t.Fatalf("%s on a registered non-Go file should be allowed: %v", op, err)
			}
		}
		h := ChangeBlock{Op: "DELETE", Target: "someHeading", FilePath: "notes.md"}
		if err := ValidateChangeBlock(h); err != nil {
			t.Fatalf("DELETE by outline path should be allowed: %v", err)
		}
	})

	t.Run("UnregisteredFileRejectsStructuralOps", func(t *testing.T) {
		h := ChangeBlock{Op: "MODIFY", Target: "someDecl", FilePath: "data.zqx", Body: "content"}
		if err := ValidateChangeBlock(h); err == nil {
			t.Fatal("expected error for MODIFY on a file without a registered grammar")
		}
	})

	t.Run("SpecialTargetsStayGoOnly", func(t *testing.T) {
		h := ChangeBlock{Op: "MODIFY", Target: "package", FilePath: "notes.md", Body: "package x"}
		if err := ValidateChangeBlock(h); err == nil {
			t.Fatal("expected error for the Go-only package target on a non-Go file")
		}
	})

	t.Run("FileAnchorsAreNotTreePaths", func(t *testing.T) {
		h := ChangeBlock{Op: "DELETE", Target: "BEGIN", FilePath: "notes.md"}
		if err := ValidateChangeBlock(h); err == nil {
			t.Fatal("expected error for the BEGIN file anchor used as a non-Go tree target")
		}
	})
}
