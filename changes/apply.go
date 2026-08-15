package changes

import (
	"bytes"
	"cmp"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"slices"
	"strings"
)

const TheoryOfChangeBlockApplication = `
Change block application translates parsed change blocks into byte-level edits
on source files. When an ADD operation targets a spec nested inside a
multi-spec declaration block (e.g., const or var groups), the insertion point
redirects to the parent block boundary to avoid producing invalid code inside
the parentheses. The inserted body must remain a complete, self-contained
declaration so the resulting source is valid Go.

When the body lacks a declaration keyword (e.g., "foo = 1" instead of "const
foo = 1"), getBodyInfo parses it by prepending the keyword, and
ApplyChangeBlockStore strips the full prefix. For ADD_BEFORE and ADD_AFTER
operations, the keyword must be re-added after findTargetRange returns,
because the body is inserted as a standalone declaration outside the target's
syntactic context. MODIFY operations handle the keyword separately inside
findTargetRange and are unaffected.

WRITE bypasses declaration-level parsing and replaces the entire file content.
Go files are still processed through goimports to keep imports synchronized
after full replacement. DELETE with target * removes the entire file from the
working tree, bypassing declaration-level parsing. This works for both Go and
non-Go files. If the file does not exist, the operation is a no-op, consistent
with the DELETE declaration behavior that returns nil when the target is not
found.

Text-level operations (REPLACE, INSERT_BEFORE, INSERT_AFTER) are rejected for
Go files at the application layer because the model cannot reliably reproduce
whitespace in find strings; structural operations must be used instead. See
TheoryOfTextLevelOperations.

Final output normalization ensures every written file ends with exactly one
trailing newline, matching the convention enforced by go fmt.

After building the modified Go source, parseAndFormat parses it immediately to
catch syntax errors before goimports, which may report formatting-aware errors
that obscure the root cause. On parse or goimports failure, an XML error log
is written to the current directory recording the original source, change
block, modified content, and error. See TheoryOfErrorLogging.

Package detection (hasPackage) skips leading comments, including build
constraint comments such as //go:build and // +build, to determine whether the
source already contains a package clause.
`

type BodyInfo struct {
	Decls     []ast.Decl
	Specs     []ast.Spec
	Fset      *token.FileSet
	PrefixLen int
	Src       []byte
	Keyword   string // The keyword prepended, if any
}

func getBodyInfo(body string) (*BodyInfo, error) {
	if body == "" {
		return nil, nil
	}

	tryParse := func(b string) (*BodyInfo, error) {
		src := []byte(b)
		prefixLen := 0
		if !hasPackage(src) {
			prefix := "package p\n"
			src = append([]byte(prefix), src...)
			prefixLen = len(prefix)
		}
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, "", src, parser.ParseComments)
		if err != nil {
			// Try prepending keywords if parsing failed
			for _, kw := range []string{"const ", "var ", "type ", "func "} {
				trialPrefix := "package p\n" + kw
				trial := append([]byte(trialPrefix), []byte(b)...)
				f2, err2 := parser.ParseFile(fset, "", trial, parser.ParseComments)
				if err2 == nil {
					f = f2
					src = trial
					prefixLen = len(trialPrefix)
					err = nil
					// Extract keyword without trailing space
					kwStr := strings.TrimSpace(kw)
					info := &BodyInfo{
						Decls:     f.Decls,
						Fset:      fset,
						PrefixLen: prefixLen,
						Src:       src,
						Keyword:   kwStr,
					}
					for _, decl := range f.Decls {
						if g, ok := decl.(*ast.GenDecl); ok {
							info.Specs = append(info.Specs, g.Specs...)
						}
					}
					return info, nil
				}
			}
		}
		if err != nil {
			return nil, err
		}
		info := &BodyInfo{
			Decls:     f.Decls,
			Fset:      fset,
			PrefixLen: prefixLen,
			Src:       src,
		}
		for _, decl := range f.Decls {
			if g, ok := decl.(*ast.GenDecl); ok {
				info.Specs = append(info.Specs, g.Specs...)
			}
		}
		return info, nil
	}

	info, err := tryParse(body)
	if err == nil {
		return info, nil
	}

	// Try trimming trailing artifacts (like extra closing parenthesis) and retry
	trimmed := strings.TrimSpace(body)
	if strings.HasSuffix(trimmed, ")") {
		info2, err2 := tryParse(trimmed[:len(trimmed)-1])
		if err2 == nil {
			return info2, nil
		}
	}

	return nil, err
}

