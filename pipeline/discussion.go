package pipeline

import (
	"context"
	"iter"
	"os"

	"github.com/reusee/dscope"
	"github.com/reusee/prompts"
	"github.com/reusee/tai/flags"
	"github.com/reusee/tai/tree"
)

const TheoryOfGoalDiscussion = `
Post-goal discussion theory:
- A goal run that ended achieved may continue as a conversation. The
  discussion is not another goal: no goal loop runs, no done block ends
  anything, and no verification loop follows. It is one interactive
  generation session about the finished work, so the user can question
  the result, ask for an explanation, or request one further change.
- The discussion session hangs under the last achieved loop node: the
  session tree continuation names that node as its parent, so every
  node of the conversation joins the loop whose work it discusses and
  each round's tree outline renders that loop's subtree. The last loop
  is therefore the whole cross-loop context of the discussion; the run
  tree carries the other loops, but no round sees them.
- The session opens a fresh scope through the command's reset — the
  latest filesystem state, exactly as every goal loop does — and forks
  DiscussMode, so the command selects the discussion system prompt
  instead of the goal system prompt. It also refreshes the run's task
  decision, so the read-only judgment and the base-prompt introduction
  stay stable across the run and no second judgment runs.
- The interactive idle handler belongs to the discussion alone: the
  codes pipeline never reads interactive input otherwise. The chat idle
  handler is installed by a run decorator appended to the decorators
  the command already carries, so a display front-end keeps observing
  the session, and the decorator turns the plan-driven rounds off: the
  user drives a discussion, while the plan tree drives goal loops.
- One user line opens one more round of the same session. The session
  ends when the user quits, which releases the blocked chat input with
  io.EOF and ends the generation loop normally.
- RunGoal starts the discussion only after the review pass and only
  when the run is achieved: a discussion about an unfinished goal would
  present incomplete work as final. A run that ends unachieved — the
  iteration budget exhausted, a repeated error, or a terminal no-task
  stop — starts no discussion.
- In TUI mode the discussion is the reason a session that did not
  declare itself interactive still shows the chat input bar: the first
  ChatInput call raises the bar (see cmd/tai.TheoryOfTUIChatInput).
`

// DiscussMode marks a post-goal discussion session. It is false for
// every other session; the discussion forks true, so the command
// selects the discussion system prompt and the discussion's run
// decorator installs the interactive idle handler. See
// TheoryOfGoalDiscussion.
type DiscussMode bool

// DiscussMode provider: the default is false — a goal run or any other
// codes session. Only the discussion session forks true. See
// TheoryOfGoalDiscussion.
func (Module) DiscussMode() DiscussMode { return false }

// GoalDiscussion runs the post-goal discussion session of one achieved
// goal run: one interactive generation session on the context of the
// last achieved loop node. tr is the run's session tree and parent the
// loop node the session's nodes hang under; the evolved tree is
// returned. See TheoryOfGoalDiscussion.
type GoalDiscussion func(ctx context.Context, tr *tree.Tree, parent string) (*tree.Tree, error)

// discussSeedChat opens the discussion session. The codes pipeline
// generates nothing without a non-empty chat, so the conversation
// starts with one short request that asks the model to report the
// run's result; the report is the discussion's first message and the
// session then waits for the user's own lines. See
// TheoryOfGoalDiscussion.
const discussSeedChat = "本次目标已经完成。请简要报告最终的运行结果，然后等待我的进一步问题或要求。"

// DiscussSystemPrompt teaches the discussion session. It replaces the
// goal system prompt, so the model reads no rule about goal loops,
// verification loops, or the done block as a completion signal — the
// goal is already achieved and the user ends the session. Further
// changes are allowed when the user asks for them, the turn always
// returns to the user — a continue block would speak over the user — and
// every response still closes with the summary block. See
// TheoryOfGoalDiscussion.
const DiscussSystemPrompt = `
**Post-Goal Discussion:**

The goal run is complete: its last loop emitted a done block while applying no change blocks, so no goal loop runs any more. This session is a conversation about the finished work. The user asks questions, requests explanations, or asks for one further change, and you answer directly.

**Rules:**
- One user message opens one round. Answer that message. Do not resume the goal protocol, do not re-run the goal loops, and do not treat this session as another goal.
- The filesystem is the ground truth. Read the current files with ingest and go-src blocks before you explain or judge code, and state what you actually checked.
- The session tree outline that closes each round's feedback is the last goal loop's subtree: it shows what that loop read, generated, and changed, and it is the context this discussion continues from.
- Further changes are allowed when the user asks for them: use change blocks as usual, verify the result with go-test blocks, and keep the change as small as the request. Work the user did not ask for does not belong in a discussion.
- Never emit a done block. The goal is already achieved, and no block ends this session but the user's quit.
- Never emit a continue block. Every response hands the turn back to the user: the session waits for the user's next message, and an automatic round would speak over the user.
- The plan tree does not drive these rounds: the user does. Do not emit plan-op blocks unless the user asks for a plan.
- Keep every answer short and concrete. Name the files and the checks your answer rests on rather than summarizing at a distance.
- End every response with the summary block. The session stays open after the response and waits for the user's next message.
`

