package pipeline

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/reusee/dscope"
	"github.com/reusee/tai/changes"
	"github.com/reusee/tai/flags"
)

// TestReviewPromptIsCritical checks that the review instruction keeps the
// properties that make the review loop adversarial and useful at the same
// time: it assumes defects exist, checks the change against the original task,
// refuses the diff as the only evidence, refuses prose claims as evidence,
// demands a failure scenario before a change is judged correct, asks why a
// protection was removed, follows every caller of a changed symbol, bounds the
// fix to confirmed defects, holds the fix itself to the same check, requires a
// test that fails when the change is reverted, treats a passing test as
// evidence for the covered paths only, keeps an unverifiable claim out of the
// defect list, excludes style preferences from it, fixes the meaning of each
// severity group, requires the evidence behind every finding, names the shapes
// of an unfinished review, runs the tests as evidence, and rejects an
// unsupported clean verdict. A prompt that loses one of these turns the loop
// into a rubber stamp or into a generator of invented defects. See
// TheoryOfReviewLoop.
func TestReviewPromptIsCritical(t *testing.T) {
	required := []string{
		"假定改动中存在缺陷",
		"是否完成了原始任务",
		"不要只看 diff",
		"待验证的主张",
		"风格偏好不是缺陷",
		"构造让它出错的场景",
		"删掉了测试",
		"其它调用者",
		"只修已确认的缺陷",
		"保持最小改动",
		"撤销修正后",
		"修正本身也是改动",
		"go-test",
		"测试通过只说明",
		"严重度定义",
		"判断依据",
		"未验证的疑点",
		"文件与行号",
		"视为审核未完成",
		"只读了 diff",
		"复述改动内容",
		"看起来没问题",
	}
	for _, want := range required {
		if !strings.Contains(ReviewPrompt, want) {
			t.Errorf("ReviewPrompt must contain %q", want)
		}
	}
	// A clean verdict must be reachable: the instruction tells the reviewer to
	// justify it, not to forbid it. An instruction that always demands a
	// defect produces invented ones.
	if !strings.Contains(ReviewPrompt, "才可给出无缺陷结论") {
		t.Error("ReviewPrompt must allow a justified clean verdict")
	}
}

// TestBuildReviewPromptCarriesInstructionTaskAndDiffs checks the shape of the
// review user message: the adversarial instruction leads, the original task
// follows, and the unified diff of the session comes last, so the reviewer
// reads the requirement before the change. A message without a task must omit
// the task section instead of emitting an empty heading; the assertion anchors
// on the section's own framing, because the instruction prose itself mentions
// the original task and would make a bare substring check pass vacuously. See
// TheoryOfReviewLoop.
func TestBuildReviewPromptCarriesInstructionTaskAndDiffs(t *testing.T) {
	diffs := []changes.FileDiff{
		{
			Path:           "test.go",
			Original:       []byte("old content\n"),
			OriginalExists: true,
			Current:        []byte("new content\n"),
			CurrentExists:  true,
		},
	}
	formatted := changes.FormatFileDiffs(diffs)
	if formatted == "" {
		t.Fatal("test diff must render as non-empty text")
	}

	const task = "把 foo 的返回值改成 int"
	prompt := buildReviewPrompt(task, diffs)
	if !strings.HasPrefix(prompt, ReviewPrompt) {
		t.Error("the review instruction must lead the review message")
	}
	taskIndex := strings.Index(prompt, task)
	if taskIndex < 0 {
		t.Fatal("the review message must carry the original task")
	}
	diffIndex := strings.Index(prompt, formatted)
	if diffIndex < 0 {
		t.Fatal("the review message must carry the session diff verbatim")
	}
	if taskIndex > diffIndex {
		t.Error("the original task must precede the diff")
	}

	const taskSection = "\n\n原始任务：\n\n" + task
	if !strings.Contains(prompt, taskSection) {
		t.Error("the original task must render in its own section")
	}

	withoutTask := buildReviewPrompt("", diffs)
	if strings.Contains(withoutTask, "\n\n原始任务：\n\n") {
		t.Error("an empty task must not render the task section")
	}
	if !strings.Contains(withoutTask, formatted) {
		t.Error("the task-free review message must still carry the diff")
	}
}

// TestRunReviewCarriesOriginalTask checks that the review session receives the
// original chat input. The review scope replaces Chats with the review prompt,
// so the task must be captured before the fork; otherwise the reviewer judges
// the diff without the requirement it was meant to satisfy. See
// TheoryOfReviewLoop.
func TestRunReviewCarriesOriginalTask(t *testing.T) {
	const task = "把 foo 的返回值改成 int"
	var reviewChat string
	fakeReset := dscope.Reset(func() dscope.Scope {
		return dscope.New(
			// dscope validates provider dependencies at registration time,
			// so the fake scope provides flags.Chats before the provider
			// that depends on it. RunReview forks the review prompt over
			// this placeholder.
			func() flags.Chats { return nil },
			func(chats flags.Chats) GenerateWithResultWithStats {
				return func(ctx context.Context, output io.Writer) (Result, []AttemptStat, error) {
					for _, chat := range chats {
						reviewChat = chat
					}
					return Result{}, nil, nil
				}
			},
		)
	})

	var m Module
	runReview := m.RunReview(
		fakeReset,
		true,
		nil,
		flags.ModelName("test-model"),
		flags.Chats{task},
	)
	if err := runReview(context.Background(), io.Discard, []changes.FileDiff{
		{
			Path:           "test.go",
			Original:       []byte("old content\n"),
			OriginalExists: true,
			Current:        []byte("new content\n"),
			CurrentExists:  true,
		},
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasPrefix(reviewChat, ReviewPrompt) {
		t.Error("the review chat must start with the review instruction")
	}
	if !strings.Contains(reviewChat, task) {
		t.Error("the review chat must carry the original task")
	}
}