func (info *BodyInfo) entityCount() int {
	if info == nil {
		return 0
	}
	count := 0
	for _, d := range info.Decls {
		if _, ok := d.(*ast.FuncDecl); ok {
			count++
		} else if g, ok := d.(*ast.GenDecl); ok {
			count += len(g.Specs)
		}
	}
	return count
}

func (info *BodyInfo) extractEntitySource(target string) string {
	if info == nil {
		return ""
	}
	for _, decl := range info.Decls {
		node, _, match := matchDecl(decl, target)
		if match {
			start := info.Fset.Position(node.Pos()).Offset
			end := info.Fset.Position(node.End()).Offset
			return string(info.Src[start:end])
		}
	}
	// fallback: if exactly 1 entity, use its source even if name doesn't match perfectly
	if info.entityCount() == 1 {
		var node ast.Node
		if len(info.Specs) == 1 {
			node = info.Specs[0]
		} else if len(info.Decls) == 1 {
			node = info.Decls[0]
		}
		if node != nil {
			start := info.Fset.Position(node.Pos()).Offset
			end := info.Fset.Position(node.End()).Offset
			return string(info.Src[start:end])
		}
	}
	return ""
}

// finalizeContent ensures content ends with exactly one trailing newline,
// matching the convention enforced by go fmt.
func finalizeContent(content []byte) []byte {
	trimmed := bytes.TrimRight(content, "\r\n")
	if len(trimmed) == 0 {
		return nil
	}
	return append(trimmed, '\n')
}

// rangeItem represents a byte range in the original source to be replaced,
// deleted, or left as-is during the single-pass source rebuild.
type rangeItem struct {
	start, end int
	body       string
	isPrimary  bool
}

func buildRangeItems(
	start, end int,
	finalBody string,
	h ChangeBlock,
	bodyInfo *BodyInfo,
	f *ast.File,
	fset *token.FileSet,
	prefixLen int,
) []rangeItem {
	var items []rangeItem
	items = append(items, rangeItem{start: start, end: end, body: finalBody, isPrimary: true})

	// Detect and remove other occurrences of entities present in the
	// change block body to prevent duplication when a change block contains
	// multiple declarations (e.g. Type + Methods).
	if bodyInfo != nil && bodyInfo.entityCount() > 1 && f != nil && h.Target != "BEGIN" && h.Target != "END" {
		ids := getIdentifiers(bodyInfo)
		deleteRanges := buildDeleteRanges(fset, f, prefixLen)
		for _, id := range ids {
			if id == h.Target {
				continue
			}
			r, ok := deleteRanges[id]
			if !ok {
				continue
			}
			s, e := r[0], r[1]
			overlap := false
			for _, item := range items {
				if (s >= item.start && s < item.end) || (e > item.start && e <= item.end) || (item.start >= s && item.start < e) {
					overlap = true
					break
				}
			}
			if !overlap {
				items = append(items, rangeItem{start: s, end: e, body: "", isPrimary: false})
			}
		}
	}

	// Sort items by start offset ascending for the single-pass forward builder.
	slices.SortStableFunc(items, func(a, b rangeItem) int {
		return cmp.Compare(a.start, b.start)
	})

	// Only strip package prefix if the body might contain one.
	if f != nil && h.Target != "BEGIN" && h.Target != "END" {
		needStripPackage := bodyInfo == nil || bodyInfo.PrefixLen == 0
		for i := range items {
			if items[i].body != "" && needStripPackage {
				items[i].body = stripPackage(items[i].body)
			}
		}
	}

	return items
}

