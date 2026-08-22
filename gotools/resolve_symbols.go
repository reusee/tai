package gotools

import (
	"fmt"
	"go/ast"
	"go/token"
	"maps"
	"slices"
	"strings"

	"github.com/reusee/tai/generators"
	"github.com/reusee/tai/logs"
)

const TheoryOfGoSrcResolution = `
ResolveGoSymbols turns the symbol names collected from go-src blocks into
declaration source parts (see TheoryOfGoSrcBlocks). The resolver
searches the Go files collected by GetFiles — the same file set the context
pipeline loaded, with raw content and parsed ASTs already cached — so
resolution spawns no Go toolchain subprocesses. The symbol forms follow
go doc: [<pkg>.][<sym>.][<methodOrField>]. A package qualifier — the
full import path, a proper suffix of it (e.g., "pkg" for "a/b/pkg"), or
a loaded package's declared name — restricts matching to that package,
longest qualifier first; the declared-name form keeps major-version
packages whose name is not a path segment (doublestar for …/v4)
addressable as name.Symbol. When a qualified form matches nothing, the
qualifier segment is retried as a receiver type name, so a package name
that shadows a type name (graphemes vs Graphemes) does not hide the
type's methods. The remaining parts select a
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

A symbol that exactly matches a loaded package — by import path (base
path, test variants merged) or by declared package name — resolves to
the package's go doc documentation instead of declaration source.
Package matching takes precedence over symbol matching, mirroring go
doc, and a package name may match several packages, all of which are
returned. go doc runs from the load directory (or the workspace root in
workspace mode) with the read-only environment of TheoryOfGoDocReadonly,
so go.sum is never modified; every package is documented with -all -cmd —
all declarations, including a main package's — and a focus (root) package
adds -u so unexported symbols are shown: the model edits focus packages
and needs their complete surface, while a context package's exported API
surface suffices. A failed go doc yields an explicit error part for that
package, never an abort.

Resolution is a read over the cached file set, not a fresh disk scan:
GetFiles is a scope-cached provider resolved once (at context assembly),
and change blocks applied during the session do not mutate the cached
file contents or ASTs, so a repeated fetch returns the pre-modification
source. The prompts state this snapshot contract explicitly — the model
verifies applied changes with go-test blocks or disk reads rather than
re-fetching symbols — and recommend the full import path as the
qualifier form because it is unique over loaded packages, unlike a
declared name or path suffix that may match several same-named packages.
`

// ResolveGoSymbols resolves Go symbol names to their declaration source
// code, returned as user-content parts for the next generation round. A
// symbol that names a loaded package (exact import path or package name)
// resolves to the package's go doc documentation instead.
// See TheoryOfGoSrcResolution and blocks.TheoryOfGoSrcBlocks.
type ResolveGoSymbols func(symbols []string) ([]generators.Part, error)

func (Module) ResolveGoSymbols(
	getFiles GetFiles,
	getTypeCheckedPackages GetTypeCheckedPackages,
	loadDir LoadDir,
	workspace Workspace,
	envs Envs,
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
		pkgIndex := indexLoadedPackages(files)
		// go doc resolves import paths from the load directory; in
		// workspace mode it runs from the workspace root so the paths of
		// every workspace module resolve. See TheoryOfGoSrcResolution.
		docDir := string(loadDir)
		if workspace != "" {
			docDir = string(workspace)
		}
		// renderedPkgs deduplicates documentation across package symbol
		// forms: requesting a package by both path and name renders it
		// once.
		renderedPkgs := make(map[string]bool)
		// The reference index is built lazily, once per resolve call: the
		// type-checked load is expensive and only needed when a symbol
		// actually resolves. A load failure degrades to source-only output
		// rather than aborting the resolve. See TheoryOfGoSrcReferences.
		var refIndex *typeCheckIndex
		refIndexReady := false
		ensureRefIndex := func() *typeCheckIndex {
			if refIndexReady {
				return refIndex
			}
			refIndexReady = true
			typeCheckedPkgs, err := getTypeCheckedPackages()
			if err != nil {
				logger.Warn("go-src references unavailable", "err", err)
				return nil
			}
			refIndex = buildTypeCheckIndex(typeCheckedPkgs)
			return refIndex
		}
		seen := make(map[string]bool, len(symbols))
		for _, symbol := range symbols {
			symbol = strings.TrimSpace(symbol)
			if symbol == "" || seen[symbol] {
				continue
			}
			seen[symbol] = true
			// A package reference takes precedence over symbol matching,
			// mirroring go doc. See TheoryOfGoSrcResolution.
			var matched bool
			parts, matched = appendPackageDocParts(parts, symbol, pkgIndex, renderedPkgs, docDir, []string(envs))
			if matched {
				continue
			}
			matches := findSymbolDeclarations(files, symbol)
			if len(matches) == 0 {
				parts = append(parts, generators.Text(fmt.Sprintf(
					"[go-src: symbol or package %q not found in the loaded packages]\n\n", symbol)))
				continue
			}
			for _, m := range matches {
				parts = append(parts, generators.Text(fmt.Sprintf(
					"``` begin of source %s %s:%d\n%s\n``` end of source %s\n\n",
					m.qualifiedName, m.filePath, m.line, m.source, m.qualifiedName)))
				// Each resolved source part is followed by the references
				// report for the same symbol when references exist. See
				// TheoryOfGoSrcReferences.
				if index := ensureRefIndex(); index != nil {
					objects := index.objectsFor(m)
					refs, truncated := index.referencesFor(objects)
					if len(refs) > 0 || truncated {
						parts = append(parts, formatReferencesPart(m.qualifiedName, refs, truncated))
					}
				}
			}
		}
		logger.Info("go-src symbols resolved",
			"requested", len(seen),
			"parts", len(parts),
		)
		return parts, nil
	}
}

