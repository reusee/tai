package main

import (
	"context"
	"strings"

	"github.com/reusee/tai/blocks"
	"github.com/reusee/tai/components"
	"github.com/reusee/tai/flags"
	"github.com/reusee/tai/generators"
	"github.com/reusee/tai/memories"
	"github.com/reusee/tai/pipeline"
)

const TheoryOfAIComponents = `
The ai command uses the Component mechanism for shell, ingest, and memory
blocks. Shell and ingest components are processed in the generation loop,
while memory blocks are processed after the loop by
memories.UpdateMemoryFromBlock. The memory component's prompt includes the
dynamic user profile text, read at Component construction time (provider
resolution) rather than at prompt assembly time. BlockFormatSystemPrompt is a
prompt-only Component that teaches the model the boundary-delimited block
format used by memory blocks.

The memory component carries a Process function that produces no parts and
no state: its only effect is the ComponentOutput the loop records, which
attaches a block-result child to every memory block node in the session
tree, marking the blocks processed. The actual profile update runs in
ai.go's OnAttemptSuccess hook via memories.UpdateMemoryFromBlock, so the
component's Process must stay inert — returning parts would trigger a
generation round and double the memory update. The block node in the
session tree then carries a result child and no longer reads as
unprocessed. See TheoryOfSessionTree.
`

// baseAISystemPrompt is the base AI assistant prompt text, a prompt-only
// Component in AIComponents. See TheoryOfAIComponents.
const baseAISystemPrompt = `提供有用的帮助。
输出易于阅读的文本，避免使用markdown格式，不要加入任何表示格式的符号，避免生成表格。`

// AIComponents is the component set type for the ai command. It embeds
// components.ComponentSet as an anonymous struct field so that dscope can
// resolve it independently from the pipeline module's CodesComponents,
// avoiding a type conflict when both providers are wired into the same
// scope. Method promotion eliminates the need for explicit delegation
// methods. See TheoryOfAIComponents.
type AIComponents struct {
	components.ComponentSet
}

