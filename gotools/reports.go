package gotools

import (
	"fmt"
	"go/types"
	"sort"
	"strings"

	"github.com/reusee/tai/generators"
)

// namedCandidate is one package-level named type of an indexed package,
// held as a candidate for the interface relations report: an interface
// candidate carries its underlying *types.Interface, a concrete candidate
// only its *types.TypeName. See TheoryOfGoSrcReferences.
type namedCandidate struct {
	qualified string
	typeName  *types.TypeName
	iface     *types.Interface
}

// collectInterfaceCandidates gathers the package-level named types and
// interfaces of the indexed packages as candidates for the interface
// relations report. Generic declarations are skipped on both sides —
// whether a generic type satisfies an interface is decidable only for an
// instantiation — and zero-method interfaces are skipped because every
// type satisfies them, which would make the report pure noise. Variants
// are skipped (they re-typecheck the same declarations), candidates
// deduplicate by qualified name, and both candidate slices are sorted so
// the report is deterministic. See TheoryOfGoSrcReferences.
func (index *typeCheckIndex) collectInterfaceCandidates() {
	seen := make(map[string]bool)
	for _, pkg := range index.pkgs {
		if pkg.Types == nil || pkg.Types.Scope() == nil {
			continue
		}
		if pkg.PkgPath != basePkgPath(pkg.PkgPath) {
			continue
		}
		pkgPath := basePkgPath(pkg.PkgPath)
		scope := pkg.Types.Scope()
		for _, name := range scope.Names() {
			tn, ok := scope.Lookup(name).(*types.TypeName)
			if !ok || tn.IsAlias() || tn.Type() == nil {
				continue
			}
			named, ok := tn.Type().(*types.Named)
			if !ok || named.TypeParams().Len() > 0 {
				continue
			}
			qualified := pkgPath + "." + name
			if seen[qualified] {
				continue
			}
			seen[qualified] = true
			if iface, ok := named.Underlying().(*types.Interface); ok {
				if iface.NumMethods() > 0 {
					index.ifaceCandidates = append(index.ifaceCandidates, namedCandidate{
						qualified: qualified,
						typeName:  tn,
						iface:     iface,
					})
				}
			} else {
				index.typeCandidates = append(index.typeCandidates, namedCandidate{
					qualified: qualified,
					typeName:  tn,
				})
			}
		}
	}
	sort.Slice(index.ifaceCandidates, func(i, j int) bool {
		return index.ifaceCandidates[i].qualified < index.ifaceCandidates[j].qualified
	})
	sort.Slice(index.typeCandidates, func(i, j int) bool {
		return index.typeCandidates[i].qualified < index.typeCandidates[j].qualified
	})
}

// receiverTypeNameFromType returns the base type name of a method's
// receiver type, unwrapping a pointer: a *types.Named yields its object's
// name, any other receiver type (an unnamed struct or interface) has no
// go doc-fetchable TypeName.MethodName form and yields "". See
// TheoryOfGoSrcReferences.
func receiverTypeNameFromType(t types.Type) string {
	if ptr, ok := t.(*types.Pointer); ok {
		t = ptr.Elem()
	}
	if named, ok := t.(*types.Named); ok && named.Obj() != nil {
		return named.Obj().Name()
	}
	return ""
}

// calleesFor collects the package-qualified symbols referenced inside the
// declaration's byte range — the out-edges of the fetch. Uses are grouped
// by file, so the scan touches one file's use list; objects declared
// inside the range itself (locals, parameters, results, receivers, labels,
// and the declaration's own name) are excluded, as are names without a
// fetchable form (see fetchableCalleeName). Byte offsets are computed
// through each use site's own token.File, never by comparing token.Pos
// across FileSets. See TheoryOfGoSrcReferences.
func (index *typeCheckIndex) calleesFor(decl symbolDeclaration) (callees []string, truncated bool) {
	seen := make(map[string]bool)
	for _, use := range index.usesByFile[decl.filePath] {
		off := use.loc.tf.Offset(use.ident.Pos())
		if off < decl.startOffset || off >= decl.endOffset {
			continue
		}
		// An object whose defining position lies inside the declaration's
		// own range is not an out-edge. Pointer equality of the defining
		// *token.File keeps the offset comparison within one FileSet.
		if objFile := use.loc.pkg.Fset.File(use.obj.Pos()); objFile == use.loc.tf {
			objOff := objFile.Offset(use.obj.Pos())
			if objOff >= decl.startOffset && objOff < decl.endOffset {
				continue
			}
		}
		name := index.fetchableCalleeName(use.obj)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		callees = append(callees, name)
	}
	sort.Strings(callees)
	if len(callees) > maxGoSrcReferencesPerSymbol {
		return callees[:maxGoSrcReferencesPerSymbol], true
	}
	return callees, false
}

