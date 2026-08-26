package gotools

import (
	"cmp"
	"errors"
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"slices"
	"sort"
	"strings"
	"sync"

	"github.com/reusee/tai/generators"
	"github.com/reusee/tai/logs"
	"golang.org/x/tools/go/packages"
)

const TheoryOfGoSrcReferences = `
go-src reference reporting: alongside a resolved declaration's source, the
resolver reports every reference to the symbol within the loaded packages
— package path, enclosing top-level declaration, file — so the model
can judge blast radius and trace callers without another fetch. References
are reported per top-level declaration, not per use: the report guides
model exploration, the model explores with top-level definition names, and
further uses inside a declaration already listed add no information.

References need type information the context loader deliberately omits
(see TheoryOfLightweightPackageLoading), so they come from a separate
on-demand packages.Load with NeedTypes, NeedTypesInfo, and NeedSyntax,
cached per scope by the TypeCheckedPackages provider: the load runs at
most once per session, only when a go-src symbol is actually resolved,
and a load failure degrades to source-only output rather than aborting
the resolve.

Object identity is per type-checked load, and the same declaration is
re-typechecked by every package variant that contains its file (a package
and its test variant), so a resolved symbolDeclaration maps to
types.Objects by file path and byte-offset overlap — byte offsets are
stable across distinct FileSets — and every variant's object contributes
references. References are collected from TypesInfo.Uses, normalized
through Origin so generic instantiations fold onto the declared method or
function, deduplicated by the referencing top-level declaration within a
file, sorted by file and declaration name, and never truncated: a complete
blast-radius list is the point of the report, and a cap would hide a
caller a change would break.
Standard-library dependencies, hidden packages, and the go tool's
synthesized test-binary packages ("<path>.test": binary mains with no
real source, only the generated _testmain.go, whose references would be
noise) are excluded from the index, matching the context loader's
boundaries (see TheoryOfHiddenPackages and TheoryOfStdLibExclusion).

The selector packages report lists the imported packages referenced via
selector expressions (pkg.Name) within the declaration's byte range,
mapped to their full import paths. The model sees the declaration's source
with import aliases, and this report resolves each alias used in a
selector to its complete import path without the model scanning the
import block, so follow-up go-src blocks can use the full path as the
package qualifier directly.

Interface relations are the third report: fetching a named type lists the
indexed interfaces it satisfies (value or pointer method set), and
fetching an interface lists the indexed concrete types implementing it,
with a leading * marking a pointer-only method set. Candidates are the
package-level named types of the indexed packages, collected once per
index and sorted for determinism; generics are skipped on both sides
(satisfaction is decidable only for an instantiation) and zero-method
interfaces are skipped (every interface satisfies them). Satisfaction is
structural — method Ids — so candidates collected from a base package
variant match objects resolved through any variant.
`

// GetTypeCheckedPackages returns the packages of the loaded graph with full
// type information, for go-src reference reporting. It is a separate load
// from the context loader (which deliberately omits types), cached per scope
// so it runs at most once per session and only when a go-src symbol is
// resolved. See TheoryOfGoSrcReferences.
type GetTypeCheckedPackages func() ([]*packages.Package, error)

