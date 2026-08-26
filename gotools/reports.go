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

// interfaceRelationsFor reports the polymorphism relations of a resolved
// named type or interface: the interfaces a concrete type satisfies (via
// value or pointer method set), or the indexed concrete types implementing
// a fetched interface, with a leading * marking a pointer-only method set.
// Generic declarations are skipped on both sides; the candidates are
// pre-sorted, so the report is deterministic. Satisfaction is structural —
// method Ids — so candidates collected from a base package variant match
// objects resolved through any variant. Reports are complete and never
// truncated. See TheoryOfGoSrcReferences.
func (index *typeCheckIndex) interfaceRelationsFor(objects []types.Object) (lines []string) {
	var tn *types.TypeName
	for _, obj := range objects {
		if name, ok := obj.(*types.TypeName); ok && name.Type() != nil {
			tn = name
			break
		}
	}
	if tn == nil {
		return nil
	}
	named, ok := tn.Type().(*types.Named)
	if !ok || named.TypeParams().Len() > 0 {
		return nil
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
	return lines
}

// formatInterfaceRelationsPart renders the interface relations report that
// follows a resolved source part: one "satisfies" or "implemented by" line
// per relation. See TheoryOfGoSrcReferences.
func formatInterfaceRelationsPart(qualified string, lines []string) generators.Part {
	var b strings.Builder
	fmt.Fprintf(&b, "``` begin of interface relations %s\n", qualified)
	for _, line := range lines {
		fmt.Fprintf(&b, "%s\n", line)
	}
	fmt.Fprintf(&b, "``` end of interface relations %s\n\n", qualified)
	return generators.Text(b.String())
}
