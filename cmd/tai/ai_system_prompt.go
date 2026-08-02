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
		// in AIComponents. Restate prompts are placed at the end of the user
		// prompt via ComponentSet.UserPromptParts(), not in the system prompt.
		// Only the dynamic current time remains here because it must be
		// computed at call time.
		// See TheoryOfAIComponents.
		ret = comps.PromptSections()

		location, err := time.LoadLocation("Asia/Hong_Kong")
		if err != nil {
			return "", err
		}
		ret += "\n当前北京时间：" + time.Now().In(location).Format("2006-01-02 15:04:05") + "\n"

		return
	}
}