// DiscussSystemPromptText assembles the discussion system prompt: the
// codes base prompt — introduced only when the task decision judges the
// session's task a programming task, matching the goal prompt — the
// discussion prompt, and the component sections. It replaces the goal
// system prompt in a discussion session, so no goal rule reaches the
// conversation. See TheoryOfGoalDiscussion.
func DiscussSystemPromptText(
	comps CodesComponents,
	programmingTask ProgrammingTask,
) SystemPrompt {
	var prompt string
	if bool(programmingTask) {
		prompt = prompts.Codes + "\n"
	}
	prompt += DiscussSystemPrompt + "\n" +
		comps.PromptSections()
	return SystemPrompt(prompt)
}

// discussRunDecorator installs the discussion session's behavior on one
// loop: the interactive idle handler that reads the user's lines
// through the injected ChatInput, and the plan-driven rounds off,
// because a discussion follows the user while the plan tree drives goal
// loops. The decorator is appended after the command's own decorators,
// so a display front-end still observes the session. See
// TheoryOfGoalDiscussion.
func discussRunDecorator(buildChatIdle BuildChatIdle) RunDecorator {
	return func(run Run) Run {
		return func(ctx context.Context, opts RunOptions, result *Result) iter.Seq2[*tree.Tree, error] {
			opts.PlanMode = false
			if opts.OnIdle == nil && opts.Generator != nil {
				opts.OnIdle = buildChatIdle(opts.Generator, nil)
			}
			return run(ctx, opts, result)
		}
	}
}

// makeGoalDiscussion builds the discussion runner of one goal run. The
// session opens a fresh scope through reset, forks the run's task
// decision, DiscussMode, the session tree continuation under the last
// achieved loop node, and the seed chat, and appends the discussion's
// run decorator to the decorators the command already carries. The
// evolved session tree is returned, so the run keeps one tree. The
// discussion's anchor node is ensured before the session opens: a
// generator may hand back a tree that does not carry the loop node —
// a failed loop's tree, or a tree assembled outside the loop
// continuation — and the discussion must never lose the branch it
// hangs under. See TheoryOfGoalDiscussion.
func makeGoalDiscussion(reset dscope.Reset, decision TaskDecision) GoalDiscussion {
	return func(ctx context.Context, tr *tree.Tree, parent string) (*tree.Tree, error) {
		if tr == nil {
			tr = tree.New()
			parent = "root"
		}
		if parent != "root" {
			if _, ok := tr.Node(parent); !ok {
				if next, werr := tr.Write("root", parent, tree.TypeLoop, tree.AuthorProgram, "goal loop"); werr == nil {
					tr = next
				}
			}
		}
		scope := reset()
		// The run's judgment is reused: the task does not change between
		// the loops and the discussion, and re-judging would run the
		// decision model again mid-run. See TheoryOfTaskDecision.
		scope = scope.Fork(func() TaskDecision { return decision })
		scope = scope.Fork(func() DiscussMode { return true })
		scope = scope.Fork(func() SessionTreeContinuation {
			return SessionTreeContinuation{Tree: tr, Parent: parent}
		})
		scope = scope.Fork(func() flags.Chats {
			return flags.Chats{discussSeedChat}
		})
		buildChatIdle := scope.Get[BuildChatIdle]()
		existing := scope.Get[RunDecorators]()
		decorators := make(RunDecorators, 0, len(existing)+1)
		decorators = append(decorators, existing...)
		decorators = append(decorators, discussRunDecorator(buildChatIdle))
		scope = scope.Fork(func() RunDecorators { return decorators })
		result, _, err := scope.Get[GenerateWithResultWithStats]()(ctx, os.Stdout)
		if result.SessionTree != nil {
			tr = result.SessionTree
		}
		return tr, err
	}
}
