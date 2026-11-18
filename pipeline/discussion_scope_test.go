package pipeline

import (
	"context"
	"io"
	"testing"

	"github.com/reusee/dscope"
	"github.com/reusee/tai/flags"
	"github.com/reusee/tai/generators"
	"github.com/reusee/tai/modes"
	"github.com/reusee/tai/tree"
)

// TestMakeGoalDiscussionForksScope pins the discussion scope's three
// critical forks: DiscussMode selects the discussion system prompt, the
// session tree continuation anchors the conversation under the last
// achieved loop node — the context the discussion continues from — and
// the seed chat gives the session its first message. The forked
// BuildChatIdle provider receives the inner DiscussMode, continuation,
// and chats, so the test observes the scope the discussion builds
// instead of trusting the source. See TheoryOfGoalDiscussion.
func TestMakeGoalDiscussionForksScope(t *testing.T) {
	var (
		discussMode  bool
		continuation SessionTreeContinuation
		chats        flags.Chats
		called       bool
	)
	scope := dscope.New(modes.ForTest(t), new(Module)).Fork(
		func() GenerateWithResultWithStats {
			return func(ctx context.Context, output io.Writer) (Result, []AttemptStat, error) {
				called = true
				return Result{}, nil, nil
			}
		},
		func(
			mode DiscussMode,
			cont SessionTreeContinuation,
			c flags.Chats,
		) BuildChatIdle {
			discussMode = bool(mode)
			continuation = cont
			chats = c
			return func(generators.Generator, *generators.GenerateOptions) IdleHandler {
				return nil
			}
		},
	)
	reset := scope.Get[dscope.Reset]()
	tr := tree.New()
	tr, err := tr.Write("root", "loop-1", tree.TypeLoop, tree.AuthorProgram, "goal loop 1")
	if err != nil {
		t.Fatal(err)
	}
	discussion := makeGoalDiscussion(reset, TaskDecision{})
	if _, err := discussion(context.Background(), tr, "loop-1"); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("the discussion must run one generation session")
	}
	if !discussMode {
		t.Fatal("the discussion must fork DiscussMode so the command selects the discussion prompt")
	}
	if continuation.Tree != tr {
		t.Fatal("the discussion must continue the run's session tree")
	}
	if continuation.Parent != "loop-1" {
		t.Fatalf("the discussion must hang under the last loop node, got parent %q", continuation.Parent)
	}
	if len(chats) != 1 || chats[0] != discussSeedChat {
		t.Fatalf("the discussion must start from the seed chat, got %v", chats)
	}
}

// TestMakeGoalDiscussionEnsuresAnchor pins the anchor repair: a tree that
// does not carry the loop node — a loop whose tree write failed, or a
// tree assembled outside the loop continuation — still carries the
// discussion's nodes, because the runner writes the anchor node before
// the session opens. See TheoryOfGoalDiscussion.
func TestMakeGoalDiscussionEnsuresAnchor(t *testing.T) {
	scope := dscope.New(modes.ForTest(t), new(Module)).Fork(
		func() GenerateWithResultWithStats {
			return func(ctx context.Context, output io.Writer) (Result, []AttemptStat, error) {
				return Result{}, nil, nil
			}
		},
		func(DiscussMode, SessionTreeContinuation, flags.Chats) BuildChatIdle {
			return func(generators.Generator, *generators.GenerateOptions) IdleHandler {
				return nil
			}
		},
	)
	reset := scope.Get[dscope.Reset]()
	discussion := makeGoalDiscussion(reset, TaskDecision{})
	out, err := discussion(context.Background(), tree.New(), "loop-1")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := out.Node("loop-1"); !ok {
		t.Fatal("the discussion must write the missing loop node before the session opens")
	}
}
