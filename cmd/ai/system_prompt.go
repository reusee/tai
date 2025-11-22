package main

import (
	"strings"
	"time"

	"github.com/reusee/tai/cmds"
)

var noMemory = cmds.Switch("-no-memory")

type GetSystemPrompt func() (string, error)

func (Module) GetSystemPrompt(
	currentMemory CurrentMemory,
) GetSystemPrompt {
	return func() (ret string, err error) {

		ret = `
你是一个很有用的AI助手。
输出易于阅读的文本，避免使用markdown格式，不要加入任何表示格式的符号，避免生成表格。
`

		if !*noMemory {

			ret += `
在每一轮对话中，你的任务流程如下：
1. 首先，根据现有的用户画像，生成对用户当前输入的回应。这是你的首要任务。
2. 在回应之后，仔细分析用户的最新输入，判断其中是否包含任何可以用来补充、修正或深化现有用户画像的新信息。
3. 如果发现了新信息，就调用 set_user_profile 工具来更新用户画像。
   - 更新时，必须提供一个完整的用户画像，包含所有旧信息和新信息，而不是只提供增量变化。
   - 如果没有发现任何新信息，则不要调用 set_user_profile 工具。
用户画像对于理解用户和提供个性化回应至关重要，因此请在每一轮对话中都认真执行这个评估过程。
用户画像需要尽可能详细。
`

			memoryEntry, err := currentMemory()
			if err != nil {
				return "", err
			}
			var text string
			if memoryEntry != nil {
				text = strings.Join(memoryEntry.Items, "\n")
			}

			ret += `
用户画像：
` + text
		}

		location, err := time.LoadLocation("Asia/Hong_Kong")
		if err != nil {
			return "", err
		}
		ret += "\n当前北京时间：" + time.Now().In(location).Format("2006-01-02 15:04:05") + "\n"

		return
	}
}
