package anytexts

import (
	"strings"

	"github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
)

const TheoryOfContextSkeleton = `
Structural text files enter the initial context as a parsed skeleton instead
of full content. The skeleton is extracted with the gotreesitter tree-sitter
runtime: the file is parsed by its language grammar and only top-level
structure is rendered — for markdown, the first heading levels (atx and
setext); for every other registered grammar, the language-neutral Outliner
projects the definition outline from the grammar's tags query. Detection and
outlining are both registry-driven, so every grammar gotreesitter ships —
and any grammar added to it later — is outlined without per-format code.
Code-fence content is inside fence nodes, not heading nodes, so a "#"
inside a fenced block never enters a markdown outline.

Truncation is by nesting depth, never by line count: the depth limit bounds
the outline while every top-level branch stays visible, so the summary keeps
its global shape even when deep detail is dropped; a line limit would cut
the outline mid-structure and lose that view.

Skeletons are summaries by construction: the model must treat them as
an index, not the source. Signaling lives in the block markers, not in
the body: a skeleton block's begin and end markers read "skeleton of
file <path>" instead of "file <path>", and the body carries no hint
text that could be mistaken for file content. The consumption rules —
treat the skeleton as an index; fetch the original with an ingest block
before modifying or fully understanding the file — live in the system
prompt (pipeline.SkeletonFilesSystemPrompt). Files explicitly specified
via -file patterns skip skeletons entirely: they are work targets the
user named, matching the -all-src semantics for Go focus files, so
their full content is provided as before.

Extraction is best-effort: an unregistered path, a parse failure,
or a file with no extractable structure yields no skeleton, and the
caller applies its own fallback — gotools' module-root listing keeps the
name-only entry, and anytexts' PartsProvider keeps the full content. The
skeleton is an enhancement, never a requirement.

Enablement is caller-selected: gotools' module-root listing always uses
skeletons, and the auto-detected default command outside a Go module
(AnyTextCommand) forks SkeletonFiles(true) so its initial context
carries skeletons for every supported file format; other
PartsProvider consumers (e.g., the ai command's -file attachments)
keep full text.
`

// skeletonMaxHeadingDepth is the heading depth limit of a markdown
// skeleton: headings deeper than this level are omitted so a deep
// outline cannot crowd the context budget.
const skeletonMaxHeadingDepth = 3

// skeletonMaxDefinitionDepth is the nesting-depth limit of a generic
// skeleton: definitions nested deeper than this level are omitted, so a
// deeply nested outline cannot crowd the context budget while every
// top-level branch stays visible. See TheoryOfContextSkeleton.
const skeletonMaxDefinitionDepth = 2

// Skeleton returns a compact structural summary of the file content, and
// whether a skeleton was extracted. Markdown files yield the heading
// outline; every other path registered in gotreesitter's grammar registry
// yields the definition outline of its language. Unsupported paths, parse
// failures, and files with no extractable structure return false, so the
// caller falls back to the name-only listing. See TheoryOfContextSkeleton.
func Skeleton(path string, content []byte) (string, bool) {
	if strings.HasSuffix(strings.ToLower(path), ".md") {
		return markdownSkeleton(content)
	}
	return genericSkeleton(path, content)
}

// SkeletonSupported reports whether the file path maps to a grammar
// registered in gotreesitter. Detection is registry-driven, so every
// grammar the library ships — and any grammar added to it later — is a
// structural text file automatically. Callers use it to decide whether a
// file belongs in a skeleton listing before paying for extraction. See
// TheoryOfContextSkeleton.
func SkeletonSupported(path string) bool {
	return grammars.DetectLanguage(path) != nil
}

