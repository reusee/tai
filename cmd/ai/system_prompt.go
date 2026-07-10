package main

import (
	"strings"
	"time"

	"github.com/reusee/tai/blocks"
	"github.com/reusee/tai/cmds"
	"github.com/reusee/tai/configs"
)

type ExtraSystemPrompt string

var _ExtraSystemPromptConfigurable configs.Configurable = ExtraSystemPrompt("")

func (e ExtraSystemPrompt) TaigoConfigurable() {}

func (Module) ExtraSystemPrompt(
	loader configs.Loader,
) ExtraSystemPrompt {
	return configs.First[ExtraSystemPrompt](loader, "extra_system_prompt")
}

var noMemory = cmds.Switch("-no-memory")

type GetSystemPrompt func() (string, error)

func (Module) GetSystemPrompt(
	currentMemory CurrentMemory,
	extra ExtraSystemPrompt,
) GetSystemPrompt {
	return func() (ret string, err error) {

		ret = `
你是一个很有用的AI助手。
在与用户交流时，输出易于阅读的文本，避免使用markdown格式，不要加入任何表示格式的符号，避免生成表格。
`

		if !*noMemory {

			ret += blocks.BlockFormatSystemPrompt

			ret += `
在每一轮对话中，你的任务流程如下：
1. 首先，根据现有的用户画像，生成对用户当前输入的回应。这是你的首要任务。
2. 在回应之后，仔细分析用户的最新输入，判断其中是否包含任何可以用来补充、修正或深化现有用户画像的新信息。
3. 如果发现了新信息，请生成一个记忆更新块（memory block）。不要将记忆更新块的内容混入常规回复中。记忆更新块的格式为：

:::memory <boundary>
<memory>
  <memory-item>用户画像项1</memory-item>
  <memory-item>用户画像项2</memory-item>
</memory>
:::end <boundary>

其中 <boundary> 是一个随机字符串，确保不会与内容冲突。你只需要提供你认为是当前最准确和相关的用户画像项。系统会自动将你的输入与现有记录合并，不会意外删除任何旧信息。

- 如果没有发现任何新信息，则不要生成此块。
- 在提取和记录信息时，坚持高度确定性的事实原则：仅记录用户在对话中明确表达的事实，严禁记录任何缺乏根据的主观推测、直觉判断或过度推论。
- 特别注意：用户询问某个话题并不代表该话题发生在用户身上。例如，用户询问手术相关信息，仅代表用户关心此话题，不代表用户本人进行了手术。严禁将用户的兴趣或咨询内容错误地记录为用户的个人经历或状态。宁愿保持简洁的画像，也不要加入未经验证的猜测。

用户画像对于理解用户和提供个性化回应至关重要，因此请在每一轮对话中都认真执行这个评估过程。
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