// buildModifiedSource builds the modified source in a single forward pass
// over the original source, applying each range item. Non-primary items are
// deletions (no content added). This is a pure function with no dscope
// dependencies.
func buildModifiedSource(src []byte, items []rangeItem, h ChangeBlock) []byte {
	newSrc := make([]byte, 0, len(src))
	pos := 0
	for _, item := range items {
		if item.start < pos {
			continue // skip overlapping edits
		}
		newSrc = append(newSrc, src[pos:item.start]...)
		if item.isPrimary {
			switch h.Op {
			case "MODIFY":
				body := item.body
				if h.Target == "BEGIN" && item.end < len(src) && !strings.HasSuffix(body, "\n") {
					body += "\n"
				}
				newSrc = append(newSrc, []byte(body)...)
			case "DELETE":
				// no content added
			case "ADD_BEFORE":
				newSrc = append(newSrc, []byte(item.body+"\n\n")...)
				newSrc = append(newSrc, src[item.start:item.end]...)
			case "ADD_AFTER":
				newSrc = append(newSrc, src[item.start:item.end]...)
				newSrc = append(newSrc, []byte("\n\n"+item.body)...)
			}
		}
		// Non-primary items are deletions: no content added.
		pos = item.end
	}
	newSrc = append(newSrc, src[pos:]...)
	return newSrc
}

