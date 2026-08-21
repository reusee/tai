package gocodes

import (
	"fmt"
	"go/ast"
	"go/token"
	"strings"

	"github.com/reusee/tai/generators"
	"github.com/reusee/tai/logs"
)

const TheoryOfGoSrcResolution = `
ResolveGoSymbols turns the symbol names collected from go-src blocks into
declaration source parts (see blocks.TheoryOfGoSrcBlocks). The resolver
searches the Go files collected by GetFiles — the same file set the context
pipeline loaded, with raw content and parsed ASTs already cached — so
resolution spawns no Go toolchain subprocesses. The symbol forms follow
go doc: [<pkg>.][<sym>.][<methodOrField>]. A package path prefix (the
full import path or a proper suffix of it, e.g. "pkg" for "a/b/pkg")
restricts matching to that package; the remaining parts select a
top-level declaration or a method on a type. An optional leading *
receiver prefix and a trailing generic parameter list ("Pair[A, B].Swap")
are stripped from the type name, so Pair[A, B].Swap, Pair[B, A].Swap,
and Pair.Swap all resolve. Name matching follows go doc's case rule: a
lower-case letter in the query matches either case in the target, an
upper-case letter matches exactly, so "reader.read" resolves Reader.Read.
All matches across loaded packages (or the specified package) are
returned, because an unqualified name may be declared in several
packages; each returned block carries the package-qualified name and
file:line so the model can disambiguate. Doc comments attached to the
matched declaration are included with the source. Unmatched symbols
produce an explicit not-found part rather than an error, giving the model
a concrete correction target; a failure of package loading itself (e.g.,
a non-Go project where the loader was never run for the context)
degrades to an informational part so one stray block cannot abort the
run.
`

// ResolveGoSymbols resolves Go symbol names to their declaration source
// code, returned as user-content parts for the next generation round.
// See TheoryOfGoSrcResolution and blocks.TheoryOfGoSrcBlocks.
type ResolveGoSymbols func(symbols []string) ([]generators.Part, error)

func (Module) ResolveGoSymbols(
	getFiles GetFiles,
	logger logs.Logger,
) ResolveGoSymbols {
	return func(symbols []string) (parts []generators.Part, err error) {
		if len(symbols) == 0 {
			return nil, nil
		}
		files, err := getFiles()
		if err != nil {
			// A non-Go project never runs the loader for its context, so
			// one stray go-src block must not abort the run: degrade to an
			// informational part the model can act on.
			// See TheoryOfGoSrcResolution.
			return []generators.Part{generators.Text(fmt.Sprintf(
				"[go-src: cannot resolve symbols, Go package loading failed: %v]\n\n", err))}, nil
		}
		seen := make(map[string]bool, len(symbols))
		for _, symbol := range symbols {
			symbol = strings.TrimSpace(symbol)
			if symbol == "" || seen[symbol] {
				continue
			}
			seen[symbol] = true
			matches := findSymbolDeclarations(files, symbol)
			if len(matches) == 0 {
				parts = append(parts, generators.Text(fmt.Sprintf(
					"[go-src: symbol %q not found in the loaded packages]\n\n", symbol)))
				continue
			}
			for _, m := range matches {
				parts = append(parts, generators.Text(fmt.Sprintf(
					"``` begin of source %s %s:%d\n%s\n``` end of source %s\n\n",
					m.qualifiedName, m.filePath, m.line, m.source, m.qualifiedName)))
			}
		}
		logger.Info("go-src symbols resolved",
			"requested", len(seen),
			"parts", len(parts),
		)
		return parts, nil
	}
}

// symbolDeclaration is one resolved declaration: its package-qualified
// name, the defining file, the 1-based line where the declaration (or its
// doc comment) starts, and the declaration source including doc comments.
type symbolDeclaration struct {
	qualifiedName string
	filePath      string
	line          int
	source        string
}

// findSymbolDeclarations searches the loaded Go files for declarations
// matching the symbol, returning every match across packages in the
// deterministic file order of the loaded file set. The symbol follows
// the go doc form [<pkg>.][<sym>.][<methodOrField>]: splitGoDocSymbol
// strips an optional package path prefix, then splitSymbolName splits
// the remaining type/method parts. See TheoryOfGoSrcResolution.
func findSymbolDeclarations(files []*File, symbol string) []symbolDeclaration {
	pkgFilter, typeName, name := splitGoDocSymbol(files, symbol)
	var matches []symbolDeclaration
	for _, f := range files {
		if f.AstFile == nil || f.TokenFile == nil || f.Package == nil || len(f.Content) == 0 {
			continue
		}
		pkgPath := basePkgPath(f.Package.PkgPath)
		if pkgFilter != "" && pkgPath != pkgFilter {
			continue
		}
		for _, decl := range f.AstFile.Decls {
			switch d := decl.(type) {
			case *ast.FuncDecl:
				if d.Name == nil || !goDocNameMatch(name, d.Name.Name) {
					continue
				}
				recv := receiverTypeName(d)
				if typeName == "" {
					// A plain name selects only top-level functions;
					// methods require the TypeName.MethodName form so
					// both readings stay unambiguous.
					if recv != "" {
						continue
					}
				} else if !goDocNameMatch(typeName, recv) {
					continue
				}
				matches = appendSymbolMatch(matches, f, pkgPath, typeName, name, declStartPos(d, d.Doc), d.End())
			case *ast.GenDecl:
				if typeName != "" {
					continue // methods never live in a GenDecl
				}
				for _, spec := range d.Specs {
					doc, ok := specDeclaresName(spec, name)
					if !ok {
						continue
					}
					if !d.Lparen.IsValid() && len(d.Specs) == 1 {
						// An unparenthesized declaration is extracted whole
						// so the keyword (type/const/var) stays with the
						// source.
						matches = appendSymbolMatch(matches, f, pkgPath, "", name, declStartPos(d, d.Doc), d.End())
					} else {
						// Inside a grouped declaration the spec alone is
						// extracted, keeping the result precise.
						matches = appendSymbolMatch(matches, f, pkgPath, "", name, declStartPos(spec, doc), spec.End())
					}
					break
				}
			}
		}
	}
	return matches
}