// loadedPackage records one package resolvable by go-src package
// resolution: its base import path, its declared package name, and
// whether it is a focus (root) package. See TheoryOfGoSrcResolution.
type loadedPackage struct {
	path  string
	name  string
	focus bool
}

// indexLoadedPackages indexes the packages of the loaded file set by
// their base import path. Test-variant packages ("pkg [pkg.test]") merge
// into the base path; any file of a root (focus) package marks the
// package focus. See TheoryOfGoSrcResolution.
func indexLoadedPackages(files []*File) map[string]loadedPackage {
	index := make(map[string]loadedPackage)
	for _, f := range files {
		if f.Package == nil {
			continue
		}
		path := basePkgPath(f.Package.PkgPath)
		if path == "" {
			continue
		}
		pkg := index[path]
		pkg.path = path
		if pkg.name == "" && f.Package.Name != "" {
			pkg.name = f.Package.Name
		}
		if f.PackageIsRoot {
			pkg.focus = true
		}
		index[path] = pkg
	}
	return index
}

// matchLoadedPackages returns the loaded packages a go-src symbol refers
// to as a package: an exact base import path match, or an exact declared
// package name match. A path match is unique by construction; a name may
// match several packages, all returned in deterministic path order.
// See TheoryOfGoSrcResolution.
func matchLoadedPackages(symbol string, index map[string]loadedPackage) []loadedPackage {
	if pkg, ok := index[symbol]; ok {
		return []loadedPackage{pkg}
	}
	var matches []loadedPackage
	for _, path := range slices.Sorted(maps.Keys(index)) {
		if index[path].name == symbol {
			matches = append(matches, index[path])
		}
	}
	return matches
}

// renderGoSrcPackageDoc runs go doc -all -cmd for the package and wraps
// the output with source package markers: -all documents every
// declaration, not only the top-level summary, and -cmd documents a main
// package. A focus package adds -u to include unexported symbols, giving
// the model the complete surface of the packages it edits, while a
// context package's exported API surface suffices. The invocation and
// its read-only environment are shared with the visibility system via
// goDocOutput. See TheoryOfGoDocReadonly and TheoryOfGoSrcResolution.
func renderGoSrcPackageDoc(pkgPath string, focus bool, dir string, envs []string) (string, error) {
	text, err := goDocOutput(pkgPath, dir, envs, focus)
	if err != nil {
		return "", err
	}
	return "``` begin of source package " + pkgPath + "\n" +
		text +
		"``` end of source package " + pkgPath + "\n\n", nil
}

// appendPackageDocParts appends the go doc documentation parts for the
// loaded packages a symbol refers to as a package, reporting whether the
// symbol was a package reference. A failed go doc yields an explicit
// error part for that package rather than an abort, giving the model a
// concrete correction target. See TheoryOfGoSrcResolution.
func appendPackageDocParts(
	parts []generators.Part,
	symbol string,
	pkgIndex map[string]loadedPackage,
	renderedPkgs map[string]bool,
	docDir string,
	envs []string,
) ([]generators.Part, bool) {
	pkgs := matchLoadedPackages(symbol, pkgIndex)
	if len(pkgs) == 0 {
		return parts, false
	}
	for _, pkg := range pkgs {
		if renderedPkgs[pkg.path] {
			continue
		}
		renderedPkgs[pkg.path] = true
		doc, err := renderGoSrcPackageDoc(pkg.path, pkg.focus, docDir, envs)
		if err != nil {
			parts = append(parts, generators.Text(fmt.Sprintf(
				"[go-src: package %q documentation unavailable: %v]\n\n", pkg.path, err)))
			continue
		}
		parts = append(parts, generators.Text(doc))
	}
	return parts, true
}