func findTargetRange(fset *token.FileSet, f *ast.File, h ChangeBlock, bodyInfo *BodyInfo, fileSize int, prefixLen int) (int, int, string, error) {
	if h.Target == "BEGIN" {
		if h.Op == "MODIFY" {
			return 0, 0, h.Body, fmt.Errorf("cannot MODIFY with target BEGIN; use ADD_BEFORE")
		}
		if h.Op == "ADD_AFTER" && f != nil {
			// Find position after package declaration
			pos := max(fset.Position(f.Name.End()).Offset-prefixLen, 0)
			return pos, pos, h.Body, nil
		}
		return 0, 0, h.Body, nil
	}
	if h.Target == "END" {
		if h.Op == "MODIFY" {
			return 0, 0, h.Body, fmt.Errorf("cannot MODIFY with target END; use ADD_AFTER")
		}
		return fileSize, fileSize, h.Body, nil
	}
	// Special Go-only targets: package and import.
	// See TheoryOfSpecialGoTargets in parse.go.
	if h.Target == "package" {
		if h.Op != "MODIFY" {
			return 0, 0, h.Body, fmt.Errorf("target package only supports MODIFY, got %s", h.Op)
		}
		if f == nil {
			return 0, 0, h.Body, fmt.Errorf("file has no package clause to modify")
		}
		// The package clause spans from the file start ("package" keyword)
		// through the end of the package name.
		start := fset.Position(f.Pos()).Offset - prefixLen
		end := fset.Position(f.Name.End()).Offset - prefixLen
		return start, end, h.Body, nil
	}
	if h.Target == "import" {
		if h.Op != "MODIFY" {
			return 0, 0, h.Body, fmt.Errorf("target import only supports MODIFY, got %s", h.Op)
		}
		if f == nil {
			return 0, 0, h.Body, fmt.Errorf("file could not be parsed for import modification")
		}
		// Find all import declarations and determine their combined range.
		var start, end int
		found := false
		for _, decl := range f.Decls {
			if genDecl, ok := decl.(*ast.GenDecl); ok && genDecl.Tok == token.IMPORT {
				s := fset.Position(genDecl.Pos()).Offset - prefixLen
				e := fset.Position(genDecl.End()).Offset - prefixLen
				if !found || s < start {
					start = s
				}
				if !found || e > end {
					end = e
				}
				found = true
			}
		}
		if !found {
			// No existing imports; insert after package clause.
			pkgEnd := fset.Position(f.Name.End()).Offset - prefixLen
			start = pkgEnd
			end = pkgEnd
		}
		return start, end, h.Body, nil
	}
	if f == nil {
		return 0, 0, h.Body, fmt.Errorf("target %s not found", h.Target)
	}

	bodyKind := ""
	if bodyInfo != nil && bodyInfo.entityCount() > 0 {
		// Look for target's kind in body
		found := false
		for _, d := range bodyInfo.Decls {
			node, _, match := matchDecl(d, h.Target)
			if match {
				bodyKind = getDeclKind(node)
				found = true
				break
			}
		}
		if !found {
			// Fallback to first node
			var firstNode ast.Node
			if len(bodyInfo.Decls) > 0 {
				if g, ok := bodyInfo.Decls[0].(*ast.GenDecl); ok && len(g.Specs) > 0 {
					firstNode = g.Specs[0]
				} else {
					firstNode = bodyInfo.Decls[0]
				}
			}
			if firstNode != nil {
				bodyKind = getDeclKind(firstNode)
			}
		}
	}

	var candidateFound bool
	var candidateStart, candidateEnd int
	var candidateBody string

	for _, decl := range f.Decls {
		node, parent, match := matchDecl(decl, h.Target)
		if !match {
			continue
		}

		// Calculate ranges
		nodeStart := fset.Position(getActualPos(node)).Offset - prefixLen
		nodeEnd := fset.Position(node.End()).Offset - prefixLen
		parentStart := fset.Position(getActualPos(parent)).Offset - prefixLen
		parentEnd := fset.Position(parent.End()).Offset - prefixLen

		// Determine actual range and body to use
		var start, end int
		var finalBody string = h.Body

		if _, ok := node.(ast.Spec); ok {
			genDecl := parent.(*ast.GenDecl)

			// Heuristic: if MODIFY and body doesn't seem to contain the target declaration,
			// try to reconstruct it as a raw value replacement for const/var.
			if h.Op == "MODIFY" && (bodyInfo == nil || bodyInfo.entityCount() == 0 || getChangeBlockBodyNameFromInfo(bodyInfo) != h.Target) {
				isString := false
				if vs, ok := node.(*ast.ValueSpec); ok && len(vs.Values) > 0 {
					if bl, ok := vs.Values[0].(*ast.BasicLit); ok && bl.Kind == token.STRING {
						isString = true
					}
				}
				kw := ""
				switch genDecl.Tok {
				case token.CONST:
					kw = "const"
				case token.VAR:
					kw = "var"
				case token.TYPE:
					kw = "type"
				}
				if kw != "" {
					reconstructed := kw + " " + h.Target + " = "
					if isString {
						trimmedBody := strings.TrimSpace(h.Body)
						if !((strings.HasPrefix(trimmedBody, "`") && strings.HasSuffix(trimmedBody, "`")) ||
							(strings.HasPrefix(trimmedBody, `"`) && strings.HasSuffix(trimmedBody, `"`))) {
							reconstructed += "`" + h.Body + "`"
						} else {
							reconstructed += h.Body
						}
					} else {
						reconstructed += h.Body
					}
					newInfo, err := getBodyInfo(reconstructed)
					if err == nil && newInfo.entityCount() > 0 {
						finalBody = string(newInfo.Src[newInfo.PrefixLen:])
						bodyInfo = newInfo
					}
				}
			}

			// DELETE logic
			if h.Op == "DELETE" {
				if len(genDecl.Specs) > 1 {
					start, end = nodeStart, nodeEnd
				} else {
					start, end = parentStart, parentEnd
				}
			} else {
				// MODIFY logic
				if bodyInfo != nil && bodyInfo.entityCount() == 1 && len(genDecl.Specs) > 1 {
					// replace only the specific spec
					start, end = nodeStart, nodeEnd
					finalBody = bodyInfo.extractEntitySource(h.Target)
				} else {
					// replace whole block
					start, end = parentStart, parentEnd
					// Ensure keyword for single-spec GenDecl or block replacement
					if h.Op == "MODIFY" {
						kind := ""
						var tok token.Token
						switch genDecl.Tok {
						case token.CONST:
							kind = "const"
							tok = token.CONST
						case token.VAR:
							kind = "var"
							tok = token.VAR
						case token.TYPE:
							kind = "type"
							tok = token.TYPE
						}
						if kind != "" {
							// Reuse the already-parsed bodyInfo instead of calling
							// getBodyInfo(finalBody) again. bodyInfo.Keyword == ""
							// means the body was self-sufficient (no keyword prefix
							// needed during parsing), so it already contains the
							// keyword. If the heuristic updated bodyInfo above,
							// the updated value is used here.
							hasKeyword := false
							if bodyInfo != nil && bodyInfo.entityCount() > 0 {
								if bodyInfo.Keyword == "" {
									hasKeyword = true
								} else if gd, ok := bodyInfo.Decls[0].(*ast.GenDecl); ok && gd.Tok == tok {
									if bodyInfo.Fset.Position(gd.Pos()).Offset >= bodyInfo.PrefixLen {
										hasKeyword = true
									}
								}
							}
							if !hasKeyword {
								finalBody = kind + " " + finalBody
							}
						}
					}
				}
			}

			// For ADD operations targeting a spec inside a multi-spec GenDecl,
			// redirect to the parent GenDecl range to avoid inserting inside the
			// parentheses. The full body declaration must be used instead of the
			// extracted spec source, so the inserted code is valid Go. The keyword
			// is prepended uniformly in ApplyChangeBlockStore for all ADD paths.
			if (h.Op == "ADD_BEFORE" || h.Op == "ADD_AFTER") && len(genDecl.Specs) > 1 {
				if start == nodeStart && end == nodeEnd {
					start, end = parentStart, parentEnd
					finalBody = h.Body
				}
			}
		} else {
			// FuncDecl or simple GenDecl
			start, end = nodeStart, nodeEnd
			if h.Op == "MODIFY" {
				if _, ok := node.(*ast.FuncDecl); ok {
					// Reuse the already-parsed bodyInfo instead of calling
					// getBodyInfo(finalBody) again. See the spec branch above
					// for the rationale on Keyword == "".
					hasKeyword := false
					if bodyInfo != nil && bodyInfo.entityCount() > 0 {
						if bodyInfo.Keyword == "" {
							hasKeyword = true
						} else if _, ok := bodyInfo.Decls[0].(*ast.FuncDecl); ok {
							if bodyInfo.Fset.Position(bodyInfo.Decls[0].Pos()).Offset >= bodyInfo.PrefixLen {
								hasKeyword = true
							}
						}
					}
					if !hasKeyword {
						finalBody = "func " + finalBody
					}
				}
			}
		}

		if h.Op == "MODIFY" && bodyKind != "" {
			declKind := getDeclKind(parent)
			if declKind != bodyKind {
				if !candidateFound {
					candidateFound = true
					candidateStart, candidateEnd = start, end
					candidateBody = finalBody
				}
				continue
			}
		}
		return start, end, finalBody, nil
	}

	if candidateFound {
		return candidateStart, candidateEnd, candidateBody, nil
	}
	return 0, 0, h.Body, fmt.Errorf("target %s not found", h.Target)
}