// fetchableCalleeName renders an object as a go doc symbol name that a
// later go-src block can resolve, or "" when the object has no fetchable
// form. Package-name references and labels are not symbols; fields have no
// package-level name; universe objects and objects of packages outside the
// index (standard library, hidden, unloaded) are not go-src-resolvable, and
// reporting them would only invite not-found noise. Methods carry the
// TypeName.MethodName form; a top-level function lives in the package
// scope, while an interface method that reaches here without a named
// receiver is qualified through its declaring interface found among the
// interface candidates. See TheoryOfGoSrcReferences.
func (index *typeCheckIndex) fetchableCalleeName(obj types.Object) string {
	switch obj.(type) {
	case *types.PkgName, *types.Label:
		return ""
	}
	if v, ok := obj.(*types.Var); ok && v.IsField() {
		return ""
	}
	if obj.Pkg() == nil {
		return ""
	}
	pkgPath := basePkgPath(obj.Pkg().Path())
	if !index.pkgPaths[pkgPath] {
		return ""
	}
	if fn, ok := obj.(*types.Func); ok {
		if sig, ok := fn.Type().(*types.Signature); ok {
			if sig.Recv() != nil {
				if typeName := receiverTypeNameFromType(sig.Recv().Type()); typeName != "" {
					return pkgPath + "." + typeName + "." + fn.Name()
				}
			}
			if obj == obj.Pkg().Scope().Lookup(obj.Name()) {
				return pkgPath + "." + obj.Name()
			}
			if owner := index.methodOwnerInterface(fn); owner != "" {
				return pkgPath + "." + owner + "." + fn.Name()
			}
			return ""
		}
	}
	return pkgPath + "." + obj.Name()
}

// methodOwnerInterface finds the declaring interface of a method object
// among the interface candidates, matching by method name within the same
// package. Object identity cannot be used: the use site may be
// type-checked by a different package variant than the candidate, so the
// same declaration exists as distinct objects. See TheoryOfGoSrcReferences.
func (index *typeCheckIndex) methodOwnerInterface(fn *types.Func) string {
	if fn.Pkg() == nil {
		return ""
	}
	pkgPath := basePkgPath(fn.Pkg().Path())
	for _, cand := range index.ifaceCandidates {
		if basePkgPath(cand.typeName.Pkg().Path()) != pkgPath {
			continue
		}
		for i := 0; i < cand.iface.NumMethods(); i++ {
			if cand.iface.Method(i).Name() == fn.Name() {
				return cand.typeName.Name()
			}
		}
	}
	return ""
}

// interfaceRelationsFor reports the polymorphism relations of a resolved
// named type or interface: the interfaces a concrete type satisfies (via
// value or pointer method set), or the indexed concrete types implementing
// a fetched interface, with a leading * marking a pointer-only method set.
// Generic declarations are skipped on both sides; the candidates are
// pre-sorted, so the report is deterministic. Satisfaction is structural —
// method Ids — so candidates collected from a base package variant match
// objects resolved through any variant. See TheoryOfGoSrcReferences.
func (index *typeCheckIndex) interfaceRelationsFor(objects []types.Object) (lines []string, truncated bool) {
	var tn *types.TypeName
	for _, obj := range objects {
		if name, ok := obj.(*types.TypeName); ok && name.Type() != nil {
			tn = name
			break
		}
	}
	if tn == nil {
		return nil, false
	}
	named, ok := tn.Type().(*types.Named)
	if !ok || named.TypeParams().Len() > 0 {
		return nil, false
	}
	seen := make(map[string]bool)
	appendLine := func(line string) {
		if seen[line] {
			return
		}
		seen[line] = true
		lines = append(lines, line)
	}
	if iface, ok := named.Underlying().(*types.Interface); ok {
		for _, cand := range index.typeCandidates {
			concrete, ok := cand.typeName.Type().(*types.Named)
			if !ok || concrete.TypeParams().Len() > 0 {
				continue
			}
			if types.Implements(concrete, iface) {
				appendLine("implemented by " + cand.qualified)
			} else if types.Implements(types.NewPointer(concrete), iface) {
				appendLine("implemented by *" + cand.qualified)
			}
		}
	} else {
		ptr := types.NewPointer(named)
		for _, cand := range index.ifaceCandidates {
			if types.Implements(named, cand.iface) {
				appendLine("satisfies " + cand.qualified)
			} else if types.Implements(ptr, cand.iface) {
				appendLine("satisfies " + cand.qualified)
			}
		}
	}
	if len(lines) > maxGoSrcReferencesPerSymbol {
		return lines[:maxGoSrcReferencesPerSymbol], true
	}
	return lines, false
}

// formatCalleesPart renders the callees report that follows a resolved
// declaration's source part: one package-qualified symbol per line, with a
// truncation note when the report was capped. See TheoryOfGoSrcReferences.
func formatCalleesPart(qualified string, callees []string, truncated bool) generators.Part {
	var b strings.Builder
	fmt.Fprintf(&b, "``` begin of callees %s\n", qualified)
	for _, name := range callees {
		fmt.Fprintf(&b, "%s\n", name)
	}
	if truncated {
		fmt.Fprintf(&b, "... truncated at %d callees\n", maxGoSrcReferencesPerSymbol)
	}
	fmt.Fprintf(&b, "``` end of callees %s\n\n", qualified)
	return generators.Text(b.String())
}

// formatInterfaceRelationsPart renders the interface relations report that
// follows a resolved source part: one "satisfies" or "implemented by" line
// per relation, with a truncation note when the report was capped. See
// TheoryOfGoSrcReferences.
func formatInterfaceRelationsPart(qualified string, lines []string, truncated bool) generators.Part {
	var b strings.Builder
	fmt.Fprintf(&b, "``` begin of interface relations %s\n", qualified)
	for _, line := range lines {
		fmt.Fprintf(&b, "%s\n", line)
	}
	if truncated {
		fmt.Fprintf(&b, "... truncated at %d relations\n", maxGoSrcReferencesPerSymbol)
	}
	fmt.Fprintf(&b, "``` end of interface relations %s\n\n", qualified)
	return generators.Text(b.String())
}
