package main

import (
	_ "embed"
	"strings"
	"time"

	"github.com/reusee/tai/anytexts"
	"github.com/reusee/tai/cmds"
	"github.com/reusee/tai/logs"
	"github.com/reusee/tai/prompts"
)

type SystemPrompt string

//go:embed system_prompt.md
var systemPrompt string

var (
	focus  []string
	ignore []string
)

func init() {
	cmds.Define("-focus", cmds.Func(func(what string) {
		focus = append(focus, what)
	}).Desc("add focus"))
	cmds.Define("-ignore", cmds.Func(func(what string) {
		ignore = append(ignore, what)
	}).Desc("add ignore"))
}

func (Module) SystemPrompt(
	codeProvider anytexts.CodeProvider,
	logger logs.Logger,
) (ret SystemPrompt) {

	location, err := time.LoadLocation("Asia/Hong_Kong")
	ce(err)
	now := time.Now().In(location)
	ret += SystemPrompt("\n\n当前北京时间是：" + now.Format("2006-01-02 15:04:05") + "\n\n")

	ret += SystemPrompt(systemPrompt)

	hasGoFiles := false
	for info, err := range codeProvider.IterFiles() {
		ce(err)
		if strings.HasSuffix(info.Path, ".go") {
			hasGoFiles = true
			break
		}
	}
	if hasGoFiles {
		logger.Info("has go file")
		ret += "\n\n" + prompts.UnifiedDiff + "\n\n"
	}

	//@@ai optimize

	if len(focus) > 0 {
		ret += "\n\n专注于这些方面：\n"
		for _, what := range focus {
			ret += "- " + SystemPrompt(what) + "\n"
		}
	}

	if len(ignore) > 0 {
		ret += "\n\n忽略这些方面：\n"
		for _, what := range ignore {
			ret += "- " + SystemPrompt(what) + "\n"
		}
	}

	return ret
}