// applyTextEdit applies a text-level operation (REPLACE, INSERT_BEFORE,
// INSERT_AFTER) to the file content. It searches for the find string,
// verifies it is unique (appears exactly once), and applies the edit
// relative to the found position. See TheoryOfTextLevelOperations.
//
// INSERT_BEFORE and INSERT_AFTER keep the inserted content on its own
// line(s): a newline separator is added automatically when the block body
// does not already carry one. Block bodies are trimmed of leading and
// trailing whitespace during parsing, so an emitted body never has a
// usable boundary newline; inserting it raw would merge the inserted line
// into the anchor line. This mirrors the Go structural ADD operations,
// which also separate inserted declarations with newlines.
// See TheoryOfTextLevelOperations.
func applyTextEdit(src []byte, h ChangeBlock) ([]byte, error) {
	find := h.Find
	if find == "" {
		return nil, fmt.Errorf("op %q requires a non-empty find attribute", h.Op)
	}

	content := string(src)
	count := strings.Count(content, find)
	if count == 0 {
		return nil, fmt.Errorf("find string not found in file %s", h.FilePath)
	}
	if count > 1 {
		return nil, fmt.Errorf("find string appears %d times in file %s; it must be unique; use WRITE for full file replacement", count, h.FilePath)
	}

	idx := strings.Index(content, find)

	switch h.Op {
	case "REPLACE":
		content = content[:idx] + h.Body + content[idx+len(find):]
	case "INSERT_BEFORE":
		body := h.Body
		if body != "" && !strings.HasSuffix(body, "\n") {
			body += "\n"
		}
		content = content[:idx] + body + content[idx:]
	case "INSERT_AFTER":
		body := h.Body
		if body != "" && !strings.HasPrefix(body, "\n") {
			body = "\n" + body
		}
		content = content[:idx+len(find)] + body + content[idx+len(find):]
	default:
		return nil, fmt.Errorf("unknown text-level operation: %s", h.Op)
	}

	return []byte(content), nil
}