// TypeCheckedPackages provider: loads the root and context patterns with
// NeedTypes, NeedTypesInfo, and NeedSyntax, walks the import graph so
// dependencies type-checked from source are indexed too, and excludes
// standard-library dependencies, hidden packages, and the go tool's
// synthesized test-binary packages, matching the context loader's
// boundaries. See TheoryOfGoSrcReferences.
func (Module) TypeCheckedPackages(
	noTests NoTests,
	envs Envs,
	logger logs.Logger,
	loadDir LoadDir,
	loadPatterns LoadPatterns,
	contextPatterns ContextPatterns,
	workspace Workspace,
	hidden HiddenPatterns,
) GetTypeCheckedPackages {
	return sync.OnceValues(func() ([]*packages.Package, error) {
		dir, patterns, loadEnvs := resolveLoadContext(loadDir, loadPatterns, workspace, envs)
		config := &packages.Config{
			Mode: packages.NeedName |
				packages.NeedFiles |
				packages.NeedImports |
				packages.NeedDeps |
				packages.NeedForTest |
				packages.NeedModule |
				packages.NeedTypes |
				packages.NeedTypesInfo |
				packages.NeedSyntax,
			Tests: !bool(noTests),
			Env:   loadEnvs,
			Dir:   dir,
		}
		pkgs, err := packages.Load(config, patterns...)
		if err != nil {
			return nil, err
		}
		if len(contextPatterns) > 0 {
			ctxPkgs, err2 := packages.Load(config, contextPatterns...)
			if err2 != nil {
				return nil, errors.Join(err, err2)
			}
			pkgs = append(pkgs, ctxPkgs...)
		}
		roots := make(map[*packages.Package]bool, len(pkgs))
		for _, pkg := range pkgs {
			roots[pkg] = true
		}
		// Walk the import graph so dependencies type-checked from source are
		// indexed too: a go-src symbol may be declared in a dependency while
		// its references live in the loaded project packages. See
		// TheoryOfGoSrcReferences.
		seen := make(map[*packages.Package]bool)
		var all []*packages.Package
		var collect func(*packages.Package)
		collect = func(pkg *packages.Package) {
			if pkg == nil || seen[pkg] {
				return
			}
			seen[pkg] = true
			all = append(all, pkg)
			for _, imp := range pkg.Imports {
				collect(imp)
			}
		}
		for _, pkg := range pkgs {
			collect(pkg)
		}
		isHidden := newHiddenPackageMatcher(hidden)
		kept := all[:0]
		for _, pkg := range all {
			if pkg.Module == nil && !roots[pkg] {
				// Standard-library dependency: never in the context file set,
				// so never a go-src declaration site, unless explicitly
				// requested via -pkg or -ctx (roots). See TheoryOfStdLibExclusion.
				continue
			}
			if isHidden != nil && isHidden(basePkgPath(pkg.PkgPath)) {
				continue
			}
			// The go tool synthesizes a test-binary main package ("<path>.test")
			// for every tested package. It has no real source — only the
			// generated _testmain.go — so references reported from it are noise:
			// exclude it from the reference index. See TheoryOfGoSrcReferences.
			if strings.HasSuffix(basePkgPath(pkg.PkgPath), ".test") {
				continue
			}
			kept = append(kept, pkg)
		}
		slices.SortStableFunc(kept, func(a, b *packages.Package) int {
			return cmp.Compare(a.PkgPath, b.PkgPath)
		})
		logger.Info("type-checked packages for go-src references", "count", len(kept))
		return kept, nil
	})
}

// indexedSyntaxFile is one syntax file of the type-checked load: the package
// that type-checked it, its AST, and its token.File.
type indexedSyntaxFile struct {
	pkg  *packages.Package
	file *ast.File
	tf   *token.File
}

// indexedUse is one use site recorded in TypesInfo.Uses. The used object is
// carried alongside so the reference index can classify the use without a
// second lookup. See TheoryOfGoSrcReferences.
type indexedUse struct {
	loc   *indexedSyntaxFile
	ident *ast.Ident
	obj   types.Object
}

// symbolReference is one reference to a resolved symbol: the referencing
// package, the file, and the top-level declaration containing the use.
// One entry is emitted per referencing top-level declaration.
type symbolReference struct {
	pkgPath string
	topDecl string
	file    string
}

// useKey deduplicates references at the granularity of the referencing
// top-level declaration: the file name plus the declaration name. The
// report guides model exploration, and the model explores with top-level
// definition names, so repeated uses inside one declaration carry no new
// information. The key also collapses package variants that re-typecheck
// the same file.
type useKey struct {
	file    string
	topDecl string
}

// typeCheckIndex indexes the type-checked load for go-src reference
// reporting: syntax files by token.File and by path, use sites grouped by
// the used object (the in-edge direction), the indexed package paths, and
// the named-type candidates for the interface relations report. See
// TheoryOfGoSrcReferences.
type typeCheckIndex struct {
	byToken map[*token.File]*indexedSyntaxFile
	byName  map[string][]*indexedSyntaxFile
	uses    map[types.Object][]indexedUse

	pkgs            []*packages.Package
	pkgPaths        map[string]bool
	ifaceCandidates []namedCandidate
	typeCandidates  []namedCandidate
}

