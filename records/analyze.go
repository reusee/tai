package records

import (
	"context"
	"fmt"
	"io"

	"github.com/reusee/tai/generators"
	"github.com/reusee/tai/phases"
)

const analysisSystemPrompt = `你是一个AI工具交互分析器。tai是一个AI辅助编码工具，其工作方式为：模型输出带有边界分隔符的结构化块——change块用于修改代码文件，shell块用于执行shell命令，go-test块用于运行Go测试，continue块用于触发下一轮生成，summary块用于标记当前轮次正常结束，request-context块用于请求更多上下文。一次生成过程按"轮次"组织：每轮模型输出内容，系统解析其中的块并处理（应用代码修改、执行命令、运行测试），随后根据continue块和组件结果决定是否进入下一轮。整个过程可能发生重试（输出截断、解析错误、应用失败）。

下面是一次完整的交互记录，包含会话元信息、每轮的时间戳、用户输入、模型输出、思考过程、生成的块、错误与重试。

请分析这次交互，输出一份改进报告，包含以下部分：

1. 交互概要：本次交互的目标、经历的主要阶段（轮次）、最终结果。
2. 做得好的地方：指出模型或用户的有效行为，以及工具机制中运转良好的部分。
3. 问题清单：列出所有问题——错误、重试、格式错误的块、未被应用的修改、浪费的轮次、低效的交互。
4. 根因分析：对每个问题，分析其最可能的根因：系统提示词不清晰、工具行为缺陷、模型策略失误、还是用户指令问题。
5. 改进建议：给出具体、可操作的改进建议。每一项必须说明：改进什么、应该改在哪里（哪个系统提示词、哪个工具机制、哪种使用方式）、预期效果。

要求：
- 具体：引用记录中的实际事件（轮次、块类型、错误信息），不要泛泛而谈。
- 优先分析反复出现或阻碍进展的问题。
- 使用简体中文，输出易读的纯文本，不要使用markdown格式符号，不要生成表格。`

// RunAnalysis renders the selected session as a transcript and sends it to
// the model with the analysis system prompt. A session id of 0 selects the
// most recent session. The generation runs as a single round using the
// provided generator and phase builder; the analysis is written to output.
// See TheoryOfInteractionRecording.
func RunAnalysis(
	ctx context.Context,
	generator generators.Generator,
	buildGenerate phases.BuildGenerate,
	recorder *Recorder,
	sessionID int64,
	output io.Writer,
) error {
	if sessionID == 0 {
		id, err := latestSessionID(recorder)
		if err != nil {
			return err
		}
		if id == 0 {
			return fmt.Errorf("no recorded sessions; run with -record to record interactions")
		}
		sessionID = id
	}
	transcript, err := Transcript(recorder, sessionID)
	if err != nil {
		return err
	}

	var state generators.State
	state = generators.NewPrompts(
		analysisSystemPrompt,
		[]*generators.Content{
			{
				Role: generators.RoleUser,
				Parts: []generators.Part{
					generators.Text(fmt.Sprintf("以下是第 %d 次交互的完整记录：\n\n%s", sessionID, transcript)),
				},
			},
		},
	)
	state = generators.NewOutput(state, output, true)

	phase := buildGenerate(generator, nil)(nil)
	for phase != nil {
		var err error
		phase, state, err = phase(ctx, state)
		if err != nil {
			return err
		}
	}
	return nil
}
