package flags

import (
	"fmt"

	"cuelang.org/go/cue"
	"github.com/reusee/tai/configs"
)

// SummaryLanguage controls the output language of the summary block.
// When empty (the default), no language hint is given to the model and
// the summary block prompt stays unchanged. When set (e.g., "zh", "en"),
// the summary block system prompt instructs the model to write the
// summary bullet items in that language. It can be configured via the
// -summary-language flag or the "summary_language" config path.
// See blocks.TheoryOfSummaryBlocks.
type SummaryLanguage string

func (Module) SummaryLanguage() SummaryLanguage {
	return SummaryLanguage("")
}

var _ Flag = SummaryLanguage("")

func (l SummaryLanguage) Handle(key string, args []string) (newDef any, remainArgs []string, err error) {
	if len(args) == 0 {
		return nil, nil, fmt.Errorf("expecting language string, got empty")
	}
	ret := SummaryLanguage(args[0])
	return &ret, args[1:], nil
}

func (l SummaryLanguage) Keys() map[string]string {
	return map[string]string{
		"-summary-language": "Set the language for summary block output (e.g., zh, en)",
	}
}

// configs.Config implementation. See TheoryOfConfigFlagParity.

var _ configs.Config = SummaryLanguage("")

func (l SummaryLanguage) ConfigPaths() []string {
	return []string{"summary_language"}
}

func (l SummaryLanguage) HandleConfig(path string, values []*cue.Value) (any, error) {
	s, err := values[0].String()
	if err != nil {
		return nil, err
	}
	ret := SummaryLanguage(s)
	return &ret, nil
}