func matchDecl(decl ast.Decl, target string) (ast.Node, ast.Decl, bool) {
	switch d := decl.(type) {
	case *ast.FuncDecl:
		funcName := d.Name.Name
		possible := []string{funcName}
		if d.Recv != nil && len(d.Recv.List) > 0 {
			recv := d.Recv.List[0].Type
			if star, ok := recv.(*ast.StarExpr); ok {
				recv = star.X
			}
			if ident, ok := recv.(*ast.Ident); ok {
				// Both value and pointer forms are valid for matching;
				// go allows calling pointer methods on values and vice versa.
				possible = append(possible, ident.Name+"."+funcName)
				possible = append(possible, "*"+ident.Name+"."+funcName)
			}
		}
		if slices.Contains(possible, target) {
			return d, d, true
		}
	case *ast.GenDecl:
		if d.Tok == token.IMPORT && target == "IMPORT" {
			return d, d, true
		}
		for _, spec := range d.Specs {
			switch s := spec.(type) {
			case *ast.TypeSpec:
				if s.Name.Name == target {
					return s, d, true
				}
			case *ast.ValueSpec:
				for _, n := range s.Names {
					if n.Name == target {
						return s, d, true
					}
				}
			}
		}
	}
	return nil, nil, false
}

// buildDeleteRanges builds a map from declaration name to the byte range
// that would be removed by a DELETE operation. This allows the duplicate
// detection in ApplyChangeBlock to look up ranges in O(1) per identifier instead
// of calling findTargetRange (O(D) per identifier) for each one.
// The range logic mirrors findTargetRange's DELETE path: for a spec in a
// multi-spec GenDecl, only the spec range is returned; for a single-spec
// GenDecl, the entire GenDecl range is returned.
func buildDeleteRanges(fset *token.FileSet, f *ast.File, prefixLen int) map[string][2]int {
	ranges := make(map[string][2]int)
	if f == nil {
		return ranges
	}
	for _, decl := range f.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			start := fset.Position(getActualPos(d)).Offset - prefixLen
			end := fset.Position(d.End()).Offset - prefixLen
			r := [2]int{start, end}
			ranges[d.Name.Name] = r
			if d.Recv != nil && len(d.Recv.List) > 0 {
				recv := d.Recv.List[0].Type
				if star, ok := recv.(*ast.StarExpr); ok {
					recv = star.X
				}
				if ident, ok := recv.(*ast.Ident); ok {
					ranges[ident.Name+"."+d.Name.Name] = r
					ranges["*"+ident.Name+"."+d.Name.Name] = r
				}
			}
		case *ast.GenDecl:
			if d.Tok == token.IMPORT {
				continue
			}
			if len(d.Specs) > 1 {
				for _, spec := range d.Specs {
					var names []string
					var node ast.Node
					switch s := spec.(type) {
					case *ast.TypeSpec:
						names = []string{s.Name.Name}
						node = s
					case *ast.ValueSpec:
						for _, n := range s.Names {
							names = append(names, n.Name)
						}
						node = s
					}
					if node == nil {
						continue
					}
					start := fset.Position(getActualPos(node)).Offset - prefixLen
					end := fset.Position(node.End()).Offset - prefixLen
					r := [2]int{start, end}
					for _, n := range names {
						ranges[n] = r
					}
				}
			} else if len(d.Specs) == 1 {
				var names []string
				switch s := d.Specs[0].(type) {
				case *ast.TypeSpec:
					names = []string{s.Name.Name}
				case *ast.ValueSpec:
					for _, n := range s.Names {
						names = append(names, n.Name)
					}
				}
				if len(names) == 0 {
					continue
				}
				start := fset.Position(getActualPos(d)).Offset - prefixLen
				end := fset.Position(d.End()).Offset - prefixLen
				r := [2]int{start, end}
				for _, n := range names {
					ranges[n] = r
				}
			}
		}
	}
	return ranges
}

