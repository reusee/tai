package gotools

import (
	"cuelang.org/go/cue"

	"github.com/reusee/tai/configs"
	"github.com/reusee/tai/flags"
)

var (
	_ configs.Config = AllSrc(false)
	_ flags.Flag     = AllSrc(true)
)

// AllSrc controls whether focus packages enter the initial context as
// full source code, including test files, instead of package
// documentation. When true, focus packages are pinned at VisibilityAll:
// every focus file is emitted at full content, the synthetic focus
// documentation block is not produced, and go-src blocks are unnecessary
// for focus declarations. The context budget still derives from the focus
// tokens at their pinned level. The overflow downgrade does not apply:
// the full-source pin is never downgraded to a documentation level, so an
// oversized focus surface proceeds oversized. See
// TheoryOfVisibilityAllocation.
type AllSrc bool

func (a AllSrc) ConfigPaths() []string {
	return []string{"go.all_src"}
}

func (a AllSrc) HandleConfig(path string, values []*cue.Value) (any, error) {
	var b bool
	if err := values[0].Decode(&b); err != nil {
		return nil, err
	}
	ret := AllSrc(b)
	return &ret, nil
}

func (a AllSrc) Handle(key string, args []string) (newDef any, remainArgs []string, err error) {
	ret := AllSrc(true)
	return &ret, args, nil
}

func (a AllSrc) Keys() map[string]string {
	return map[string]string{
		"-all-src": "Include full source of focus packages, including tests",
	}
}

func (Module) AllSrc() AllSrc {
	return false
}
