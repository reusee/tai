package changes

import (
	"fmt"
	"strings"

	"github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
)

const TheoryOfTreeStructuredEdits = `
Tree-structured edits extend change blocks beyond Go files: a non-Go file
backed by a gotreesitter grammar (grammars.DetectLanguage) supports MODIFY,
ADD_BEFORE, ADD_AFTER, and DELETE with a specific target. The target is a
dotted path of outline symbols projected by the same Outliner that renders
structural skeletons in context, so the vocabulary a model sees is exactly
the vocabulary targets match against. Each segment is "Kind/Name" (the kind
string exactly as the skeleton shows it) or a bare "Name"; names match
case-insensitively. A single-segment target is searched across the whole
outline forest and must match exactly one definition; a multi-segment
target walks the path strictly. Zero matches, ambiguity, and unregistered
paths fail with errors that list the candidate definitions, giving the next
round a concrete correction target.

Edits apply at line boundaries through a single byte-range rewrite
(gotreesitter.Rewriter): everything outside the target's line span —
comments, blank lines, exact indentation — is preserved verbatim, and the
file is never re-serialized from the tree. When the target's first line is
indented and the body's first non-empty line is not, the anchor indentation
is applied to every non-empty body line, preserving the body's relative
structure for indentation-sensitive languages (Python, YAML). After the
rewrite the modified source is re-parsed: a file whose original tree was
error-free must stay error-free, so an edit that corrupts the syntax is
rejected before anything reaches the store. Go files are unaffected and
keep the go/ast path with goimports.
`

// maxOutlineHintSymbols caps the top-level definitions listed in a
// target-not-found error so a large file cannot flood the feedback message.
const maxOutlineHintSymbols = 8

// isTreeStructuredTarget reports whether the path is backed by a gotreesitter
// grammar, so structural change operations can address its definitions by
// outline path. Go files are excluded: they keep the go/ast path.
func isTreeStructuredTarget(path string) bool {
	if isGoFile(path) {
		return false
	}
	return grammars.DetectLanguage(path) != nil
}

// isTreeStructuredOperation reports whether the operation locates a specific
// definition in the file's parse tree. BEGIN, END, "package", "import", and
// DELETE * are file anchors or Go-only targets, not tree paths.
func isTreeStructuredOperation(op string, target string) bool {
	switch op {
	case "MODIFY", "ADD_BEFORE", "ADD_AFTER":
		return target != "" &&
			target != "BEGIN" && target != "END" &&
			target != "package" && target != "import"
	case "DELETE":
		return target != "" && target != "*" &&
			target != "BEGIN" && target != "END"
	}
	return false
}

// parseTreeForPath parses src with the grammar registered for path, bridging
// to the entry's host lexer when the language defines a token source factory.
func parseTreeForPath(path string, src []byte) (*gotreesitter.Tree, error) {
	entry := grammars.DetectLanguage(path)
	if entry == nil {
		return nil, fmt.Errorf("no grammar registered for %s", path)
	}
	language := entry.Language()
	if language == nil {
		return nil, fmt.Errorf("grammar for %s did not load", path)
	}
	parser := gotreesitter.NewParser(language)
	if entry.TokenSourceFactory != nil {
		tokenSource := entry.TokenSourceFactory(src, language)
		return parser.ParseWithTokenSource(src, tokenSource)
	}
	return parser.Parse(src)
}