// buildTypeCheckIndex builds the reference index over the type-checked
// packages: every syntax file is indexed by its token.File and path, and
// every TypesInfo.Uses entry is grouped under its (normalized) object.
// The usable packages, their base import paths, and their named-type
// candidates feed the selector packages and interface relations reports.
// See TheoryOfGoSrcReferences.
func buildTypeCheckIndex(pkgs []*packages.Package) *typeCheckIndex {
	index := &typeCheckIndex{
		byToken:  make(map[*token.File]*indexedSyntaxFile),
		byName:   make(map[string][]*indexedSyntaxFile),
		uses:     make(map[types.Object][]indexedUse),
		pkgPaths: make(map[string]bool),
	}
	for _, pkg := range pkgs {
		if pkg.TypesInfo == nil || pkg.Fset == nil {
			continue
		}
		index.pkgs = append(index.pkgs, pkg)
		index.pkgPaths[basePkgPath(pkg.PkgPath)] = true
		for _, f := range pkg.Syntax {
			if f == nil {
				continue
			}
			tf := pkg.Fset.File(f.Pos())
			if tf == nil {
				continue
			}
			loc := &indexedSyntaxFile{pkg: pkg, file: f, tf: tf}
			index.byToken[tf] = loc
			index.byName[tf.Name()] = append(index.byName[tf.Name()], loc)
		}
		for ident, obj := range pkg.TypesInfo.Uses {
			if ident == nil || obj == nil {
				continue
			}
			loc := index.byToken[pkg.Fset.File(ident.Pos())]
			if loc == nil {
				continue
			}
			obj = normalizeObject(obj)
			use := indexedUse{loc: loc, ident: ident, obj: obj}
			index.uses[obj] = append(index.uses[obj], use)
		}
	}
	index.collectInterfaceCandidates()
	return index
}

// normalizeObject folds a generic method or function instantiation onto the
// object of its declaration, so a use through an instantiation and the
// declaration's Defs entry compare equal. See TheoryOfGoSrcReferences.
func normalizeObject(obj types.Object) types.Object {
	if fn, ok := obj.(*types.Func); ok {
		if origin := fn.Origin(); origin != nil {
			return origin
		}
	}
	return obj
}

// objectsFor maps a resolved declaration to the type-checked objects that
// declare it. The declaration may be type-checked by every package variant
// containing its file, so all variants' objects are returned; referencesFor
// deduplicates the references they produce. See TheoryOfGoSrcReferences.
func (index *typeCheckIndex) objectsFor(decl symbolDeclaration) []types.Object {
	var objects []types.Object
	for _, loc := range index.byName[decl.filePath] {
		ident := declarationNameIdent(loc, decl)
		if ident == nil {
			continue
		}
		if obj := loc.pkg.TypesInfo.Defs[ident]; obj != nil {
			objects = append(objects, normalizeObject(obj))
		}
	}
	return objects
}

// declarationNameIdent locates the defining identifier of decl's declaration
// within one type-checked syntax file, by byte-offset overlap between the
// resolved source range and the file's declarations, plus name matching
// under go doc's case rule. See TheoryOfGoSrcReferences.
func declarationNameIdent(loc *indexedSyntaxFile, decl symbolDeclaration) *ast.Ident {
	for _, d := range loc.file.Decls {
		if !offsetRangeOverlaps(loc.tf, d.Pos(), d.End(), decl.startOffset, decl.endOffset) {
			continue
		}
		switch n := d.(type) {
		case *ast.FuncDecl:
			if n.Name == nil || !goDocNameMatch(decl.name, n.Name.Name) {
				continue
			}
			if decl.typeName != "" {
				if !goDocNameMatch(decl.typeName, receiverTypeName(n)) {
					continue
				}
			} else if receiverTypeName(n) != "" {
				// A plain name selects only top-level functions; methods
				// require the TypeName.MethodName form, matching
				// matchSymbolDeclarations.
				continue
			}
			return n.Name
		case *ast.GenDecl:
			if decl.typeName != "" {
				continue // methods never live in a GenDecl
			}
			for _, spec := range n.Specs {
				if !offsetRangeOverlaps(loc.tf, spec.Pos(), spec.End(), decl.startOffset, decl.endOffset) {
					continue
				}
				switch s := spec.(type) {
				case *ast.ValueSpec:
					for _, id := range s.Names {
						if id != nil && goDocNameMatch(decl.name, id.Name) {
							return id
						}
					}
				case *ast.TypeSpec:
					if s.Name != nil && goDocNameMatch(decl.name, s.Name.Name) {
						return s.Name
					}
				}
			}
		}
	}
	return nil
}

// offsetRangeOverlaps reports whether the byte range of [start, end) in tf
// overlaps the byte range [from, to). Byte offsets are stable across distinct
// FileSets, so ranges from the context loader's fset compare against the
// type-checked load's fset. See TheoryOfGoSrcReferences.
func offsetRangeOverlaps(tf *token.File, start, end token.Pos, from, to int) bool {
	s := tf.Offset(start)
	e := tf.Offset(end)
	return s < to && from < e
}

