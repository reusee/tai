package anytexts

import (
	"strings"
	"testing"
)

// TestSkeletonMarkdownHeadings verifies that atx headings within the
// depth limit are extracted with their nesting, and a heading deeper
// than the limit is omitted. See TheoryOfContextSkeleton.
func TestSkeletonMarkdownHeadings(t *testing.T) {
	content := []byte("# Top\n\ntext\n\n## Section\n\nmore\n\n### Detail\n\ndetail\n\n#### Too Deep\n\nbottom\n")
	skeleton, ok := Skeleton("README.md", content)
	if !ok {
		t.Fatal("expected skeleton for markdown with headings")
	}
	for _, want := range []string{"Top", "  Section", "    Detail"} {
		if !strings.Contains(skeleton, want) {
			t.Errorf("skeleton must contain %q, got:\n%s", want, skeleton)
		}
	}
	if strings.Contains(skeleton, "Too Deep") {
		t.Errorf("skeleton must omit headings deeper than the limit, got:\n%s", skeleton)
	}
}

// TestSkeletonMarkdownSetextHeading verifies that an underlined (setext)
// heading enters the outline with its title only: the node text spans
// the title line and its underline, and the underline must not leak
// into the skeleton as an extra line. See TheoryOfContextSkeleton.
func TestSkeletonMarkdownSetextHeading(t *testing.T) {
	content := []byte("Title\n=====\n\ntext\n\nSubsection\n-----\nmore\n")
	skeleton, ok := Skeleton("README.md", content)
	if !ok {
		t.Fatal("expected skeleton for markdown with setext headings")
	}
	if !strings.Contains(skeleton, "Title") {
		t.Errorf("skeleton must contain the level-1 setext title, got:\n%s", skeleton)
	}
	if !strings.Contains(skeleton, "  Subsection") {
		t.Errorf("skeleton must contain the level-2 setext title indented one level, got:\n%s", skeleton)
	}
	for _, underline := range []string{"=====", "-----"} {
		if strings.Contains(skeleton, underline) {
			t.Errorf("skeleton must not contain the setext underline %q, got:\n%s", underline, skeleton)
		}
	}
}

// TestSkeletonMarkdownFencedHashIsNotHeading verifies that a "#" line
// inside a fenced code block does not enter the outline: the markdown
// grammar parses fences as code nodes, not headings. See
// TheoryOfContextSkeleton.
func TestSkeletonMarkdownFencedHashIsNotHeading(t *testing.T) {
	content := []byte("# Real\n\n```go\n# not a heading\n```\n")
	skeleton, ok := Skeleton("doc.md", content)
	if !ok {
		t.Fatal("expected skeleton")
	}
	if !strings.Contains(skeleton, "Real") {
		t.Errorf("skeleton must contain the real heading, got:\n%s", skeleton)
	}
	if strings.Contains(skeleton, "not a heading") {
		t.Errorf("fenced content must not enter the outline, got:\n%s", skeleton)
	}
}

// TestSkeletonUntitledMarkdownFallsBack verifies that a markdown file
// with no headings yields no skeleton, so the caller falls back to the
// name-only listing. See TheoryOfContextSkeleton.
func TestSkeletonUntitledMarkdownFallsBack(t *testing.T) {
	if _, ok := Skeleton("notes.md", []byte("plain text without headings\n")); ok {
		t.Error("untitled markdown must not produce a skeleton")
	}
}

// TestSkeletonUnsupportedExtension verifies the conservative default:
// paths no registered grammar recognizes return no skeleton.
func TestSkeletonUnsupportedExtension(t *testing.T) {
	if _, ok := Skeleton("notes.taiunknown", []byte("anything")); ok {
		t.Error("unregistered path must not produce a skeleton")
	}
}

// TestSkeletonDataFormatWithoutDefinitions verifies that a registered
// grammar whose tags query captures no definition-shaped structure yields
// no skeleton, so data files stay name-only. See TheoryOfContextSkeleton.
func TestSkeletonDataFormatWithoutDefinitions(t *testing.T) {
	if _, ok := Skeleton("config.json", []byte(`{"a":1}`)); ok {
		t.Error("data format without definitions must not produce a skeleton")
	}
}

// TestSkeletonMarkdownNoLineTruncation verifies that a long markdown
// outline is not truncated by line count: every heading within the depth
// limit is kept, so the skeleton preserves its global view. See
// TheoryOfContextSkeleton.
func TestSkeletonMarkdownNoLineTruncation(t *testing.T) {
	const sectionCount = 300
	var builder strings.Builder
	for i := 0; i < sectionCount; i++ {
		builder.WriteString("## Section\n\ntext\n\n")
	}
	skeleton, ok := Skeleton("README.md", []byte(builder.String()))
	if !ok {
		t.Fatal("expected skeleton for markdown with many headings")
	}
	if got := strings.Count(skeleton, "Section"); got < sectionCount {
		t.Errorf("skeleton must keep every heading within the depth limit, got %d of %d:\n%s", got, sectionCount, skeleton)
	}
}

// TestSkeletonPythonDefinitions verifies that a code language registered
// in gotreesitter is outlined generically: its functions and classes are
// captured through the grammar's tags query, nested one level per lexical
// containment. See TheoryOfContextSkeleton.
func TestSkeletonPythonDefinitions(t *testing.T) {
	content := []byte("import os\n\ndef handler(request):\n    return request\n\nclass Widget:\n    def render(self):\n        pass\n")
	skeleton, ok := Skeleton("app.py", content)
	if !ok {
		t.Fatal("expected skeleton for python with definitions")
	}
	for _, want := range []string{"handler", "Widget", "render"} {
		if !strings.Contains(skeleton, want) {
			t.Errorf("skeleton must contain %q, got:\n%s", want, skeleton)
		}
	}
}

// TestSkeletonDepthTruncation verifies that a generic outline is truncated
// by nesting depth, not by line count: definitions within the depth limit
// stay visible and deeper ones are omitted, so the outline keeps its
// global shape. See TheoryOfContextSkeleton.
func TestSkeletonDepthTruncation(t *testing.T) {
	content := []byte("class Widget:\n    def render(self):\n        def inner():\n            def deepest():\n                pass\n            return deepest\n        return inner\n")
	skeleton, ok := Skeleton("app.py", content)
	if !ok {
		t.Fatal("expected skeleton for python with nested definitions")
	}
	for _, want := range []string{"Widget", "render", "inner"} {
		if !strings.Contains(skeleton, want) {
			t.Errorf("skeleton must contain %q within the depth limit, got:\n%s", want, skeleton)
		}
	}
	if strings.Contains(skeleton, "deepest") {
		t.Errorf("skeleton must omit definitions deeper than the depth limit, got:\n%s", skeleton)
	}
}

// TestSkeletonSupported verifies registry-driven structural text
// detection: every registered grammar is supported, unknown extensions
// are not. See TheoryOfContextSkeleton.
func TestSkeletonSupported(t *testing.T) {
	for _, path := range []string{"README.md", "app.py", "go.mod"} {
		if !SkeletonSupported(path) {
			t.Errorf("expected %q to be skeleton-supported", path)
		}
	}
	for _, path := range []string{"notes.taiunknown", "notes.xyz"} {
		if SkeletonSupported(path) {
			t.Errorf("expected %q to not be skeleton-supported", path)
		}
	}
}