// applyTreeStructuredEdit resolves the target against the file's outline,
// applies the operation to the target's line span, and re-parses the result
// for validity. See TheoryOfTreeStructuredEdits.
func applyTreeStructuredEdit(src []byte, path string, h ChangeBlock) ([]byte, error) {
	tree, err := parseTreeForPath(path, src)
	if err != nil {
		return nil, err
	}
	defer tree.Release()
	originalHasError := tree.RootNode().HasError()

	entry := grammars.DetectLanguage(path)
	language := entry.Language()
	outliner, err := gotreesitter.NewOutliner(
		language,
		grammars.ResolveTagsQuery(*entry),
		gotreesitter.WithOutlineOwnerRules(grammars.OutlineOwnerRules(*entry)),
	)
	if err != nil {
		return nil, err
	}
	symbols, report := outliner.OutlineTree(tree)
	if report.Declined() || len(symbols) == 0 {
		return nil, fmt.Errorf("file %s has no extractable outline to address targets by; use REPLACE with a unique find string, or WRITE", path)
	}

	match, err := resolveTreePathTarget(symbols, h.Target, path)
	if err != nil {
		return nil, err
	}

	body := strings.Trim(h.Body, "\r\n")
	if h.Op != "DELETE" && body == "" {
		return nil, fmt.Errorf("op %s requires a non-empty body", h.Op)
	}

	lineStart, lineEnd := targetLineSpan(src, match.symbol.Range.StartByte, match.symbol.Range.EndByte)
	line := src[lineStart:lineEnd]
	anchorIndent := string(line[:indentWidth(line)])

	rw := gotreesitter.NewRewriter(src)
	switch h.Op {
	case "MODIFY":
		rw.ReplaceRange(uint32(lineStart), uint32(lineEnd), []byte(preserveIndentation(anchorIndent, body)))
	case "DELETE":
		rw.ReplaceRange(uint32(lineStart), uint32(lineTerminatorEnd(src, lineEnd)), nil)
	case "ADD_BEFORE":
		rw.ReplaceRange(uint32(lineStart), uint32(lineStart), []byte(preserveIndentation(anchorIndent, body)+"\n"))
	case "ADD_AFTER":
		insertAt := lineTerminatorEnd(src, lineEnd)
		insertion := preserveIndentation(anchorIndent, body) + "\n"
		if insertAt > 0 && src[insertAt-1] != '\n' && src[insertAt-1] != '\r' {
			// The anchor line ends without a terminator (the file lacks a
			// trailing newline): open a fresh line instead of gluing the
			// body onto the anchor's last line.
			insertion = "\n" + insertion
		}
		rw.ReplaceRange(uint32(insertAt), uint32(insertAt), []byte(insertion))
	default:
		return nil, fmt.Errorf("op %q is not a tree-structured operation", h.Op)
	}
	newSrc, _, err := rw.Apply()
	if err != nil {
		return nil, err
	}

	// Differential validation: a file whose original tree parsed cleanly
	// must still parse cleanly after the edit, so a body that corrupts the
	// syntax is rejected before it reaches the store.
	if !originalHasError {
		newTree, err := parseTreeForPath(path, newSrc)
		if err != nil {
			return nil, fmt.Errorf("tree-structured edit of %s produced unparseable source: %w", path, err)
		}
		defer newTree.Release()
		if newTree.RootNode().HasError() {
			return nil, fmt.Errorf("tree-structured edit of %s produced source that no longer parses; check the body's completeness and indentation", path)
		}
	}
	return newSrc, nil
}

// treePathSegment is one element of a target path: an optional kind prefix
// restricting the outline kind, and the symbol name.
type treePathSegment struct {
	kind string
	name string
}

// treePathMatch is one resolved target together with the outline chain it
// was found through, rendered for error messages.
type treePathMatch struct {
	symbol gotreesitter.OutlineSymbol
	path   string
}

// parseTreePathTarget splits a dotted target into segments, each "Kind/Name"
// or a bare "Name" that matches any outline kind.
func parseTreePathTarget(target string) ([]treePathSegment, error) {
	var segments []treePathSegment
	for _, part := range strings.Split(target, ".") {
		part = strings.TrimSpace(part)
		if part == "" {
			return nil, fmt.Errorf("target %q has an empty path segment", target)
		}
		kind, name, hasKind := strings.Cut(part, "/")
		if !hasKind {
			segments = append(segments, treePathSegment{name: part})
			continue
		}
		kind = strings.TrimSpace(kind)
		name = strings.TrimSpace(name)
		if kind == "" || name == "" {
			return nil, fmt.Errorf("target segment %q must be Kind/Name or a bare Name", part)
		}
		segments = append(segments, treePathSegment{kind: kind, name: name})
	}
	return segments, nil
}

// segmentMatchesSymbol reports whether one target segment selects a symbol.
func segmentMatchesSymbol(segment treePathSegment, symbol gotreesitter.OutlineSymbol) bool {
	if !strings.EqualFold(segment.name, symbol.Name) {
		return false
	}
	return segment.kind == "" || strings.EqualFold(segment.kind, symbol.Kind)
}

// resolveTreePathTarget locates exactly one outline symbol for the target.
// A single-segment target is searched across the whole outline forest; a
// multi-segment target walks the path strictly. Both require a unique match.
func resolveTreePathTarget(symbols []gotreesitter.OutlineSymbol, target string, filePath string) (treePathMatch, error) {
	segments, err := parseTreePathTarget(target)
	if err != nil {
		return treePathMatch{}, err
	}
	var matches []treePathMatch
	if len(segments) == 1 {
		collectWholeForestMatches(symbols, segments[0], "", &matches)
	} else {
		collectStrictPathMatches(symbols, segments, 0, "", &matches)
	}
	switch len(matches) {
	case 1:
		return matches[0], nil
	case 0:
		return treePathMatch{}, fmt.Errorf("target %q not found in %s; top-level definitions: %s",
			target, filePath, outlineHint(symbols))
	default:
		rendered := make([]string, 0, min(len(matches), maxOutlineHintSymbols))
		for i, m := range matches {
			if i == maxOutlineHintSymbols {
				// Cap the list like outlineHint so a large file cannot
				// flood the feedback message; the full match count stays
				// in the message.
				rendered = append(rendered, fmt.Sprintf("... (+%d more)", len(matches)-i))
				break
			}
			rendered = append(rendered, m.path)
		}
		return treePathMatch{}, fmt.Errorf("target %q matches %d definitions in %s (%s); qualify the path with Kind/Name segments to disambiguate",
			target, len(matches), filePath, strings.Join(rendered, ", "))
	}
}

