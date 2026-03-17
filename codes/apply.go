package codes

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Hunk represents a single modification unit parsed from AI output.
type Hunk struct {
	Op       string
	Target   string
	FilePath string
	Body     string
	Raw      string
}

var headerRegexp = regexp.MustCompile(`^(\s*)\[\[\[ (MODIFY|ADD_BEFORE|ADD_AFTER|DELETE)\s+(\S+?)\s+IN\s+("[^"]*"|'[^']*'|\S+?)(\s*\]\]\]|\s|$)`)

type BodyInfo struct {
	Decls     []ast.Decl
	Specs     []ast.Spec
	Fset      *token.FileSet
	PrefixLen int
	Src       []byte
}

func getBodyInfo(body string) (*BodyInfo, error) {
	if body == "" {
		return nil, nil
	}
	src := []byte(body)
	prefixLen := 0
	if !hasPackage(src) {
		prefix := "package p\n"
		src = append([]byte(prefix), src...)
		prefixLen = len(prefix)
	}
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "", src, parser.ParseComments)
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

func ApplyHunks(root *os.Root, aiFilePath string) error {
	for {
		content, err := os.ReadFile(aiFilePath)
		if err != nil {
			return err
		}
		h, start, end, ok := parseFirstHunk(content)
		if !ok {
			break
		}
		if err := applyHunk(root, h); err != nil {
			return fmt.Errorf("hunk %s %s: %w", h.Op, h.Target, err)
		}
		// Remove the successfully applied hunk from the file content
		newContent := append(content[:start], content[end:]...)
		if err := os.WriteFile(aiFilePath, bytes.TrimSpace(newContent), 0644); err != nil {
			return err
		}
	}
	return nil
}

func parseFirstHunk(content []byte) (h Hunk, start int, end int, ok bool) {
	lines := bytes.Split(content, []byte("\n"))
	var startOffset int
	for i, line := range lines {
		if m := headerRegexp.FindSubmatchIndex(line); m != nil {
			h.Op = string(line[m[4]:m[5]])
			h.Target = string(line[m[6]:m[7]])
			filePathMatch := string(line[m[8]:m[9]])
			// Remove surrounding quotes if present
			if len(filePathMatch) >= 2 && (filePathMatch[0] == '"' || filePathMatch[0] == '\'') && filePathMatch[0] == filePathMatch[len(filePathMatch)-1] {
				h.FilePath = filePathMatch[1 : len(filePathMatch)-1]
			} else {
				h.FilePath = filePathMatch
			}
			start = startOffset

			// Special case for DELETE on same line
			if h.Op == "DELETE" && bytes.Contains(line, []byte("]]]")) {
				end = start + len(line)
				h.Raw = string(content[start:end])
				h.Body = ""
				ok = true
				return
			}

			// Search for closing ]]]
			var footerOffset int = startOffset + len(line) + 1
			for j := i + 1; j < len(lines); j++ {
				// If we encounter another header before a footer
				if headerRegexp.Match(lines[j]) {
					if bytes.Contains(line, []byte("]]]")) {
						// Header had ]]], treat as closing tag and take everything before next header as body
						end = footerOffset - 1
						h.Raw = string(content[start:end])
						bodyStart := start + len(line) + 1
						bodyEnd := end
						if bodyEnd > bodyStart {
							h.Body = strings.TrimSpace(string(content[bodyStart:bodyEnd]))
						}
						ok = true
						return
					}
					// Otherwise broken hunk
					break
				}

				trimmedLine := bytes.TrimSpace(lines[j])
				if idx := bytes.Index(lines[j], []byte("]]]")); idx != -1 {
					// Delimiter is ]]] at start or end of line (ignoring whitespace)
					if bytes.HasPrefix(trimmedLine, []byte("]]]")) || bytes.HasSuffix(trimmedLine, []byte("]]]")) {
						if bytes.HasSuffix(trimmedLine, []byte("]]]")) {
							idx = bytes.LastIndex(lines[j], []byte("]]]"))
						}
						end = footerOffset + idx + 3
						h.Raw = string(content[start:end])
						bodyStart := start + len(line) + 1
						bodyEnd := footerOffset + idx
						if bodyEnd > bodyStart {
							h.Body = strings.TrimSpace(string(content[bodyStart:bodyEnd]))
						}
						ok = true
						return
					}
				}

				footerOffset += len(lines[j]) + 1
			}

			// End of file reached
			if bytes.Contains(line, []byte("]]]")) {
				end = len(content)
				h.Raw = string(content[start:end])
				bodyStart := start + len(line) + 1
				bodyEnd := end
				if bodyEnd > bodyStart {
					h.Body = strings.TrimSpace(string(content[bodyStart:bodyEnd]))
				}
				ok = true
				return
			}
			return
		}
		startOffset += len(line) + 1
	}
	return
}