// symbolDeclaration is one resolved declaration: its package-qualified
// name, the defining file, the 1-based line where the declaration (or its
// doc comment) starts, and the declaration source including doc comments.
// typeName and name record the declaration's receiver type and declaration
// names; startOffset and endOffset delimit the source's byte range within
// the file. go-src reference reporting maps the byte range — stable across
// distinct FileSets — onto the type-checked load's AST, name-checked with
// typeName and name. See TheoryOfGoSrcReferences.
type symbolDeclaration struct {
	qualifiedName string
	typeName      string
	name          string
	filePath      string
	line          int
	source        string
	startOffset   int
	endOffset     int
}

// findSymbolDeclarations searches the loaded Go files for declarations
// matching the symbol, returning every match across packages in the
// deterministic file order of the loaded file set. The symbol follows
// the go doc form [<pkg>.][<sym>.][<methodOrField>]: splitGoDocSymbol
// strips an optional package qualifier (an import path suffix or the
// declared package name), then splitSymbolName splits the remaining
// type/method parts. When the qualified interpretation matches nothing,
// the qualifier segment is retried as a receiver type name, so a package
// name that shadows a type name (graphemes vs Graphemes) does not hide
// the type's methods. See TheoryOfGoSrcResolution.
func findSymbolDeclarations(files []*File, symbol string) []symbolDeclaration {
	pkgFilter, typeName, name := splitGoDocSymbol(files, symbol)
	if matches := matchSymbolDeclarations(files, pkgFilter, typeName, name); len(matches) > 0 || pkgFilter == "" {
		return matches
	}
	// Retry with the qualifier segment as the receiver type: the primary
	// parse stripped it as a package qualifier (path suffix or package
	// name), but it may name a type instead.
	typeName, name = splitSymbolName(symbol)
	return matchSymbolDeclarations(files, "", typeName, name)
}

// matchSymbolDeclarations scans the loaded files for declarations
// matching the parsed symbol form, restricted to pkgFilter when it is
// non-empty.
func matchSymbolDeclarations(files []*File, pkgFilter, typeName, name string) []symbolDeclaration {
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
// file's cached content and appends a symbolDeclaration to matches. The
// declaration names and the byte-offset range of the extraction are
// recorded for go-src reference reporting. See TheoryOfGoSrcReferences.
func appendSymbolMatch(matches []symbolDeclaration, f *File, pkgPath, typeName, name string, start, end token.Pos) []symbolDeclaration {
	qualified := pkgPath + "." + name
	if typeName != "" {
		qualified = pkgPath + "." + typeName + "." + name
	}
	return append(matches, symbolDeclaration{
		qualifiedName: qualified,
		typeName:      typeName,
		name:          name,
		filePath:      f.Path,
		line:          f.TokenFile.Line(start),
		source:        string(f.Content[f.TokenFile.Offset(start):f.TokenFile.Offset(end)]),
		startOffset:   f.TokenFile.Offset(start),
		endOffset:     f.TokenFile.Offset(end),
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

// splitGoDocSymbol splits a go doc symbol form into its package filter
// and its type/method name parts. The filter is the longest qualifier
// that prefixes the symbol followed by a dot; a qualifier is either a
// suffix of a loaded package's import path (e.g., for path a/b/c, the
// suffixes tried are a/b/c, b/c, c) or a loaded package's declared name,
// so packages whose name is not a path segment — major-version import
// paths such as …/doublestar/v4 declaring "doublestar" — remain
// addressable as pkgName.Symbol. The filter records the full package
// path so the file loop compares exact paths. The remainder after
// stripping is split by splitSymbolName, which handles the * receiver
// prefix and generic parameter lists. See TheoryOfGoSrcResolution.
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
		// The declared package name qualifies symbols like a path
		// suffix. Longest-match-wins applies across both qualifier
		// kinds, so a longer path suffix of another package still
		// takes precedence over a package name.
		if pkgName := f.Package.Name; pkgName != "" &&
			strings.HasPrefix(symbol, pkgName+".") && len(pkgName) > bestPrefixLen {
			bestPkg = p
			bestPrefixLen = len(pkgName)
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
