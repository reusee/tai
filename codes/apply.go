package codes

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
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

var hunkRegexp = regexp.MustCompile(`(?s)\[\[\[ (MODIFY|ADD_BEFORE|ADD_AFTER|DELETE) (\S+) IN (\S+)\n?(.*?)\n?\]\]\]`)

// ApplyHunks processes hunks from a file and applies them to the source.
// It returns the content of the file after removing applied hunks.
func ApplyHunks(aiFilePath string) error {
	content, err := os.ReadFile(aiFilePath)
	if err != nil {
		return err
	}
	matches := hunkRegexp.FindAllSubmatchIndex(content, -1)
	if len(matches) == 0 {
		return nil
	}
	// process from last to first to maintain offsets in aiFilePath
	var appliedIndices []int
	for i := 0; i < len(matches); i++ {
		m := matches[i]
		h := Hunk{
			Op:       string(content[m[2]:m[3]]),
			Target:   string(content[m[4]:m[5]]),
			FilePath: string(content[m[6]:m[7]]),
			Body:     strings.TrimSpace(string(content[m[8]:m[9]])),
			Raw:      string(content[m[0]:m[1]]),
		}
		if err := applyHunk(h); err != nil {
			return fmt.Errorf("hunk %s %s: %w", h.Op, h.Target, err)
		}
		appliedIndices = append(appliedIndices, i)
	}
	// remove applied hunks from .AI file
	newAIContent := content
	for i := len(appliedIndices) - 1; i >= 0; i-- {
		m := matches[appliedIndices[i]]
		newAIContent = append(newAIContent[:m[0]], newAIContent[m[1]:]...)
	}
	return os.WriteFile(aiFilePath, bytes.TrimSpace(newAIContent), 0644)
}

func applyHunk(h Hunk) error {
	fset := token.NewFileSet()
	src, err := os.ReadFile(h.FilePath)
	if err != nil {
		return err
	}
	f, err := parser.ParseFile(fset, h.FilePath, src, parser.ParseComments)
	if err != nil {
		return err
	}
	start, end, err := findTargetRange(fset, f, h.Target, len(src))
	if err != nil {
		return err
	}
	var newSrc []byte
	switch h.Op {
	case "MODIFY":
		newSrc = append(src[:start], append([]byte(h.Body), src[end:]...)...)
	case "DELETE":
		newSrc = append(src[:start], src[end:]...)
	case "ADD_BEFORE":
		newSrc = append(src[:start], append([]byte(h.Body+"\n\n"), src[start:]...)...)
	case "ADD_AFTER":
		newSrc = append(src[:end], append([]byte("\n\n"+h.Body), src[end:]...)...)
	}
	formatted, err := format.Source(newSrc)
	if err != nil {
		return fmt.Errorf("format failed: %w", err)
	}
	return os.WriteFile(h.FilePath, formatted, 0644)
}

func findTargetRange(fset *token.FileSet, f *ast.File, target string, fileSize int) (int, int, error) {
	if target == "BEGIN" {
		return 0, 0, nil
	}
	if target == "END" {
		return fileSize, fileSize, nil
	}
	for _, decl := range f.Decls {
		start, end, match := matchDecl(fset, decl, target)
		if match {
			return start, end, nil
		}
	}
	return 0, 0, fmt.Errorf("target %s not found", target)
}

func matchDecl(fset *token.FileSet, decl ast.Decl, target string) (int, int, bool) {
	switch d := decl.(type) {
	case *ast.FuncDecl:
		name := d.Name.Name
		if d.Recv != nil && len(d.Recv.List) > 0 {
			recv := d.Recv.List[0].Type
			if star, ok := recv.(*ast.StarExpr); ok {
				recv = star.X
			}
			if ident, ok := recv.(*ast.Ident); ok {
				name = ident.Name + "." + name
			}
		}
		if name == target {
			return fset.Position(d.Pos()).Offset, fset.Position(d.End()).Offset, true
		}
	case *ast.GenDecl:
		for _, spec := range d.Specs {
			switch s := spec.(type) {
			case *ast.TypeSpec:
				if s.Name.Name == target {
					return fset.Position(d.Pos()).Offset, fset.Position(d.End()).Offset, true
				}
			case *ast.ValueSpec:
				for _, n := range s.Names {
					if n.Name == target {
						return fset.Position(d.Pos()).Offset, fset.Position(d.End()).Offset, true
					}
				}
			}
		}
	}
	return 0, 0, false
}
