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
resolution) rather than at prompt assembly time. BlockFormatSystemPrompt is a
prompt-only Component that teaches the model the boundary-delimited block
format used by memory blocks.

The base AI assistant prompt text and the config-derived ExtraSystemPrompt are
prompt-only Components, unifying all system prompt contributions under the
Component framework. AISystemPrompt assembles only the dynamic current time,
which must be computed at call time.

The memory component is appended last, after the static shell/continue and
extra prompt components, so that the dynamic user profile text — which
changes across sessions as the profile accumulates — never shifts the
position of static system prompt sections. When the profile changes, only
the final memory section changes; the base, block-format, shell, continue,
and extra prompt sections remain byte-identical and fully cacheable. This
applies the dynamic-content-last principle to the system prompt; the same
principle places the current time at the end of the system prompt and the
user input at the end of the user prompt (see TheoryOfAiCommand). See
TheoryOfPrefixCaching in generators/state_func_map.go.

Shell and continue components are reused from components.CommonComponents.
AIComponents is a distinct named type embedding components.ComponentSet so that
dscope resolves it independently from the codes module's CodesComponents
provider.

RestatePrompts are included for the block format, memory, shell, and continue
components. Each RestatePrompt provides a short critical reminder that
reinforces the block format rules. Restate prompts are placed at the end of the
user prompt via ComponentSet.UserPromptParts(), not in the system prompt.
`

// baseAISystemPrompt is the base AI assistant prompt text, a prompt-only
// Component in AIComponents. See TheoryOfAIComponents.
const baseAISystemPrompt = `提供有用的帮助。
输出易于阅读的文本，避免使用markdown格式，不要加入任何表示格式的符号，避免生成表格。`

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
	extra flags.ExtraSystemPrompt,
	noMemory NoMemory,
) (ret AIComponents) {
	var comps components.ComponentSet

	// Base AI assistant prompt: prompt-only Component for unified prompt
	// assembly. See TheoryOfAIComponents.
	comps = append(comps, components.Component{
		PromptSection: baseAISystemPrompt,
	})

	// BlockFormatSystemPrompt is a prompt-only Component that teaches the
	// model the boundary-delimited block format used by memory blocks.
	// RestatePrompt reinforces the critical line-start and boundary
	// uniqueness rules at the end of the system prompt.
	// See TheoryOfAIComponents.
	comps = append(comps, components.Component{
		PromptSection: blocks.BlockFormatSystemPrompt,
		RestatePrompt: blocks.BlockFormatRestatePrompt,
	})

	// Common components: shell (conditional on flagShell) and continue.
	// Reused from components.CommonComponents so that shell and continue
	// configuration is shared across all generation commands.
	// See TheoryOfCommonComponents in components/common_components.go.
	comps = append(comps, components.CommonComponents(bool(flagShell))...)

	// Extra system prompt from configuration: prompt-only Component.
	// Each entry is added as a separate prompt-only Component so that
	// multiple config sources are all included.
	// See TheoryOfAIComponents.
	for _, prompt := range extra {
		if prompt != "" {
			comps = append(comps, components.Component{
				PromptSection: prompt,
			})
		}
	}

	// Memory component: appended last so the dynamic user profile text —
	// which changes across sessions as the profile accumulates — never
	// shifts the position of the static components above. When the profile
	// changes, only this final section changes; the base, block-format,
	// shell/continue, and extra prompt sections remain byte-identical and
	// fully cacheable. Processing is done post-loop in ai.go via
	// memories.UpdateMemoryFromBlock, not in the generation loop.
	// RestatePrompt reinforces the memory block format.
	// See TheoryOfAIComponents.
	if !noMemory {
		var profileText string
		if entry, err := currentMemory(); err == nil && entry != nil {
			profileText = strings.Join(entry.Items, "\n")
		}
		comps = append(comps, components.Component{
			Kind:          "memory",
			PromptSection: memoryBlockSystemPrompt(profileText),
			RestatePrompt: memoryBlockRestatePrompt,
		})
	}

	ret.ComponentSet = comps
	return
}

func memoryBlockSystemPrompt(profileText string) string {
	return `
在每一轮对话中，按以下流程执行：
1. 首先，根据现有的用户画像，生成对用户当前输入的回应。这是首要任务。
2. 在回应之后，仔细分析用户的最新输入，判断其中是否包含任何可以用来补充、修正或深化现有用户画像的新信息。
3. 如果发现了新信息，生成一个记忆更新块（memory block）。不要将记忆更新块的内容混入常规回复中。记忆更新块的格式为：

<<爨齉龘 <memory>
<memory>
  <memory-item>用户画像项1</memory-item>
  <memory-item>用户画像项2</memory-item>
</memory>
爨齉龘

其中 爨齉龘 是一个示例分隔符。每次生成时选择三个新的非常用汉字作为分隔符，并确保分隔符不会与内容冲突。

- 如果没有发现任何新信息，则不要生成此块。
- 在提取和记录信息时，坚持高度确定性的事实原则：仅记录用户在对话中明确表达的事实，严禁记录任何缺乏根据的主观推测、直觉判断或过度推论。
- 特别注意：用户询问某个话题并不代表该话题发生在用户身上。例如，用户询问手术相关信息，仅代表用户关心此话题，不代表用户本人进行了手术。严禁将用户的兴趣或咨询内容错误地记录为用户的个人经历或状态。宁愿保持简洁的画像，也不要加入未经验证的猜测。

用户画像对于理解用户和提供个性化回应至关重要，因此在每一轮对话中都认真执行这个评估过程。

用户画像：
` + profileText
}

const memoryBlockRestatePrompt = `- Memory block: emit <<麐黿龘 <memory>
<memory>
  <memory-item>user profile fact</memory-item>
</memory>
麐黿龘 only when there is new factual information about the user. Do not mix memory content into the regular reply. If no new information, do not emit this block. The example delimiter 麐黿龘 is illustrative: choose your own three uncommon Chinese characters as the delimiter, the SAME delimiter on the closing line.`