// getChangeBlockBodyNameFromInfo extracts the primary entity name from a parsed
// BodyInfo without re-parsing the body. Callers that already hold a BodyInfo
// (e.g., ApplyChangeBlock, findTargetRange) should use this instead of
// getChangeBlockBodyName to avoid redundant AST parsing.
func getChangeBlockBodyNameFromInfo(info *BodyInfo) string {
	if info == nil || info.entityCount() == 0 {
		return ""
	}
	for _, d := range info.Decls {
		var name string
		if fn, ok := d.(*ast.FuncDecl); ok {
			name = fn.Name.Name
			if fn.Recv != nil && len(fn.Recv.List) > 0 {
				recv := fn.Recv.List[0].Type
				// Use pointer form if the receiver is a pointer
				if star, ok := recv.(*ast.StarExpr); ok {
					recv = star.X
					if ident, ok := recv.(*ast.Ident); ok {
						name = "*" + ident.Name + "." + name
					}
				} else if ident, ok := recv.(*ast.Ident); ok {
					name = ident.Name + "." + name
				}
			}
		} else if g, ok := d.(*ast.GenDecl); ok && len(g.Specs) > 0 {
			spec := g.Specs[0]
			if g.Tok == token.IMPORT {
				name = "IMPORT"
			} else if ts, ok := spec.(*ast.TypeSpec); ok {
				name = ts.Name.Name
			} else if vs, ok := spec.(*ast.ValueSpec); ok {
				name = vs.Names[0].Name
			}
		}
		if name != "" {
			return name
		}
	}
	return ""
}

func getIdentifiers(info *BodyInfo) []string {
	var ids []string
	if info == nil {
		return nil
	}
	for _, decl := range info.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			funcName := d.Name.Name
			if d.Recv != nil && len(d.Recv.List) > 0 {
				recv := d.Recv.List[0].Type
				if star, ok := recv.(*ast.StarExpr); ok {
					recv = star.X
					if ident, ok := recv.(*ast.Ident); ok {
						ids = append(ids, "*"+ident.Name+"."+funcName)
						// The non-pointer form is still useful to detect conflicts
						ids = append(ids, ident.Name+"."+funcName)
						continue
					}
				} else if ident, ok := recv.(*ast.Ident); ok {
					ids = append(ids, ident.Name+"."+funcName)
					ids = append(ids, "*"+ident.Name+"."+funcName)
					continue
				}
			}
			ids = append(ids, funcName)
		case *ast.GenDecl:
			if d.Tok == token.IMPORT {
				ids = append(ids, "IMPORT")
				continue
			}
			for _, spec := range d.Specs {
				switch s := spec.(type) {
				case *ast.TypeSpec:
					ids = append(ids, s.Name.Name)
				case *ast.ValueSpec:
					for _, n := range s.Names {
						ids = append(ids, n.Name)
					}
				}
			}
		}
	}
	return ids
}

