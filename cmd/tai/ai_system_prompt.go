package main

import (
	"time"

	"github.com/reusee/tai/flags"
)

type NoMemory bool

func (Module) NoMemory() NoMemory {
	return false
}

var _ flags.Flag = NoMemory(true)

func (n NoMemory) Handle(key string, args []string) (newDef any, remainArgs []string, err error) {
	ret := NoMemory(true)
	return &ret, args, nil
}

func (n NoMemory) Keys() map[string]string {
	return map[string]string{
		"-no-memory": "Disable user profile memory persistence",
		"-no-mem":    "Alias for -no-memory: disable user profile memory persistence",
	}
}

type AISystemPrompt func() (string, error)

func (Module) AISystemPrompt(
	comps AIComponents,
) AISystemPrompt {
	return func() (ret string, err error) {
		// All system prompt contributions — base text, block format, memory,
		// shell, continue, and extra prompt — are unified as Components
		// in AIComponents. The system prompt carries no reminder section:
		// the late reminder is the verbatim system prompt restate
		// (components.SystemPromptRestate), appended at the end of the
		// user prompt. Only the dynamic current time remains here because
		// it must be computed at call time. PromptSections joins its
		// sections with blank lines and ends with a blank line, so the
		// time is appended directly without an extra leading newline.
		// See TheoryOfAIComponents.
		ret = comps.PromptSections()

		location, err := time.LoadLocation("Asia/Hong_Kong")
		if err != nil {
			return "", err
		}
		ret += "当前北京时间：" + time.Now().In(location).Format("2006-01-02 15:04:05") + "\n"

		return
	}
}
