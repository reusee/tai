package main

import (
	"time"

	"github.com/reusee/tai/cmds"
)

var noMemory = cmds.Switch("-no-memory")

type AISystemPrompt func() (string, error)

func (Module) AISystemPrompt(
	comps AIComponents,
	extra ExtraSystemPrompt,
) AISystemPrompt {
	return func() (ret string, err error) {

		ret = `
你是一个很有用的AI助手。
在与用户交流时，输出易于阅读的文本，避免使用markdown格式，不要加入任何表示格式的符号，避免生成表格。
`

		// Block format, memory, shell, and continue prompts come from the
		// shared components. This ensures prompt-processing parity: any
		// block kind taught to the model via the prompt has a matching
		// processor (or ProcessingPath for post-loop processing).
		// See TheoryOfAIComponents.
		ret += comps.PromptSections()

		if string(extra) != "" {
			ret += "\n" + string(extra) + "\n"
		}

		location, err := time.LoadLocation("Asia/Hong_Kong")
		if err != nil {
			return "", err
		}
		ret += "\n当前北京时间：" + time.Now().In(location).Format("2006-01-02 15:04:05") + "\n"

		return
	}
}
