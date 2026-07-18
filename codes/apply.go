package codes

import (
	"bytes"
	"cmp"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/reusee/tai/codes/codetypes"
	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/modernize"
	"golang.org/x/tools/imports"
)

const HunkApplicationTheory = `
Hunk application translates parsed change blocks into byte-level edits on source files.
When an ADD operation targets a spec nested inside a multi-spec declaration block (e.g.,
const or var groups), the insertion point redirects to the parent block boundary to avoid
producing invalid code inside the parentheses. The inserted body must remain a complete,
self-contained declaration so the resulting source is valid Go.
WRITE bypasses declaration-level parsing and replaces the entire file content. Go files
are still processed through goimports to keep imports synchronized after full replacement.
Final output normalization ensures every written file ends with exactly one trailing
newline, matching the convention enforced by go fmt. This replaces the prior use of
bytes.TrimSpace which stripped the trailing newline entirely.
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
		node, _, match := matchDecl(info.Fset, decl, target)
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
// matching the convention enforced by go fmt. goimports output already ends
// with a single '\n', but bytes.TrimSpace was stripping it, producing files
// that did not end with a newline — inconsistent with go fmt.
func finalizeContent(content []byte) []byte {
	trimmed := bytes.TrimRight(content, "\r\n")
	if len(trimmed) == 0 {
		return nil
	}
	return append(trimmed, '\n')
}

// pathEscapesDir reports whether a cleaned relative path escapes the current
// directory via parent-directory traversal. It distinguishes ".." (parent
// directory) and "../"-prefixed paths from names that merely start with two
// dots (e.g., "..hidden", "..."), which are valid directory or file names.
func pathEscapesDir(cleaned string) bool {
	return cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator))
}

func applyHunk(root *os.Root, h codetypes.Hunk) error {
	path := h.FilePath
	if filepath.IsAbs(path) { // Convert absolute path to relative if it is within CWD
		cwd, err := os.Getwd()
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(cwd, path)
		if err != nil || pathEscapesDir(rel) {
			return fmt.Errorf("path outside of current directory: %s", path)
		}
		path = rel
	}
	if pathEscapesDir(filepath.Clean(path)) { // Proactively block directory escape
		return fmt.Errorf("path escapes current directory: %s", path)
	}

	// Handle RENAME before any file content checks
	if h.Op == "RENAME" {
		newPath := h.Target
		if filepath.IsAbs(newPath) {
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			rel, err := filepath.Rel(cwd, newPath)
			if err != nil || pathEscapesDir(rel) {
				return fmt.Errorf("new path outside of current directory: %s", newPath)
			}
			newPath = rel
		}
		if pathEscapesDir(filepath.Clean(newPath)) {
			return fmt.Errorf("new path escapes current directory: %s", newPath)
		}
		if dir := filepath.Dir(newPath); dir != "." {
			if err := rootMkdirAll(root, dir, 0755); err != nil {
				return err
			}
		}
		return root.Rename(path, newPath)
	}

	// Handle WRITE: replace the entire file content, bypassing declaration-level parsing.
	// The target field is ignored; file-path determines the destination.
	// Go files are processed through goimports to keep imports synchronized.
	if h.Op == "WRITE" {
		if dir := filepath.Dir(path); dir != "." {
			if err := rootMkdirAll(root, dir, 0755); err != nil {
				return err
			}
		}
		content := []byte(h.Body)
		if strings.HasSuffix(path, ".go") {
			formatted, err := imports.Process(path, content, nil)
			if err != nil {
				return fmt.Errorf("goimports: %w", err)
			}
			content = formatted
		}
		return root.WriteFile(path, finalizeContent(content), 0644)
	}

	src, err := root.ReadFile(path) // Use os.Root for safe reading
	if err != nil && !os.IsNotExist(err) {
		return err
	}

	// Handle non-Go files
	if !strings.HasSuffix(path, ".go") {
		if os.IsNotExist(err) && h.Op == "ADD_BEFORE" && h.Target == "BEGIN" {
			// Allow creating new non-Go file
			body := h.Body
			if dir := filepath.Dir(path); dir != "." {
				if err := rootMkdirAll(root, dir, 0755); err != nil {
					return err
				}
			}
			return root.WriteFile(path, []byte(body), 0644)
		}
		return fmt.Errorf("only .go files are supported for modification: %s", path)
	}

	fset := token.NewFileSet()
	var f *ast.File
	var prefixLen int
	if len(src) > 0 {
		f, prefixLen, err = parseGoSource(fset, path, src)
		if err != nil {
			return err
		}
	}

	bodyInfo, _ := getBodyInfo(h.Body)
	if bodyInfo != nil {
		h.Body = string(bodyInfo.Src[bodyInfo.PrefixLen:])
	}
	bodyName := getHunkBodyNameFromInfo(bodyInfo)

	var start, end int
	var finalBody string = h.Body

	// Implementation of Theory: ADD_BEFORE/AFTER acts as MODIFY if name already exists
	if (h.Op == "ADD_BEFORE" || h.Op == "ADD_AFTER") && bodyName != "" {
		if s, e, fb, err := findTargetRange(fset, f, codetypes.Hunk{Op: "MODIFY", Target: bodyName, Body: h.Body}, bodyInfo, len(src), prefixLen); err == nil {
			h.Op = "MODIFY"
			h.Target = bodyName
			start, end, finalBody = s, e, fb
		}
	}

	// Resolve target range
	if start == 0 && end == 0 {
		var err error
		start, end, finalBody, err = findTargetRange(fset, f, h, bodyInfo, len(src), prefixLen)
		if err != nil {
			if h.Op == "MODIFY" || h.Op == "DELETE" {
				// Theory: MODIFY and DELETE have no effect if target is not found
				return nil
			}
			// ADD anchor missing: append to the end of file
			start, end = len(src), len(src)
		}
	}

	type rangeItem struct {
		start, end int
		body       string
		isPrimary  bool
	}
	var items []rangeItem
	items = append(items, rangeItem{start: start, end: end, body: finalBody, isPrimary: true})

	// Detect and remove other occurrences of entities present in the hunk body
	// to prevent duplication when a hunk contains multiple declarations (e.g. Type + Methods).
	if bodyInfo != nil && bodyInfo.entityCount() > 1 && f != nil && h.Target != "BEGIN" && h.Target != "END" {
		ids := getIdentifiers(bodyInfo)
		// Build a delete-range index in a single pass over declarations,
		// instead of calling findTargetRange (O(D)) for each identifier.
		deleteRanges := buildDeleteRanges(fset, f, prefixLen)
		for _, id := range ids {
			// Skip the primary target or anything that matches it
			if id == h.Target {
				continue
			}
			r, ok := deleteRanges[id]
			if !ok {
				continue
			}
			s, e := r[0], r[1]
			// Check for overlap with existing items
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

	// Only strip package prefix if the body might contain one. If bodyInfo
	// prepended a "package p\n" prefix (PrefixLen > 0), the body was already
	// stripped of any package declaration during parsing.
	if f != nil && h.Target != "BEGIN" && h.Target != "END" {
		needStripPackage := bodyInfo == nil || bodyInfo.PrefixLen == 0
		for i := range items {
			if items[i].body != "" && needStripPackage {
				items[i].body = stripPackage(items[i].body)
			}
		}
	}

	// Build the result in a single forward pass over the original source.
	// Items are non-overlapping (guaranteed by the overlap check above) and
	// sorted ascending by start, so each edit operates on a distinct range.
	// This avoids repeated O(n) slice copies that the previous end-to-start
	// in-place append approach incurred for each item.
	newSrc := make([]byte, 0, len(src))
	pos := 0
	for _, item := range items {
		if item.start < pos {
			continue // skip overlapping edits (shouldn't happen due to overlap check)
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

	outputSrc := newSrc
	outputPrefixLen := 0
	if !hasPackage(newSrc) {
		outputSrc = append([]byte("package p\n"), newSrc...)
		outputPrefixLen = len("package p\n")
	}
	formatted, err := imports.Process(path, outputSrc, nil)
	if err != nil {
		return fmt.Errorf("goimports: %w", err)
	}
	if outputPrefixLen > 0 {
		formatted = formatted[outputPrefixLen:]
	}

	if dir := filepath.Dir(path); dir != "." {
		if err := rootMkdirAll(root, dir, 0755); err != nil {
			return err
		}
	}
	if err := root.WriteFile(path, finalizeContent(formatted), 0644); err != nil {
		return err
	}
	if strings.HasSuffix(path, ".go") {
		optimizeGoFile(root, path)
	}
	return nil
}

func rootMkdirAll(root *os.Root, path string, perm os.FileMode) error {
	path = filepath.Clean(path)
	if path == "." || path == "/" || path == "" {
		return nil
	}
	err := root.Mkdir(path, perm) // Try creating directly
	if err == nil || os.IsExist(err) {
		return nil
	}
	parent := filepath.Dir(path)
	if parent != path {
		if err := rootMkdirAll(root, parent, perm); err != nil {
			return err
		}
	}
	return root.Mkdir(path, perm)
}

func findTargetRange(fset *token.FileSet, f *ast.File, h codetypes.Hunk, bodyInfo *BodyInfo, fileSize int, prefixLen int) (int, int, string, error) {
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
	if f == nil {
		return 0, 0, h.Body, fmt.Errorf("target %s not found", h.Target)
	}

	bodyKind := ""
	if bodyInfo != nil && bodyInfo.entityCount() > 0 {
		// Look for target's kind in body
		found := false
		for _, d := range bodyInfo.Decls {
			node, _, match := matchDecl(bodyInfo.Fset, d, h.Target)
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
		node, parent, match := matchDecl(fset, decl, h.Target)
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
			if h.Op == "MODIFY" && (bodyInfo == nil || bodyInfo.entityCount() == 0 || getHunkBodyNameFromInfo(bodyInfo) != h.Target) {
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
							// keyword. If heuristic updated bodyInfo above, the
							// updated value is used here, matching the prior
							// semantics of re-parsing finalBody.
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
			// parentheses. The full body declaration (with keyword) must be used
			// instead of the extracted spec source, so the inserted code is valid Go.
			if (h.Op == "ADD_BEFORE" || h.Op == "ADD_AFTER") && len(genDecl.Specs) > 1 {
				if start == nodeStart && end == nodeEnd {
					start, end = parentStart, parentEnd
					finalBody = h.Body
					if bodyInfo != nil && bodyInfo.Keyword != "" {
						finalBody = bodyInfo.Keyword + " " + finalBody
					}
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

func matchDecl(fset *token.FileSet, decl ast.Decl, target string) (ast.Node, ast.Decl, bool) {
	switch d := decl.(type) {
	case *ast.FuncDecl:
		funcName := d.Name.Name
		possible := []string{funcName}
		if d.Recv != nil && len(d.Recv.List) > 0 {
			recv := d.Recv.List[0].Type
			isPtr := false
			if star, ok := recv.(*ast.StarExpr); ok {
				recv = star.X
				isPtr = true
			}
			if ident, ok := recv.(*ast.Ident); ok {
				// Both value and pointer forms are valid for matching;
				// go allows calling pointer methods on values and vice versa.
				possible = append(possible, ident.Name+"."+funcName)
				possible = append(possible, "*"+ident.Name+"."+funcName)
				_ = isPtr
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
// detection in applyHunk to look up ranges in O(1) per identifier instead
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

// getHunkBodyNameFromInfo extracts the primary entity name from a parsed
// BodyInfo without re-parsing the body. Callers that already hold a BodyInfo
// (e.g., applyHunk, findTargetRange) should use this instead of
// getHunkBodyName to avoid redundant AST parsing.
func getHunkBodyNameFromInfo(info *BodyInfo) string {
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

func getHunkBodyName(body string) string {
	info, err := getBodyInfo(body)
	if err != nil || info == nil {
		return ""
	}
	return getHunkBodyNameFromInfo(info)
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

func hasPackage(src []byte) bool {
	trimmed := bytes.TrimLeft(src, " \t\n\r")
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

// optimizeGoFile runs modernize analyzers on the given Go file and applies
// suggested fixes. It is called after each hunk is applied to keep the
// codebase modernized incrementally. Errors during optimization are logged
// to stderr but do not cause the apply to fail, because the code is already
// valid and the optimization is best-effort.
func optimizeGoFile(root *os.Root, filePath string) {
	dir := filepath.Dir(filePath)
	cfg := &packages.Config{
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles |
			packages.NeedTypes | packages.NeedTypesInfo | packages.NeedSyntax |
			packages.NeedFset,
		Dir: dir,
	}
	pkgs, err := packages.Load(cfg, ".")
	if err != nil {
		fmt.Fprintf(os.Stderr, "optimizeGoFile: loading package for %s: %v\n", filePath, err)
		return
	}
	if len(pkgs) == 0 {
		return
	}
	pkg := pkgs[0]
	if len(pkg.Errors) > 0 {
		// Package has errors; skip optimization to avoid cascading failures.
		return
	}

	var diags []analysis.Diagnostic
	for _, a := range modernize.Suite {
		pass := &analysis.Pass{
			Analyzer:  a,
			Fset:      pkg.Fset,
			Files:     pkg.Syntax,
			Pkg:       pkg.Types,
			TypesInfo: pkg.TypesInfo,
			Report: func(d analysis.Diagnostic) {
				diags = append(diags, d)
			},
		}
		if _, err := a.Run(pass); err != nil {
			fmt.Fprintf(os.Stderr, "optimizeGoFile: analyzer %s: %v\n", a.Name, err)
			return
		}
	}

	var fileDiags []analysis.Diagnostic
	for _, d := range diags {
		if d.Position.Filename == filePath {
			fileDiags = append(fileDiags, d)
		}
	}
	if len(fileDiags) == 0 {
		return
	}

	fixed, err := analysisutil.ApplyFixes(pkg.Fset, fileDiags)
	if err != nil {
		fmt.Fprintf(os.Stderr, "optimizeGoFile: applying fixes: %v\n", err)
		return
	}
	for f, content := range fixed {
		if f.Name() == filePath {
			if err := root.WriteFile(filePath, finalizeContent(content), 0644); err != nil {
				fmt.Fprintf(os.Stderr, "optimizeGoFile: writing fixed file: %v\n", err)
			}
			break
		}
	}
}
