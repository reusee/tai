package flags

import (
	"fmt"

	"cuelang.org/go/cue"
	"github.com/reusee/tai/configs"
)

// ThoughtsSummarizeLanguage controls the output language for thought
// summaries. When empty (the default), no language hint is given to the
// summarizer. When set (e.g., "zh", "en"), the summarizer is instructed
// to output summaries in that language. It can be configured via the
// -thoughts-summarize-language flag or the "thoughts_summarize_language"
// config path.
type ThoughtsSummarizeLanguage string

func (Module) ThoughtsSummarizeLanguage() ThoughtsSummarizeLanguage {
	return ThoughtsSummarizeLanguage("")
}

var _ Flag = ThoughtsSummarizeLanguage("")

func (l ThoughtsSummarizeLanguage) Handle(key string, args []string) (newDef any, remainArgs []string, err error) {
	if len(args) == 0 {
		return nil, nil, fmt.Errorf("expecting language string, got empty")
	}
	ret := ThoughtsSummarizeLanguage(args[0])
	return &ret, args[1:], nil
}

func (l ThoughtsSummarizeLanguage) Keys() map[string]string {
	return map[string]string{
		"-thoughts-summarize-language": "Set the language for thought summaries (e.g., zh, en)",
	}
}

// configs.Config implementation. See TheoryOfConfigFlagParity.

var _ configs.Config = ThoughtsSummarizeLanguage("")

func (l ThoughtsSummarizeLanguage) ConfigPaths() []string {
	return []string{"thoughts_summarize_language"}
}

func (l ThoughtsSummarizeLanguage) HandleConfig(path string, values []*cue.Value) (any, error) {
	s, err := values[0].String()
	if err != nil {
		return nil, err
	}
	ret := ThoughtsSummarizeLanguage(s)
	return &ret, nil
}
