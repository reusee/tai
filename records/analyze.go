package records

import (
	"context"
	"fmt"
	"io"

	"github.com/reusee/tai/generators"
)

const analysisSystemPrompt = `你是一个AI工具交互分析器。tai是一个AI辅助编码工具，其工作方式为：模型输出带有边界分隔符的结构化块——change块用于修改代码文件，shell块用于执行shell命令，go-test块用于运行Go测试，continue块用于触发下一轮生成，summary块用于标记当前尝试正常结束，ingest块用于请求更多上下文。一次生成过程由若干次尝试（attempt）组成：每次尝试是模型的一次完整生成请求；系统在节点产生时立即记录（会话树操作流），并解析该次尝试输出中的块进行处理（应用代码修改、执行命令、运行测试），随后根据continue块和组件结果决定是否开始新的生成。同一生成内的重试表现为新的attempt节点；输出截断、解析错误、应用失败都可能触发重试。一次目标运行（goal run）由若干次循环（loop）组成，每次循环是独立的一次生成会话。

下面是一次完整的交互记录，其形式为会话树的重建：树的每个节点有一行「名称 [类型/作者]」，其下缩进的行是该节点的完整内容；缩进更深的部分是该节点的子节点。直属关系即归属：attempt节点下属它消耗的输入、它产生的输出、它生成的块及其处理结果、以及过程事件节点；loop节点下属该轮的会话节点。

请分析这次交互，输出一份改进报告，包含以下部分：

1. 交互概要：本次交互的目标、经历的主要阶段（循环与尝试）、最终结果。
2. 做得好的地方：指出模型或用户的有效行为，以及工具机制中运转良好的部分。
3. 问题清单：列出所有问题——错误、重试、接口调用错误、格式错误的块、未被应用的修改、浪费的尝试、低效的交互。
4. 根因分析：对每个问题，分析其最可能的根因：系统提示词不清晰、工具行为缺陷、模型策略失误、还是用户指令问题。
5. 改进建议：给出具体、可操作的改进建议。每一项必须说明：改进什么、应该改在哪里（哪个系统提示词、哪个工具机制、哪种使用方式）、预期效果。

要求：
- 具体：引用记录中的实际节点（节点名称、类型、内容、错误信息），不要泛泛而谈。
- 优先分析反复出现或阻碍进展的问题。
- 使用简体中文，输出易读的纯文本，不要使用markdown格式符号，不要生成表格。`

// runAnalysis renders the selected session as a transcript and sends it to
// the model with the analysis system prompt. A session id of 0 selects the
// most recent session. The generation runs as a single round using the
// provided generator and phase builder; the analysis is written to output.
// See TheoryOfInteractionRecording.
func runAnalysis(
	ctx context.Context,
	generator generators.Generator,
	buildGenerate generators.BuildGenerate,
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

// RunAnalysis analyzes a recorded session with the model to seek
// improvements. The generator, phase builder, and recorder are bound from
// the dscope scope, so callers pass only the runtime values (context, the
// session id, and the output writer). See TheoryOfInteractionRecording.
type RunAnalysis func(ctx context.Context, sessionID int64, output io.Writer) error

func (Module) RunAnalysis(
	recorder *Recorder,
	getDefaultGenerator generators.GetDefaultGenerator,
	buildGenerate generators.BuildGenerate,
) RunAnalysis {
	return func(ctx context.Context, sessionID int64, output io.Writer) error {
		generator, err := getDefaultGenerator()
		if err != nil {
			return err
		}
		return runAnalysis(ctx, generator, buildGenerate, recorder, sessionID, output)
	}
}
