package main

import (
	"strings"

	"github.com/reusee/tai/blocks"
	"github.com/reusee/tai/components"
	"github.com/reusee/tai/flags"
	"github.com/reusee/tai/memories"
)

const TheoryOfAIComponents = `
The ai command uses the Component mechanism for shell, continue, and memory
blocks. Shell and continue components are processed in the generation loop,
while memory blocks are processed after the loop by
memories.UpdateMemoryFromBlock. The memory component's prompt includes the
dynamic user profile text, read at Component construction time (provider
resolution) rather than at prompt assembly time; this is equivalent to the
prior inline approach because the profile is read once before generation
starts. BlockFormatSystemPrompt is a prompt-only Component that teaches the
model the boundary-delimited format used by memory blocks.

The base AI assistant prompt text and the config-derived ExtraSystemPrompt are
also prompt-only Components, unifying all system prompt contributions under the
Component framework. AISystemPrompt assembles only the dynamic current time,
which must be computed at call time and cannot be a static Component.

Shell and continue components are reused from components.CommonComponents, the
shared component set constructed in the components package. The codes module
also reuses CommonComponents, prepending its codes-specific components (change,
go-test, finish, request-context) and appending summary, read-only files,
mandatory planning, and extra system prompt. This eliminates the duplicate
component construction that previously existed when the ai command and codes
module each defined their own shell and continue components independently.

AIComponents is a distinct named type embedding components.ComponentSet so that
dscope resolves it independently from the codes module's CodesComponents
provider. Both the ai command and the codes module use distinct named types
embedding components.ComponentSet, ensuring each module's components are
resolved independently in the dscope scope without type conflicts.
`

// baseAISystemPrompt is the base AI assistant prompt text, now a prompt-only
// Component in AIComponents rather than a direct concatenation in
// AISystemPrompt. See TheoryOfAIComponents.
const baseAISystemPrompt = `你是一个很有用的AI助手。
在与用户交流时，输出易于阅读的文本，避免使用markdown格式，不要加入任何表示格式的符号，避免生成表格。`

// AIComponents is the component set type for the ai command. It embeds
// components.ComponentSet as an anonymous struct field so that dscope can
// resolve it independently from the codes module's CodesComponents, avoiding
// a type conflict when both providers are wired into the same scope. Method
// promotion eliminates the need for explicit delegation methods.
// See TheoryOfAIComponents.
type AIComponents struct {
	components.ComponentSet
}

func (Module) AIComponents(
	flagShell flags.Shell,
	currentMemory memories.CurrentMemory,
	extra ExtraSystemPrompt,
	noMemory NoMemory,
) (ret AIComponents) {
	var comps components.ComponentSet

	// Base AI assistant prompt: prompt-only Component for unified prompt
	// assembly. Previously prepended directly in AISystemPrompt.
	// See TheoryOfAIComponents.
	comps = append(comps, components.Component{
		PromptSection: baseAISystemPrompt,
	})

	// BlockFormatSystemPrompt is a prompt-only Component that teaches the
	// model the boundary-delimited block format used by memory blocks.
	comps = append(comps, components.Component{
		PromptSection: blocks.BlockFormatSystemPrompt,
	})

	// Memory component: the prompt includes the dynamic user profile text,
	// read at construction time (provider resolution). Processing is done
	// post-loop in ai.go via memories.UpdateMemoryFromBlock, not in the
	// generation loop. See TheoryOfAIComponents.
	if !noMemory {
		var profileText string
		if entry, err := currentMemory(); err == nil && entry != nil {
			profileText = strings.Join(entry.Items, "\n")
		}
		comps = append(comps, components.Component{
			Kind:           "memory",
			PromptSection:  memoryBlockSystemPrompt(profileText),
			ProcessingPath: "post-loop (ai.go: UpdateMemoryFromBlock)",
		})
	}

	// Common components: shell (conditional on flagShell) and continue.
	// Reused from components.CommonComponents so that shell and continue
	// configuration is shared across all generation commands.
	// See TheoryOfCommonComponents in components/common_components.go.
	comps = append(comps, components.CommonComponents(bool(flagShell))...)

	// Extra system prompt from configuration: prompt-only Component.
	// Previously appended directly in AISystemPrompt. Now unified under
	// the Component framework. See TheoryOfAIComponents.
	if string(extra) != "" {
		comps = append(comps, components.Component{
			PromptSection: string(extra),
		})
	}

	ret.ComponentSet = comps
	return
}

// memoryBlockSystemPrompt constructs the memory block prompt section with the
// user profile text. The prompt teaches the model how to emit memory blocks
// and includes the current user profile for context. The profile text is read
// at Component construction time and embedded as a static string in the
// PromptSection. See TheoryOfAIComponents.
func memoryBlockSystemPrompt(profileText string) string {
	return `
在每一轮对话中，你的任务流程如下：
1. 首先，根据现有的用户画像，生成对用户当前输入的回应。这是你的首要任务。
2. 在回应之后，仔细分析用户的最新输入，判断其中是否包含任何可以用来补充、修正或深化现有用户画像的新信息。
3. 如果发现了新信息，请生成一个记忆更新块（memory block）。不要将记忆更新块的内容混入常规回复中。记忆更新块的格式为：

:::<boundary> <memory>
<memory>
  <memory-item>用户画像项1</memory-item>
  <memory-item>用户画像项2</memory-item>
</memory>
:::<boundary> </memory>

其中 <boundary> 是一个随机字符串，确保不会与内容冲突。你只需要提供你认为是当前最准确和相关的用户画像项。系统会自动将你的输入与现有记录合并，不会意外删除任何旧信息。

- 如果没有发现任何新信息，则不要生成此块。
- 在提取和记录信息时，坚持高度确定性的事实原则：仅记录用户在对话中明确表达的事实，严禁记录任何缺乏根据的主观推测、直觉判断或过度推论。
- 特别注意：用户询问某个话题并不代表该话题发生在用户身上。例如，用户询问手术相关信息，仅代表用户关心此话题，不代表用户本人进行了手术。严禁将用户的兴趣或咨询内容错误地记录为用户的个人经历或状态。宁愿保持简洁的画像，也不要加入未经验证的猜测。

用户画像对于理解用户和提供个性化回应至关重要，因此请在每一轮对话中都认真执行这个评估过程。

用户画像：
` + profileText
}
