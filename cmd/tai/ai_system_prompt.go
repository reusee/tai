package main

import (
	"time"

	"github.com/reusee/tai/cmds"
)

var noMemory = cmds.Switch("-no-memory")

type AISystemPrompt func() (string, error)

func (Module) AISystemPrompt(
	comps AIComponents,
) AISystemPrompt {
	return func() (ret string, err error) {
		// All system prompt contributions — base text, block format, memory,
		// shell, continue, and extra prompt — are now unified as Components
		// in AIComponents. Only the dynamic current time remains here
		// because it must be computed at call time.
		// See TheoryOfAIComponents.
		ret = comps.PromptSections() + comps.RestatePrompts()

		location, err := time.LoadLocation("Asia/Hong_Kong")
		if err != nil {
			return "", err
		}
		ret += "\n当前北京时间：" + time.Now().In(location).Format("2006-01-02 15:04:05") + "\n"

		return
	}
}