func applyHunk(root *os.Root, h Hunk) error {
	path := h.FilePath
	if filepath.IsAbs(path) { // Convert absolute path to relative if it is within CWD
		cwd, err := os.Getwd()
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(cwd, path)
		if err != nil || strings.HasPrefix(rel, "..") {
			return fmt.Errorf("path outside of current directory: %s", path)
		}
		path = rel
	}
	if strings.HasPrefix(filepath.Clean(path), "..") { // Proactively block directory escape
		return fmt.Errorf("path escapes current directory: %s", path)
	}

	src, err := root.ReadFile(path) // Use os.Root for safe reading
	if err != nil && !os.IsNotExist(err) {
		return err
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

	h.Body = stripMarkdown(h.Body)
	bodyInfo, _ := getBodyInfo(h.Body)
	bodyName := getHunkBodyName(h.Body)

	var start, end int
	var finalBody string = h.Body

	// Implementation of Theory: ADD_BEFORE/AFTER acts as MODIFY if name already exists
	if (h.Op == "ADD_BEFORE" || h.Op == "ADD_AFTER") && bodyName != "" {
		if s, e, fb, err := findTargetRange(fset, f, Hunk{Op: "MODIFY", Target: bodyName, Body: h.Body}, bodyInfo, len(src), prefixLen); err == nil {
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

	if f != nil && h.Target != "BEGIN" && h.Target != "END" {
		finalBody = stripPackage(finalBody)
	}

	var newSrc []byte
	switch h.Op {
	case "MODIFY":
		newSrc = append(src[:start], append([]byte(finalBody), src[end:]...)...)
	case "DELETE":
		newSrc = append(src[:start], src[end:]...)
	case "ADD_BEFORE":
		newSrc = append(src[:start], append([]byte(finalBody+"\n\n"), src[start:]...)...)
	case "ADD_AFTER":
		newSrc = append(src[:end], append([]byte("\n\n"+finalBody), src[end:]...)...)
	}

	outputSrc := newSrc
	outputPrefixLen := 0
	if !hasPackage(newSrc) {
		outputSrc = append([]byte("package p\n"), newSrc...)
		outputPrefixLen = len("package p\n")
	}
	formatted, err := format.Source(outputSrc)
	if err != nil {
		return fmt.Errorf("format failed: %w", err)
	}
	if outputPrefixLen > 0 {
		formatted = formatted[outputPrefixLen:]
	}

	if dir := filepath.Dir(path); dir != "." {
		if err := rootMkdirAll(root, dir, 0755); err != nil {
			return err
		}
	}
	return root.WriteFile(path, bytes.TrimSpace(formatted), 0644) // Use os.Root for safe writing
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

func findTargetRange(fset *token.FileSet, f *ast.File, h Hunk, bodyInfo *BodyInfo, fileSize int, prefixLen int) (int, int, string, error) {
	if h.Target == "BEGIN" {
		return 0, 0, h.Body, nil
	}
	if h.Target == "END" {
		return fileSize, fileSize, h.Body, nil
	}
	if f == nil {
		return 0, 0, h.Body, fmt.Errorf("target %s not found", h.Target)
	}

	bodyKind := ""
	if bodyInfo != nil && bodyInfo.entityCount() > 0 {
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
				}
			}
		} else {
			// FuncDecl or simple GenDecl
			start, end = nodeStart, nodeEnd
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
		fullName := funcName
		if d.Recv != nil && len(d.Recv.List) > 0 {
			recv := d.Recv.List[0].Type
			if star, ok := recv.(*ast.StarExpr); ok {
				recv = star.X
			}
			if ident, ok := recv.(*ast.Ident); ok {
				fullName = ident.Name + "." + funcName
			}
		}
		if fullName == target || funcName == target {
			return d, d, true
		}
	case *ast.GenDecl:
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

func getHunkBodyName(body string) string {
	info, err := getBodyInfo(body)
	if err != nil || info == nil || info.entityCount() == 0 {
		return ""
	}
	d := info.Decls[0]
	if fn, ok := d.(*ast.FuncDecl); ok {
		name := fn.Name.Name
		if fn.Recv != nil && len(fn.Recv.List) > 0 {
			recv := fn.Recv.List[0].Type
			if star, ok := recv.(*ast.StarExpr); ok {
				recv = star.X
			}
			if ident, ok := recv.(*ast.Ident); ok {
				return ident.Name + "." + name
			}
		}
		return name
	}
	if g, ok := d.(*ast.GenDecl); ok && len(g.Specs) > 0 {
		spec := g.Specs[0]
		if ts, ok := spec.(*ast.TypeSpec); ok {
			return ts.Name.Name
		}
		if vs, ok := spec.(*ast.ValueSpec); ok {
			return vs.Names[0].Name
		}
	}
	return ""
}

func getHunkBodyKind(body string) string {
	info, err := getBodyInfo(body)
	if err != nil || info == nil || info.entityCount() == 0 {
		return ""
	}
	return getDeclKind(info.Decls[0])
}

func getDeclKind(node ast.Node) string {
	switch n := node.(type) {
	case *ast.FuncDecl:
		if n.Recv != nil && len(n.Recv.List) > 0 {
			return "method"
		}
		return "function"
	case *ast.GenDecl:
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
	if !strings.HasPrefix(strings.TrimLeft(body, " \t\n\r"), "package ") {
		return body
	}
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

func stripMarkdown(s string) string {
	s = strings.TrimSpace(s)
	start := strings.Index(s, "```")
	if start == -1 {
		return s
	}
	// skip the line with ```
	nextNl := strings.Index(s[start:], "\n")
	if nextNl == -1 {
		return s
	}
	contentStart := start + nextNl + 1

	end := strings.LastIndex(s, "```")
	if end <= contentStart {
		return s
	}
	return strings.TrimSpace(s[contentStart:end])
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