package changes

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

	"github.com/reusee/tai/pathutil"
	"golang.org/x/tools/imports"
)

const TheoryOfChangeBlockApplication = `
Change block application translates parsed change blocks into byte-level edits on source files.
When an ADD operation targets a spec nested inside a multi-spec declaration block (e.g.,
const or var groups), the insertion point redirects to the parent block boundary to avoid
producing invalid code inside the parentheses. The inserted body must remain a complete,
self-contained declaration so the resulting source is valid Go.
When the body lacks a declaration keyword (e.g., "foo = 1" instead of "const foo = 1"),
getBodyInfo parses it by prepending the keyword, and ApplyChangeBlockStore strips the full
prefix. For ADD_BEFORE and ADD_AFTER operations, the keyword must be re-added after
findTargetRange returns, because the body is inserted as a standalone declaration outside
the target's syntactic context. MODIFY operations handle the keyword separately inside
findTargetRange and are unaffected.
WRITE bypasses declaration-level parsing and replaces the entire file content. Go files
are still processed through goimports to keep imports synchronized after full replacement.
DELETE with target * removes the entire file from the working tree, bypassing
declaration-level parsing. This works for both Go and non-Go files. If the file does not
exist, the operation is a no-op, consistent with the DELETE declaration behavior that
returns nil when the target is not found.
Text-level operations (REPLACE, INSERT_BEFORE, INSERT_AFTER) are rejected for Go files
at the application layer because the model cannot reliably reproduce whitespace in find
strings; structural operations must be used instead. See TheoryOfTextLevelOperations.
Final output normalization ensures every written file ends with exactly one trailing
newline, matching the convention enforced by go fmt. This replaces the prior use of
bytes.TrimSpace which stripped the trailing newline entirely.
After building the modified Go source, parseAndFormat parses it immediately to catch
syntax errors before goimports, which may report formatting-aware errors that obscure
the root cause. On parse or goimports failure, an XML error log is written to the
current directory recording the original source, change block, modified content, and
error. See TheoryOfErrorLogging.
Package detection (hasPackage) skips leading comments, including build constraint
comments such as //go:build and // +build, to determine whether the source already
contains a package clause. Without this, a file whose package declaration is preceded
by a build constraint would be misdetected as lacking a package, causing a synthetic
"package p" prefix to be prepended and producing a file with two package declarations
that fails to parse.
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

// ApplyChangeBlock applies a change block to the given root.
// It is a convenience wrapper that creates a rootStore from the given
// *os.Root and delegates to ApplyChangeBlockStore. See TheoryOfInMemoryApply.
func ApplyChangeBlock(root *os.Root, h ChangeBlock) error {
	return ApplyChangeBlockStore(NewRootStore(root), h)
}

// parseAndFormat parses the modified Go source immediately to catch syntax
// errors before goimports, then runs goimports for formatting and import
// synchronization. On error, an XML error log is written to the current
// directory with the original source, modified content, and error details.
// See TheoryOfErrorLogging.
func parseAndFormat(path string, h ChangeBlock, src []byte, modified []byte, prefixLen int) ([]byte, error) {
	if _, parseErr := parser.ParseFile(token.NewFileSet(), path, modified, parser.ParseComments); parseErr != nil {
		_ = writeErrorLog(h, src, modified, parseErr)
		return nil, fmt.Errorf("parse error after apply: %w", parseErr)
	}
	formatted, err := imports.Process(path, modified, nil)
	if err != nil {
		_ = writeErrorLog(h, src, modified, err)
		return nil, fmt.Errorf("goimports: %w", err)
	}
	if prefixLen > 0 {
		formatted = formatted[prefixLen:]
	}
	return formatted, nil
}

// ApplyChangeBlockStore applies a change block to the given FileStore.
// When the store is a MemoryStore, changes are buffered in memory and
// only written to disk on Flush. This enables early error detection
// during streaming while preserving filesystem consistency on retry.
// See TheoryOfInMemoryApply.
func ApplyChangeBlockStore(store FileStore, h ChangeBlock) error {
	path := h.FilePath
	if filepath.IsAbs(path) { // Convert absolute path to relative if it is within CWD
		cwd, err := os.Getwd()
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(cwd, path)
		if err != nil || pathutil.EscapesDir(rel) {
			return fmt.Errorf("path outside of current directory: %s", path)
		}
		path = rel
	}
	if pathutil.EscapesDir(filepath.Clean(path)) { // Proactively block directory escape
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
			if err != nil || pathutil.EscapesDir(rel) {
				return fmt.Errorf("new path outside of current directory: %s", newPath)
			}
			newPath = rel
		}
		if pathutil.EscapesDir(filepath.Clean(newPath)) {
			return fmt.Errorf("new path escapes current directory: %s", newPath)
		}
		return store.Rename(path, newPath)
	}

	// Handle WRITE: replace the entire file content, bypassing declaration-level parsing.
	// The target field is ignored; file-path determines the destination.
	// Go files are processed through parseAndFormat (parse check + goimports) to catch
	// syntax errors early and keep imports synchronized. See TheoryOfErrorLogging.
	if h.Op == "WRITE" {
		content := []byte(h.Body)
		if strings.HasSuffix(path, ".go") {
			formatted, err := parseAndFormat(path, h, nil, content, 0)
			if err != nil {
				return err
			}
			content = formatted
		}
		return store.WriteFile(path, finalizeContent(content), 0644)
	}

	// Handle DELETE with target *: delete the entire file, bypassing
	// declaration-level parsing. Works for both Go and non-Go files. If the
	// file does not exist, the operation is a no-op, consistent with the
	// DELETE declaration behavior that returns nil when the target is not
	// found. See ChangeBlockApplicationTheory.
	if h.Op == "DELETE" && h.Target == "*" {
		if err := store.Remove(path); err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		return nil
	}

	src, err := store.ReadFile(path) // Use FileStore for safe reading
	if err != nil && !os.IsNotExist(err) {
		return err
	}

	// Handle text-level operations (REPLACE, INSERT_BEFORE, INSERT_AFTER):
	// These work on non-Go text files using string search, bypassing
	// structural parsing. They are rejected for Go files because the model
	// cannot reliably reproduce whitespace characters in the find string,
	// causing matching failures; structural operations must be used instead.
	// See TheoryOfTextLevelOperations.
	if isTextLevelOperation(h.Op) {
		if isGoFile(path) {
			return fmt.Errorf("Go file %q does not support text-level operations (REPLACE, INSERT_BEFORE, INSERT_AFTER); use structural operations (MODIFY, ADD_BEFORE, ADD_AFTER, DELETE) instead", path)
		}
		if err != nil {
			return err
		}
		newContent, editErr := applyTextEdit(src, h)
		if editErr != nil {
			_ = writeErrorLog(h, src, nil, editErr)
			return editErr
		}
		return store.WriteFile(path, finalizeContent(newContent), 0644)
	}

	// Handle non-Go files
	if !strings.HasSuffix(path, ".go") {
		if os.IsNotExist(err) && h.Op == "ADD_BEFORE" && h.Target == "BEGIN" {
			// Allow creating new non-Go file
			body := h.Body
			return store.WriteFile(path, []byte(body), 0644)
		}
		return fmt.Errorf("only .go files are supported for modification: %s", path)
	}

	fset := token.NewFileSet()
	var f *ast.File
	var prefixLen int
	if len(src) > 0 {
		f, prefixLen, err = parseGoSource(fset, path, src)
		if err != nil {
			_ = writeErrorLog(h, src, nil, err)
			return err
		}
	}

	// Handle special Go-only targets: package and import.
	// These replace the package clause or import block without rewriting
	// the entire file, saving tokens. See TheoryOfSpecialGoTargets in
	// parse.go.
	if h.Target == "package" || h.Target == "import" {
		if h.Op != "MODIFY" {
			return fmt.Errorf("target %q only supports MODIFY, got op=%q", h.Target, h.Op)
		}
		if f == nil {
			return fmt.Errorf("target %q: file %s could not be parsed", h.Target, path)
		}
		return applySpecialTargetModify(store, path, src, f, fset, prefixLen, h)
	}

	bodyInfo, _ := getBodyInfo(h.Body)
	if bodyInfo != nil {
		h.Body = string(bodyInfo.Src[bodyInfo.PrefixLen:])
	}
	bodyName := getChangeBlockBodyNameFromInfo(bodyInfo)

	var start, end int
	var finalBody string = h.Body

	// Implementation of Theory: ADD_BEFORE/AFTER acts as MODIFY if name already exists
	if (h.Op == "ADD_BEFORE" || h.Op == "ADD_AFTER") && bodyName != "" {
		if s, e, fb, err := findTargetRange(fset, f, ChangeBlock{Op: "MODIFY", Target: bodyName, Body: h.Body}, bodyInfo, len(src), prefixLen); err == nil {
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

	// For ADD_BEFORE/ADD_AFTER that weren't converted to MODIFY, ensure the
	// keyword is present if the body was parsed with a keyword prefix. The
	// prefix is stripped from h.Body during parsing, so the keyword must be
	// re-added to produce a complete, valid Go declaration. This covers all
	// ADD code paths uniformly: target found, target not found (append to
	// end), BEGIN/END, and both single-spec and multi-spec GenDecls. MODIFY
	// paths handle the keyword separately inside findTargetRange and are
	// unaffected because h.Op is no longer ADD after the ADD-as-MODIFY
	// conversion.
	if (h.Op == "ADD_BEFORE" || h.Op == "ADD_AFTER") && bodyInfo != nil && bodyInfo.Keyword != "" {
		finalBody = bodyInfo.Keyword + " " + finalBody
	}

	type rangeItem struct {
		start, end int
		body       string
		isPrimary  bool
	}
	var items []rangeItem
	items = append(items, rangeItem{start: start, end: end, body: finalBody, isPrimary: true})

	// Detect and remove other occurrences of entities present in the change block body
	// to prevent duplication when a change block contains multiple declarations (e.g. Type + Methods).
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
	// Special targets (package, import) are handled before this point and
	// never reach here, so no exclusion is needed.
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
	// Parse and format: parse immediately to catch syntax errors before
	// goimports, then run goimports for formatting and import synchronization.
	// On error, an XML error log is written with the original source, modified
	// content, and error details. See TheoryOfErrorLogging.
	formatted, err := parseAndFormat(path, h, src, outputSrc, outputPrefixLen)
	if err != nil {
		return err
	}

	return store.WriteFile(path, finalizeContent(formatted), 0644) // Use FileStore for safe writing
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

// applySpecialTargetModify handles MODIFY operations for the special Go-only
// targets "package" and "import". These replace the package clause or import
// block without rewriting the entire file, saving tokens compared to WRITE.
// See TheoryOfSpecialGoTargets in parse.go.
func applySpecialTargetModify(store FileStore, path string, src []byte, f *ast.File, fset *token.FileSet, prefixLen int, h ChangeBlock) error {
	var newSrc []byte

	switch h.Target {
	case "package":
		// Normalize the body: parse out the package name and rebuild the clause.
		newPkgName := strings.TrimSpace(h.Body)
		// If the body is a full package clause like "package foo", extract just the name.
		// parser.PackageClauseOnly parses only the package clause without needing a valid body.
		fset2 := token.NewFileSet()
		if f2, err := parser.ParseFile(fset2, "", newPkgName, parser.PackageClauseOnly); err == nil && f2 != nil {
			newPkgName = f2.Name.Name
		} else if after, found := strings.CutPrefix(newPkgName, "package "); found {
			newPkgName = strings.TrimSpace(after)
		}
		if newPkgName == "" {
			return fmt.Errorf("empty package name in MODIFY package body")
		}

		start := fset.Position(f.Pos()).Offset - prefixLen
		end := fset.Position(f.Name.End()).Offset - prefixLen
		newSrc = make([]byte, 0, len(src)+len("package ")+len(newPkgName))
		newSrc = append(newSrc, src[:start]...)
		newSrc = append(newSrc, []byte("package "+newPkgName)...)
		newSrc = append(newSrc, src[end:]...)

	case "import":
		body := strings.TrimSpace(h.Body)

		// Find existing import declarations and their combined range.
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
			// No existing imports; insert after the package clause.
			pkgEnd := fset.Position(f.Name.End()).Offset - prefixLen
			start = pkgEnd
			end = pkgEnd
		}

		// Normalize the body into valid import syntax.
		if body == "" {
			// Remove all imports; goimports will add back any still needed.
			newSrc = make([]byte, 0, len(src))
			newSrc = append(newSrc, src[:start]...)
			newSrc = append(newSrc, src[end:]...)
		} else {
			// Validate that the body contains parseable import declarations.
			// Use the parsed AST only for validation; the body text is used
			// directly to preserve the model's formatting intent.
			// Goimports will fix any formatting issues afterward.
			_, parseErr := parser.ParseFile(token.NewFileSet(), "", "package p\n"+body, parser.ImportsOnly)
			if parseErr != nil {
				return fmt.Errorf("import body is not valid Go import syntax: %w", parseErr)
			}

			// Ensure the body uses proper import syntax. If it looks like raw
			// import paths (no "import" keyword), wrap them in an import block.
			if !strings.HasPrefix(body, "import ") && !strings.HasPrefix(body, "import(") {
				body = "import (\n" + body + "\n)"
			}

			// For existing imports: replace the import range with the body,
			// respecting surrounding whitespace.
			// For new imports: insert after the package clause with a newline
			// separator so the package clause and imports don't merge.
			newSrc = make([]byte, 0, len(src)+len(body)+4)
			newSrc = append(newSrc, src[:start]...)
			if !found {
				// Insert after package clause; add newline separator.
				newSrc = append(newSrc, '\n')
			}
			newSrc = append(newSrc, []byte(body)...)
			newSrc = append(newSrc, '\n')
			newSrc = append(newSrc, src[end:]...)
		}

	default:
		return fmt.Errorf("unknown special target: %q", h.Target)
	}

	// Parse and format: parse immediately to catch syntax errors before
	// goimports, then run goimports for formatting and import synchronization.
	// See TheoryOfErrorLogging.
	formatted, err := parseAndFormat(path, h, src, newSrc, 0)
	if err != nil {
		return err
	}

	return store.WriteFile(path, finalizeContent(formatted), 0644)
}

// applyTextEdit applies a text-level operation (REPLACE, INSERT_BEFORE,
// INSERT_AFTER) to the file content. It searches for the find string,
// verifies it is unique (appears exactly once), and applies the edit
// relative to the found position. See TheoryOfTextLevelOperations.
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
		content = content[:idx] + h.Body + content[idx:]
	case "INSERT_AFTER":
		content = content[:idx+len(find)] + h.Body + content[idx+len(find):]
	default:
		return nil, fmt.Errorf("unknown text-level operation: %s", h.Op)
	}

	return []byte(content), nil
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