// collectStrictPathMatches walks the outline forest following the segments
// strictly, collecting the symbols the full path resolves to.
func collectStrictPathMatches(
	symbols []gotreesitter.OutlineSymbol,
	segments []treePathSegment,
	depth int,
	prefix string,
	matches *[]treePathMatch,
) {
	for _, symbol := range symbols {
		path := renderOutlinePath(symbol, prefix)
		if !segmentMatchesSymbol(segments[depth], symbol) {
			continue
		}
		if depth == len(segments)-1 {
			*matches = append(*matches, treePathMatch{symbol: symbol, path: path})
			continue
		}
		collectStrictPathMatches(symbol.Children, segments, depth+1, path, matches)
	}
}

// collectWholeForestMatches searches every nesting level for one segment, so
// a bare name resolves when it is unique anywhere in the file.
func collectWholeForestMatches(
	symbols []gotreesitter.OutlineSymbol,
	segment treePathSegment,
	prefix string,
	matches *[]treePathMatch,
) {
	for _, symbol := range symbols {
		path := renderOutlinePath(symbol, prefix)
		if segmentMatchesSymbol(segment, symbol) {
			*matches = append(*matches, treePathMatch{symbol: symbol, path: path})
		}
		collectWholeForestMatches(symbol.Children, segment, path, matches)
	}
}

// renderOutlinePath renders one symbol as "Kind/Name", joined onto the
// ancestor chain given by prefix.
func renderOutlinePath(symbol gotreesitter.OutlineSymbol, prefix string) string {
	path := symbol.Kind + "/" + symbol.Name
	if prefix != "" {
		path = prefix + "." + path
	}
	return path
}

// outlineHint lists the top-level definitions for a target-not-found error.
func outlineHint(symbols []gotreesitter.OutlineSymbol) string {
	var names []string
	for i, symbol := range symbols {
		if i == maxOutlineHintSymbols {
			names = append(names, "...")
			break
		}
		names = append(names, symbol.Kind+"/"+symbol.Name)
	}
	return strings.Join(names, ", ")
}

// targetLineSpan widens the symbol's byte range to whole lines: lineStart is
// the first byte of the line containing the range start, lineEnd is the index
// just past the line's last content byte (excluding the line terminator).
func targetLineSpan(src []byte, startByte, endByte uint32) (lineStart, lineEnd int) {
	lineStart = int(startByte)
	for lineStart > 0 && src[lineStart-1] != '\n' {
		lineStart--
	}
	contentEnd := int(endByte) - 1
	for contentEnd >= lineStart && (src[contentEnd] == '\n' || src[contentEnd] == '\r') {
		contentEnd--
	}
	if contentEnd < lineStart {
		// Degenerate zero-width range: anchor at the line start.
		return lineStart, lineStart
	}
	return lineStart, contentEnd + 1
}

// lineTerminatorEnd returns the index just past the newline that terminates
// the line ending at lineEnd, handling CRLF; len(src) when the last line has
// no terminator.
func lineTerminatorEnd(src []byte, lineEnd int) int {
	if lineEnd < len(src) && src[lineEnd] == '\r' && lineEnd+1 < len(src) && src[lineEnd+1] == '\n' {
		return lineEnd + 2
	}
	if lineEnd < len(src) && src[lineEnd] == '\n' {
		return lineEnd + 1
	}
	return lineEnd
}

// indentWidth returns the length of the leading run of spaces and tabs.
func indentWidth(line []byte) int {
	for i, b := range line {
		if b != ' ' && b != '\t' {
			return i
		}
	}
	return len(line)
}

// preserveIndentation applies the anchor indentation to the body when the
// body's first non-empty line is not indented, preserving the body's
// relative structure for indentation-sensitive languages. An empty anchor
// indentation or an already-indented body is returned unchanged.
func preserveIndentation(anchorIndent string, body string) string {
	if anchorIndent == "" {
		return body
	}
	for _, line := range strings.Split(body, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if line != strings.TrimLeft(line, " \t") {
			return body
		}
		break
	}
	lines := strings.Split(body, "\n")
	for i, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		lines[i] = anchorIndent + line
	}
	return strings.Join(lines, "\n")
}