// appendSymbolMatch extracts the source between start and end from the
// file's cached content and appends a symbolDeclaration to matches.
func appendSymbolMatch(matches []symbolDeclaration, f *File, pkgPath, typeName, name string, start, end token.Pos) []symbolDeclaration {
	qualified := pkgPath + "." + name
	if typeName != "" {
		qualified = pkgPath + "." + typeName + "." + name
	}
	return append(matches, symbolDeclaration{
		qualifiedName: qualified,
		filePath:      f.Path,
		line:          f.TokenFile.Line(start),
		source:        string(f.Content[f.TokenFile.Offset(start):f.TokenFile.Offset(end)]),
	})
}

// splitSymbolName splits a go-src symbol into its receiver type name and
// declaration name: "Foo" yields ("", "Foo"); "Reader.Read" yields
// ("Reader", "Read"); a leading * receiver prefix and a trailing generic
// parameter list ("Pair[A, B].Swap") are stripped from the type name.
func splitSymbolName(symbol string) (typeName, name string) {
	symbol = strings.TrimPrefix(symbol, "*")
	if i := strings.LastIndex(symbol, "."); i >= 0 {
		typeName = strings.TrimPrefix(symbol[:i], "*")
		name = symbol[i+1:]
		if i := strings.Index(typeName, "["); i >= 0 {
			typeName = typeName[:i]
		}
		return typeName, name
	}
	return "", symbol
}

// goDocNameMatch reports whether the query matches the target under
// go doc's case rule: a lower-case letter in the query matches either
// case in the target, an upper-case letter in the query matches exactly.
// The rule covers ASCII letters; other characters match exactly. See
// TheoryOfGoSrcResolution.
func goDocNameMatch(query, target string) bool {
	if len(query) != len(target) {
		return false
	}
	for i := 0; i < len(query); i++ {
		q := query[i]
		t := target[i]
		if q >= 'a' && q <= 'z' {
			if t != q && t != q-('a'-'A') {
				return false
			}
		} else if q != t {
			return false
		}
	}
	return true
}

// splitGoDocSymbol splits a go doc symbol form into its package path
// filter and its type/method name parts. The package path is the longest
// suffix of a loaded package path that prefixes the symbol followed by a
// dot (e.g., for path a/b/c, the suffixes tried are a/b/c, b/c, c); the
// filter records the full package path so the file loop compares exact
// paths. The remainder after stripping is split by splitSymbolName, which
// handles the * receiver prefix and generic parameter lists. See
// TheoryOfGoSrcResolution.
func splitGoDocSymbol(files []*File, symbol string) (pkgFilter, typeName, name string) {
	bestPkg := ""
	bestPrefixLen := 0
	for _, f := range files {
		if f.Package == nil {
			continue
		}
		p := basePkgPath(f.Package.PkgPath)
		if p == "" {
			continue
		}
		for candidate := p; candidate != ""; {
			if strings.HasPrefix(symbol, candidate+".") && len(candidate) > bestPrefixLen {
				bestPkg = p
				bestPrefixLen = len(candidate)
				break
			}
			if i := strings.Index(candidate, "/"); i >= 0 {
				candidate = candidate[i+1:]
			} else {
				break
			}
		}
	}
	if bestPrefixLen > 0 {
		symbol = symbol[bestPrefixLen+1:]
	}
	typeName, name = splitSymbolName(symbol)
	return bestPkg, typeName, name
}

// receiverTypeName returns the base type name of a method's receiver,
// unwrapping pointer and generic instantiation forms: *Foo, Foo[T], and
// *Foo[T, U] all yield Foo.
func receiverTypeName(d *ast.FuncDecl) string {
	if d.Recv == nil || len(d.Recv.List) == 0 {
		return ""
	}
	t := d.Recv.List[0].Type
	for {
		switch x := t.(type) {
		case *ast.StarExpr:
			t = x.X
		case *ast.IndexExpr:
			t = x.X
		case *ast.IndexListExpr:
			t = x.X
		case *ast.Ident:
			return x.Name
		default:
			return ""
		}
	}
}

// specDeclaresName reports whether the spec declares a name matching the
// query under go doc's case rule; the returned comment group is the
// spec's own doc comment. See TheoryOfGoSrcResolution.
func specDeclaresName(spec ast.Spec, name string) (*ast.CommentGroup, bool) {
	switch s := spec.(type) {
	case *ast.TypeSpec:
		if s.Name != nil && goDocNameMatch(name, s.Name.Name) {
			return s.Doc, true
		}
	case *ast.ValueSpec:
		for _, n := range s.Names {
			if goDocNameMatch(name, n.Name) {
				return s.Doc, true
			}
		}
	}
	return nil, false
}

// declStartPos returns the start position of a declaration, extended to
// include its doc comment when one is attached.
func declStartPos(decl ast.Node, doc *ast.CommentGroup) token.Pos {
	if doc != nil {
		return doc.Pos()
	}
	return decl.Pos()
}