func getDeclKind(node ast.Node) string {
	switch n := node.(type) {
	case *ast.FuncDecl:
		if n.Recv != nil && len(n.Recv.List) > 0 {
			return "method"
		}
		return "function"
	case *ast.GenDecl:
		if n.Tok == token.IMPORT {
			return "import"
		}
		if len(n.Specs) == 0 {
			return ""
		}
		switch n.Specs[0].(type) {
		case *ast.TypeSpec:
			return "type"
		case *ast.ValueSpec:
			if n.Tok == token.VAR {
				return "var"
			}
			if n.Tok == token.CONST {
				return "const"
			}
		}
	case *ast.TypeSpec:
		return "type"
	case *ast.ValueSpec:
		return "var" // context independent, parent GenDecl check is needed for const
	}
	return ""
}

func parseGoSource(fset *token.FileSet, filename string, src []byte) (*ast.File, int, error) {
	f, err := parser.ParseFile(fset, filename, src, parser.ParseComments)
	if err == nil {
		return f, 0, nil
	}
	if !hasPackage(src) {
		prefix := "package p\n"
		newSrc := append([]byte(prefix), src...)
		f, err = parser.ParseFile(fset, filename, newSrc, parser.ParseComments)
		if err == nil {
			return f, len(prefix), nil
		}
	}
	return nil, 0, err
}

// hasPackage reports whether the source contains a package clause, skipping
// leading whitespace and comments (including build constraint comments such
// as //go:build and // +build) that may precede the package declaration.
// Without skipping comments, a file whose package clause is preceded by a
// build constraint would be misdetected as lacking a package, causing a
// synthetic "package p" prefix to be prepended and producing a file with
// two package declarations that fails to parse.
func hasPackage(src []byte) bool {
	trimmed := bytes.TrimLeft(src, " \t\n\r")
	for {
		if bytes.HasPrefix(trimmed, []byte("//")) {
			// Skip a line comment.
			idx := bytes.IndexByte(trimmed, '\n')
			if idx == -1 {
				return false
			}
			trimmed = bytes.TrimLeft(trimmed[idx+1:], " \t\n\r")
		} else if bytes.HasPrefix(trimmed, []byte("/*")) {
			// Skip a block comment.
			idx := bytes.Index(trimmed, []byte("*/"))
			if idx == -1 {
				return false
			}
			trimmed = bytes.TrimLeft(trimmed[idx+2:], " \t\n\r")
		} else {
			break
		}
	}
	return bytes.HasPrefix(trimmed, []byte("package "))
}

func stripPackage(body string) string {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "", body, parser.ParseComments)
	if err != nil || len(f.Decls) == 0 {
		return body
	}
	firstDecl := f.Decls[0]
	startPos := getActualPos(firstDecl)
	offset := fset.Position(startPos).Offset
	return strings.TrimSpace(body[offset:])
}

func getActualPos(node ast.Node) token.Pos {
	switch n := node.(type) {
	case *ast.FuncDecl:
		if n.Doc != nil {
			return n.Doc.Pos()
		}
	case *ast.GenDecl:
		if n.Doc != nil {
			return n.Doc.Pos()
		}
	case *ast.TypeSpec:
		if n.Doc != nil {
			return n.Doc.Pos()
		}
	case *ast.ValueSpec:
		if n.Doc != nil {
			return n.Doc.Pos()
		}
	}
	return node.Pos()
}