// referencesFor collects the deduplicated references to objects — one
// entry per referencing top-level declaration — sorted by file and
// declaration name. Reports are complete and never truncated; see
// TheoryOfGoSrcReferences.
func (index *typeCheckIndex) referencesFor(objects []types.Object) (refs []symbolReference) {
	seen := make(map[useKey]bool)
	for _, obj := range objects {
		for _, use := range index.uses[obj] {
			topDecl := enclosingTopLevelName(use.loc.file, use.ident.Pos())
			key := useKey{file: use.loc.tf.Name(), topDecl: topDecl}
			if seen[key] {
				continue
			}
			seen[key] = true
			refs = append(refs, symbolReference{
				pkgPath: basePkgPath(use.loc.pkg.PkgPath),
				topDecl: topDecl,
				file:    use.loc.tf.Name(),
			})
		}
	}
	sort.Slice(refs, func(i, j int) bool {
		if refs[i].file != refs[j].file {
			return refs[i].file < refs[j].file
		}
		return refs[i].topDecl < refs[j].topDecl
	})
	return refs
}

// selectorPackagesFor collects the imported packages referenced via
// selector expressions (pkg.Name) within the declaration's byte range,
// mapped to their full import paths. The report lets the model use the
// full path as a package qualifier in follow-up go-src blocks without
// scanning the import block. See TheoryOfGoSrcReferences.
func (index *typeCheckIndex) selectorPackagesFor(decl symbolDeclaration) []string {
	seen := make(map[string]bool)
	for _, loc := range index.byName[decl.filePath] {
		if loc.pkg.TypesInfo == nil {
			continue
		}
		ast.Inspect(loc.file, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			off := loc.tf.Offset(sel.Pos())
			if off < decl.startOffset || off >= decl.endOffset {
				return true
			}
			ident, ok := sel.X.(*ast.Ident)
			if !ok {
				return true
			}
			obj := loc.pkg.TypesInfo.Uses[ident]
			if obj == nil {
				return true
			}
			pkgName, ok := obj.(*types.PkgName)
			if !ok {
				return true
			}
			if pkgName.Imported() == nil {
				return true
			}
			path := pkgName.Imported().Path()
			if path == "" || seen[path] {
				return true
			}
			seen[path] = true
			return true
		})
	}
	paths := make([]string, 0, len(seen))
	for path := range seen {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths
}

// enclosingTopLevelName names the top-level declaration containing pos:
// a function's name, a "Type.Method" form for methods, the first name of a
// value or type spec inside a GenDecl, the declaration keyword between
// specs, and "file-level" outside any declaration.
func enclosingTopLevelName(f *ast.File, pos token.Pos) string {
	for _, d := range f.Decls {
		if pos < d.Pos() || pos > d.End() {
			continue
		}
		switch n := d.(type) {
		case *ast.FuncDecl:
			if n.Name == nil {
				return "file-level"
			}
			if recv := receiverTypeName(n); recv != "" {
				return recv + "." + n.Name.Name
			}
			return n.Name.Name
		case *ast.GenDecl:
			for _, spec := range n.Specs {
				if pos < spec.Pos() || pos > spec.End() {
					continue
				}
				switch s := spec.(type) {
				case *ast.ValueSpec:
					if len(s.Names) > 0 && s.Names[0] != nil {
						return s.Names[0].Name
					}
				case *ast.TypeSpec:
					if s.Name != nil {
						return s.Name.Name
					}
				case *ast.ImportSpec:
					return "import"
				}
				return n.Tok.String()
			}
			return n.Tok.String()
		}
	}
	return "file-level"
}

// formatReferencesPart renders the references report that follows a resolved
// declaration's source part: one line per referencing top-level declaration
// as "package: top-level declaration (file)". See TheoryOfGoSrcReferences.
func formatReferencesPart(qualified string, refs []symbolReference) generators.Part {
	var b strings.Builder
	fmt.Fprintf(&b, "``` begin of references %s\n", qualified)
	for _, ref := range refs {
		fmt.Fprintf(&b, "%s: %s (%s)\n", ref.pkgPath, ref.topDecl, ref.file)
	}
	fmt.Fprintf(&b, "``` end of references %s\n\n", qualified)
	return generators.Text(b.String())
}

// formatSelectorPackagesPart renders the selector packages report that
// follows a resolved declaration's source part: one full import path per
// line for each package used as a selector prefix within the declaration.
// See TheoryOfGoSrcReferences.
func formatSelectorPackagesPart(qualified string, paths []string) generators.Part {
	var b strings.Builder
	fmt.Fprintf(&b, "``` begin of selector packages %s\n", qualified)
	for _, path := range paths {
		fmt.Fprintf(&b, "%s\n", path)
	}
	fmt.Fprintf(&b, "``` end of selector packages %s\n\n", qualified)
	return generators.Text(b.String())
}
