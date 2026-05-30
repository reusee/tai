package codes

import (
	"github.com/reusee/dscope"
	"github.com/reusee/prompts"
	"github.com/reusee/tai/codes/codetypes"
	"github.com/reusee/tai/generators"
)

type UnifiedDiff struct {
}

var _ codetypes.DiffHandler = UnifiedDiff{}

func (u UnifiedDiff) Functions() []*generators.Function {
	return nil
}

func (u UnifiedDiff) SystemPrompt() string {
	return prompts.UnifiedDiff
}

func (u UnifiedDiff) RestatePrompt() string {
	return prompts.UnifiedDiffRestate
}

func (Module) UnifiedDiff(
	inject dscope.InjectStruct,
) (ret UnifiedDiff) {
	inject(&ret)
	return ret
}