func (Module) AIComponents(
	flagShell flags.Shell,
	currentMemory memories.CurrentMemory,
	extra flags.ExtraSystemPrompt,
	familyExtra flags.FamilyExtraSystemPrompt,
	modelFamily generators.ModelFamily,
	noMemory NoMemory,
	lspHandler blocks.LSPHandler,
) (ret AIComponents) {
	var comps components.ComponentSet

	// Base AI assistant prompt: prompt-only Component for unified prompt
	// assembly. See TheoryOfAIComponents.
	comps = append(comps, components.Component{
		PromptSection: baseAISystemPrompt,
	})

	// BlockFormatSystemPrompt is a prompt-only Component that teaches the
	// model the boundary-delimited block format used by memory blocks.
	// See TheoryOfAIComponents.
	comps = append(comps, components.Component{
		PromptSection: blocks.BlockFormatSystemPrompt,
	})

	// Ingest component: shared with the codes pipeline through
	// pipeline.NewIngestComponent, so the ai session teaches and processes
	// the kind identically — fetched context is appended as user content
	// and triggers the next generation. Placed before the common
	// components so fetched context precedes shell execution in the
	// processing order. The constructor attaches the session's
	// language-server handler and appends its Go-specific lsp tag
	// documentation when one resolves. See TheoryOfAIComponents and
	// blocks.TheoryOfIngestBlocks.
	comps = append(comps, pipeline.NewIngestComponent(lspHandler))

	// Common components: shell (conditional on flagShell) only. The
	// continue component is deliberately filtered out: in the interactive
	// ai chat the user's next input arrives through OnIdle
	// (phases.BuildChatIdle) after the round ends, so a continue block
	// would only feed the model's own body back as user content, allowing
	// meaningless self-prompts such as "Please provide the next task or
	// user input" to bypass the user prompt. Shell output remains useful:
	// it is real feedback that triggers the next round without user
	// input. See TheoryOfAIComponents and TheoryOfAiCommand.
	for _, comp := range components.CommonComponents(bool(flagShell)) {
		if comp.Kind == "continue" {
			continue
		}
		comps = append(comps, comp)
	}

	// Disabled-blocks notice: list the block kinds this session cannot
	// process so the model does not emit them from habit — an unprocessed
	// block is silently ignored while implying an action that never
	// happened. The ai command processes shell, ingest, and memory blocks:
	// the remaining pipeline kinds (change, go-test, go-src) have no
	// processor here, and continue is deliberately excluded because
	// OnIdle is the sole input gateway. Shell is listed when the flag is
	// off, memory when -no-memory is set. The notice is static per
	// configuration and placed before the config-derived extras and the
	// dynamic memory section, keeping the cacheable prefix stable. See
	// components.TheoryOfDisabledBlocks and TheoryOfAIComponents.
	disabledKinds := []string{
		"change", "continue", "go-test", "go-src",
	}
	if !bool(flagShell) {
		disabledKinds = append(disabledKinds, "shell")
	}
	if noMemory {
		disabledKinds = append(disabledKinds, "memory")
	}
	comps = append(comps, components.DisabledBlocksComponent(disabledKinds...))

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

	// Family-specific extra system prompts: top-level prompts keyed by
	// the model family. The family is resolved from the scope via
	// generators.ModelFamily; when the family matches a key, the
	// corresponding prompts are appended after the generic extra prompts
	// as prompt-only components. See pipeline.TheoryOfFamilyExtraSystemPrompt.
	for _, prompt := range familyExtra[string(modelFamily)] {
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
	// shell, and extra prompt sections remain byte-identical and fully
	// cacheable. The Process function is inert on purpose: the actual
	// profile update runs post-loop in ai.go's OnAttemptSuccess hook
	// (memories.UpdateMemoryFromBlock), and the inert Process exists so
	// the loop records a ComponentOutput for memory blocks, which
	// attaches a block-result child to each memory block node in the
	// session tree — without it the block node reads as unprocessed. A
	// nil Process leaves the block childless, so the loop must see a
	// Process, and parts or state must stay empty so no extra generation
	// round is triggered. See TheoryOfAIComponents.
	if !noMemory {
		var profileText string
		if entry, err := currentMemory(); err == nil && entry != nil {
			profileText = strings.Join(entry.Items, "\n")
		}
		comps = append(comps, components.Component{
			Kind:          "memory",
			PromptSection: memoryBlockSystemPrompt(profileText),
			Process: func(ctx context.Context, pctx *components.ProcessContext) components.ProcessResult {
				return components.ProcessResult{}
			},
		})
	}

	ret.ComponentSet = comps
	return
}

func memoryBlockSystemPrompt(profileText string) string {
	return `
在每一轮对话中，按以下流程执行：
1. 首先，根据现有的用户画像，生成对用户当前输入的回应。这是首要任务。
2. 在回应之后，仔细分析用户的最新输入，判断其中是否包含任何可以用来补充、修正或删除现有用户画像条目的信息。
3. 如果发现了此类信息，生成一个记忆更新块（memory block）。不要将记忆更新块的内容混入常规回复中。记忆更新块的正文为 XML 结构，可同时携带新增条目（memory-item）与删除条目（memory-delete）：

<memory>
  <memory-item>新增的用户画像项</memory-item>
  <memory-delete>要删除的画像项，内容必须与现有条目逐字一致</memory-delete>
</memory>

- 新增条目（memory-item）记录新的用户画像事实。
- 删除条目（memory-delete）按逐字一致的原则移除现有画像条目：仅当用户明确更正了某个事实，或明确表示某个条目已过时、不再成立时使用。同一轮中对同一条目既有新增又有删除时，删除生效。
- 如果没有发现任何需要新增或删除的信息，则不要生成此块。
- 在提取和记录信息时，坚持高度确定性的事实原则：仅记录用户在对话中明确表达的事实，严禁记录任何缺乏根据的主观推测、直觉判断或过度推论。删除同样遵循确定性原则：仅凭推测不得删除任何条目。
- 特别注意：用户询问某个话题并不代表该话题发生在用户身上。例如，用户询问手术相关信息，仅代表用户关心此话题，不代表用户本人进行了手术。严禁将用户的兴趣或咨询内容错误地记录为用户的个人经历或状态。宁愿保持简洁的画像，也不要加入未经验证的猜测。

用户画像对于理解用户和提供个性化回应至关重要，因此在每一轮对话中都认真执行这个评估过程。

用户画像：
` + profileText
}
