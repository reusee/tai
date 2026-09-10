package anytexts

import (
	"path"
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

Extensionless files — Makefile, Dockerfile, and the like — detect through
gotreesitter's linguist exact-filename match and parse cleanly, but their
grammars carry no tags query, so the definition outline is declined by
construction. When the outline is declined and the file's basename carries
no extension, the skeleton falls back to the parse tree's top-level
structure: one line per named root child, truncated to its first line and
capped by length, and only from an error-free parse. The extensionless gate
preserves the conservative contract for suffixed data formats (JSON and the
like), which stay name-only when their tags query yields nothing.

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

// skeletonMaxLineRunes caps one top-level structure line of the skeleton
// fallback, keeping each line an index entry rather than a source
// excerpt. See TheoryOfContextSkeleton.
const skeletonMaxLineRunes = 120

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
// When the outline is declined and the file is extensionless — the shape
// of Makefile and Dockerfile, which detect through linguist exact
// filenames and whose grammars carry no tags query — the skeleton falls
// back to the parse tree's top-level structure. Parse failures and files
// with no extractable structure yield no skeleton, so the caller falls
// back to the name-only listing. See TheoryOfContextSkeleton.
func genericSkeleton(path string, content []byte) (string, bool) {
	entry := grammars.DetectLanguage(path)
	if entry == nil {
		return "", false
	}
	language := entry.Language()
	if language == nil {
		return "", false
	}

	tree, err := parseSkeletonTree(entry, language, content)
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
	if !report.Declined() && len(symbols) > 0 {
		var lines []string
		renderOutlineSymbols(symbols, 0, &lines)
		if len(lines) > 0 {
			return strings.Join(lines, "\n"), true
		}
		return "", false
	}
	if report.Declined() && isExtensionlessPath(path) {
		return structureSkeleton(tree, content)
	}
	return "", false
}

// parseSkeletonTree parses content with the language's grammar, bridging
// to the entry's host lexer when the language defines a token source
// factory. It is the single parse path of the skeleton extraction.
func parseSkeletonTree(
	entry *grammars.LangEntry,
	language *gotreesitter.Language,
	content []byte,
) (*gotreesitter.Tree, error) {
	parser := gotreesitter.NewParser(language)
	if entry.TokenSourceFactory != nil {
		return parser.ParseWithTokenSource(content, entry.TokenSourceFactory(content, language))
	}
	return parser.Parse(content)
}

// structureSkeleton renders the parse tree's top-level structure as the
// skeleton fallback for languages whose tags query is empty: one line
// per named child of the root, truncated to its first line. Makefile
// and Dockerfile detect through linguist exact filenames, parse cleanly,
// and carry no tags query, so the outline is empty by construction and
// the top level is the only projection the grammar offers. Blank and
// anonymous nodes are skipped, and a tree holding a parse error yields
// no skeleton: a recovered error tree's top level may swallow content,
// so the caller keeps full text. See TheoryOfContextSkeleton.
func structureSkeleton(tree *gotreesitter.Tree, content []byte) (string, bool) {
	root := tree.RootNode()
	if root == nil || root.HasError() {
		return "", false
	}
	var lines []string
	for _, child := range root.Children() {
		if !child.IsNamed() {
			continue
		}
		line := truncateSkeletonLine(string(child.Text(content)))
		if line == "" {
			continue
		}
		lines = append(lines, line)
	}
	if len(lines) == 0 {
		return "", false
	}
	return strings.Join(lines, "\n"), true
}

// isExtensionlessPath reports whether the file's basename carries no
// extension. The top-level structure fallback applies only to these
// files: they have no other structural signal, and suffixed data formats
// whose tags query yields nothing keep the conservative name-only
// contract. See TheoryOfContextSkeleton.
func isExtensionlessPath(filePath string) bool {
	return !strings.Contains(path.Base(filePath), ".")
}

// truncateSkeletonLine reduces one top-level structure line to its first
// source line, trimmed, capped at skeletonMaxLineRunes so the skeleton
// stays an index rather than a source excerpt.
func truncateSkeletonLine(text string) string {
	line, _, _ := strings.Cut(text, "\n")
	line = strings.TrimSpace(line)
	runes := []rune(line)
	if len(runes) > skeletonMaxLineRunes {
		line = string(runes[:skeletonMaxLineRunes]) + "…"
	}
	return line
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
// fenced content never enters the outline. A setext heading node spans
// the title line and its underline: the underline determines the level
// ('=' level 1, '-' level 2, per CommonMark) and is kept out of the
// outline. See TheoryOfContextSkeleton.
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
		headingText := string(node.Text(content))
		titleLine, underline, hasUnderline := strings.Cut(headingText, "\n")
		level := 0
		switch nodeType {
		case "atx_heading":
			for _, r := range headingText {
				if r == '#' {
					level++
				} else {
					break
				}
			}
		case "setext_heading":
			// The node text spans the title line and its underline
			// (e.g. "Title\n====="). The underline sets the level —
			// '=' level 1, '-' level 2 — and never enters the outline.
			if hasUnderline {
				if trimmed := strings.TrimSpace(underline); trimmed != "" {
					if trimmed[0] == '-' {
						level = 2
					} else {
						level = 1
					}
				}
			}
		default:
			return gotreesitter.WalkContinue
		}
		if level == 0 || level > skeletonMaxHeadingDepth {
			return gotreesitter.WalkContinue
		}
		title := strings.TrimSpace(strings.TrimLeft(titleLine, "#"))
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