// genericSkeleton parses content as the language registered for the file
// path and renders the definition outline: one line per definition the
// grammar's tags query captures, nested definitions indented one level.
// Languages without tags data, parse failures, and files with no captured
// definitions yield no skeleton, so the caller falls back to the
// name-only listing. See TheoryOfContextSkeleton.
func genericSkeleton(path string, content []byte) (string, bool) {
	entry := grammars.DetectLanguage(path)
	if entry == nil {
		return "", false
	}
	language := entry.Language()
	if language == nil {
		return "", false
	}

	parser := gotreesitter.NewParser(language)
	var tree *gotreesitter.Tree
	var err error
	if entry.TokenSourceFactory != nil {
		// Languages bridged to a host lexer (e.g., Go via go/scanner) cannot
		// be lexed by the DFA path; build their token source first.
		tokenSource := entry.TokenSourceFactory(content, language)
		tree, err = parser.ParseWithTokenSource(content, tokenSource)
	} else {
		tree, err = parser.Parse(content)
	}
	if err != nil || tree == nil || tree.RootNode() == nil {
		return "", false
	}
	defer tree.Release()

	outliner, err := gotreesitter.NewOutliner(
		language,
		grammars.ResolveTagsQuery(*entry),
		gotreesitter.WithOutlineOwnerRules(grammars.OutlineOwnerRules(*entry)),
	)
	if err != nil {
		return "", false
	}
	symbols, report := outliner.OutlineTree(tree)
	if report.Declined() || len(symbols) == 0 {
		return "", false
	}

	var lines []string
	renderOutlineSymbols(symbols, 0, &lines)
	if len(lines) == 0 {
		return "", false
	}
	return strings.Join(lines, "\n"), true
}

// renderOutlineSymbols renders one line per definition, nested definitions
// indented one level deeper, omitting definitions deeper than the depth
// limit so every top-level branch stays visible. See TheoryOfContextSkeleton.
func renderOutlineSymbols(symbols []gotreesitter.OutlineSymbol, depth int, lines *[]string) {
	if depth > skeletonMaxDefinitionDepth {
		return
	}
	for _, symbol := range symbols {
		*lines = append(*lines, strings.Repeat("  ", depth)+symbol.Kind+" "+symbol.Name)
		renderOutlineSymbols(symbol.Children, depth+1, lines)
	}
}

// markdownSkeleton parses content as markdown and renders the heading
// outline: one line per heading within the depth limit, indented by
// its level. The gotreesitter markdown grammar yields atx_heading
// nodes for "# Title" lines and setext_heading nodes for underlined
// titles; a "#" inside a fenced code block is not a heading node, so
// fenced content never enters the outline. See TheoryOfContextSkeleton.
func markdownSkeleton(content []byte) (string, bool) {
	entry := grammars.DetectLanguageByName("markdown")
	if entry == nil {
		return "", false
	}
	parser := gotreesitter.NewParser(entry.Language())
	tree, err := parser.Parse(content)
	if err != nil || tree == nil || tree.RootNode() == nil {
		return "", false
	}
	defer tree.Release()

	var lines []string
	gotreesitter.Walk(tree.RootNode(), func(node *gotreesitter.Node, depth int) gotreesitter.WalkAction {
		nodeType := node.Type(entry.Language())
		level := 0
		switch nodeType {
		case "atx_heading":
			for _, r := range node.Text(content) {
				if r == '#' {
					level++
				} else {
					break
				}
			}
		case "setext_heading":
			level = 1
		default:
			return gotreesitter.WalkContinue
		}
		if level == 0 || level > skeletonMaxHeadingDepth {
			return gotreesitter.WalkContinue
		}
		title := strings.TrimSpace(strings.TrimLeft(string(node.Text(content)), "#"))
		if title == "" {
			return gotreesitter.WalkContinue
		}
		lines = append(lines, strings.Repeat("  ", level-1)+title)
		return gotreesitter.WalkContinue
	})
	if len(lines) == 0 {
		return "", false
	}
	return strings.Join(lines, "\n"), true
}

// buildTextFilePart renders one text file as a model-facing context unit.
// When skeletonEnabled and the file was discovered during directory
// traversal, a parsed skeleton replaces the full content, and the begin
// and end markers read "skeleton of file <path>" so the model can tell
// summary from source; the body carries no hint text. Extraction failure
// or an unsupported format falls back to full text under the plain file
// marker. Directly matched files (-file patterns) are work targets the
// user named and always render full text. See TheoryOfContextSkeleton.
func buildTextFilePart(info FileInfo, skeletonEnabled bool) string {
	readOnlyNote := ""
	if info.ReadOnly {
		readOnlyNote = " (read-only)"
	}
	kind := "file"
	body := string(info.Content)
	if skeletonEnabled && !info.DirectMatch {
		if skeleton, ok := Skeleton(info.Path, info.Content); ok {
			kind = "skeleton of file"
			body = skeleton
		}
	}
	// The part ends with a blank line so consecutive units stay
	// paragraph-separated after verbatim part concatenation. See
	// generators.TheoryOfContentUnitSeparation.
	return "``` begin of " + kind + " " + info.Path + readOnlyNote + "\n" +
		body + "\n" +
		"``` end of " + kind + " " + info.Path + "\n\n"
}
