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

var headerRegexp = regexp.MustCompile(`^(\s*)\[\[\[ (MODIFY|ADD_BEFORE|ADD_AFTER|DELETE) (\S+) IN (\S+)`)

// ApplyHunks processes hunks from a file and applies them to the source.
func ApplyHunks(aiFilePath string) error {
	for {
		content, err := os.ReadFile(aiFilePath)
		if err != nil {
			return err
		}
		h, start, end, ok := parseFirstHunk(content)
		if !ok {
			break
		}
		if err := applyHunk(h); err != nil {
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
			h.FilePath = string(line[m[8]:m[9]])
			start = startOffset
			if bytes.Contains(line, []byte("]]]")) {
				end = start + len(line)
				h.Raw = string(content[start:end])
				h.Body = ""
				ok = true
				return
			}
			var footerOffset int = startOffset + len(line) + 1
			for j := i + 1; j < len(lines); j++ {
				if string(bytes.TrimSpace(lines[j])) == "]]]" {
					end = footerOffset + len(lines[j])
					h.Raw = string(content[start:end])
					bodyStart := start + len(line) + 1
					bodyEnd := footerOffset
					if bodyEnd > bodyStart {
						h.Body = strings.TrimSpace(string(content[bodyStart:bodyEnd]))
					}
					ok = true
					return
				}
				footerOffset += len(lines[j]) + 1
			}
			return
		}
		startOffset += len(line) + 1
	}
	return
}

func applyHunk(h Hunk) error {
	fset := token.NewFileSet()
	src, err := os.ReadFile(h.FilePath)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	var f *ast.File
	if len(src) > 0 {
		f, err = parser.ParseFile(fset, h.FilePath, src, parser.ParseComments)
		if err != nil {
			return err
		}
	}
	start, end, err := findTargetRange(fset, f, h, len(src))
	if err != nil {
		if h.Op == "MODIFY" {
			// fallback to append if not found
			start, end = len(src), len(src)
		} else if h.Op == "DELETE" {
			// no-op if not found
			return nil
		} else {
			return err
		}
	}

	body := h.Body
	if f != nil && h.Target != "BEGIN" && h.Target != "END" {
		body = stripPackage(body)
	}

	var newSrc []byte
	switch h.Op {
	case "MODIFY":
		bodyBytes := []byte(body)
		if start == end && start == len(src) && len(src) > 0 {
			bodyBytes = append([]byte("\n\n"), bodyBytes...)
		}
		newSrc = append(src[:start], append(bodyBytes, src[end:]...)...)
	case "DELETE":
		newSrc = append(src[:start], src[end:]...)
	case "ADD_BEFORE":
		newSrc = append(src[:start], append([]byte(body+"\n\n"), src[start:]...)...)
	case "ADD_AFTER":
		newSrc = append(src[:end], append([]byte("\n\n"+body), src[end:]...)...)
	}
	formatted, err := format.Source(newSrc)
	if err != nil {
		return fmt.Errorf("format failed: %w", err)
	}
	if dir := filepath.Dir(h.FilePath); dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
	}
	return os.WriteFile(h.FilePath, formatted, 0644)
}

func findTargetRange(fset *token.FileSet, f *ast.File, h Hunk, fileSize int) (int, int, error) {
	if h.Target == "BEGIN" {
		return 0, 0, nil
	}
	if h.Target == "END" {
		return fileSize, fileSize, nil
	}
	if f == nil {
		return 0, 0, fmt.Errorf("target %s not found", h.Target)
	}

	bodyKind := getHunkBodyKind(h.Body)
	var candidateFound bool
	var candidateStart, candidateEnd int

	for _, decl := range f.Decls {
		start, end, match := matchDecl(fset, decl, h.Target)
		if !match {
			continue
		}
		if h.Op == "MODIFY" && bodyKind != "" {
			declKind := getDeclKind(decl)
			if declKind != bodyKind {
				if !candidateFound {
					candidateFound = true
					candidateStart, candidateEnd = start, end
				}
				continue
			}
		}
		return start, end, nil
	}

	if candidateFound {
		return candidateStart, candidateEnd, nil
	}
	return 0, 0, fmt.Errorf("target %s not found", h.Target)
}

func matchDecl(fset *token.FileSet, decl ast.Decl, target string) (int, int, bool) {
	startPos := decl.Pos()
	switch d := decl.(type) {
	case *ast.FuncDecl:
		if d.Doc != nil {
			startPos = d.Doc.Pos()
		}
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
			return fset.Position(startPos).Offset, fset.Position(d.End()).Offset, true
		}
	case *ast.GenDecl:
		if d.Doc != nil {
			startPos = d.Doc.Pos()
		}
		for _, spec := range d.Specs {
			switch s := spec.(type) {
			case *ast.TypeSpec:
				if s.Name.Name == target {
					return fset.Position(startPos).Offset, fset.Position(d.End()).Offset, true
				}
			case *ast.ValueSpec:
				for _, n := range s.Names {
					if n.Name == target {
						return fset.Position(startPos).Offset, fset.Position(d.End()).Offset, true
					}
				}
			}
		}
	}
	return 0, 0, false
}

func getHunkBodyKind(body string) string {
	fset := token.NewFileSet()
	src := body
	if !strings.HasPrefix(strings.TrimLeft(body, " \t\n\r"), "package ") {
		src = "package p\n" + body
	}
	f, err := parser.ParseFile(fset, "", src, 0)
	if err != nil || len(f.Decls) == 0 {
		return ""
	}
	return getDeclKind(f.Decls[0])
}

func getDeclKind(decl ast.Decl) string {
	switch d := decl.(type) {
	case *ast.FuncDecl:
		if d.Recv != nil && len(d.Recv.List) > 0 {
			return "method"
		}
		return "function"
	case *ast.GenDecl:
		if len(d.Specs) == 0 {
			return ""
		}
		switch d.Specs[0].(type) {
		case *ast.TypeSpec:
			return "type"
		case *ast.ValueSpec:
			if d.Tok == token.VAR {
				return "var"
			}
			if d.Tok == token.CONST {
				return "const"
			}
		}
	}
	return ""
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
	var startPos token.Pos = firstDecl.Pos()
	switch d := firstDecl.(type) {
	case *ast.FuncDecl:
		if d.Doc != nil {
			startPos = d.Doc.Pos()
		}
	case *ast.GenDecl:
		if d.Doc != nil {
			startPos = d.Doc.Pos()
		}
	}
	offset := fset.Position(startPos).Offset
	return strings.TrimSpace(body[offset:])
}
