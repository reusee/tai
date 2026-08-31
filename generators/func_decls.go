package generators

import (
	"slices"

	"cuelang.org/go/cue"
	"github.com/reusee/tai/configs"
)

type FuncDecls []FuncDecl

func (Module) FuncDecls() FuncDecls {
	return nil
}

var _ configs.Config = FuncDecls{}

func (f FuncDecls) ConfigPaths() []string {
	return []string{"functions"}
}

// HandleConfig aggregates function declarations from all config values,
// deduplicating them by name and keeping the first occurrence: config roots
// are ordered from most local to most global, so the first occurrence is
// the most local definition. See TheoryOfPrefixCaching.
func (f FuncDecls) HandleConfig(path string, values []*cue.Value) (any, error) {
	ret := slices.Clone(f)
	for _, value := range values {
		var decls FuncDecls
		if err := value.Decode(&decls); err != nil {
			return nil, err
		}
		ret = append(ret, decls...)
	}
	// Deduplicate by name: duplicate tool declarations would waste prompt
	// tokens and may be rejected by model APIs.
	seen := make(map[string]struct{}, len(ret))
	deduped := ret[:0]
	for _, decl := range ret {
		if _, ok := seen[decl.Name]; ok {
			continue
		}
		seen[decl.Name] = struct{}{}
		deduped = append(deduped, decl)
	}
	return &deduped, nil
}
