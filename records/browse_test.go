package records

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/reusee/dscope"
	"github.com/reusee/tai/modes"
	"github.com/reusee/tai/tree"
)

// TestSessionBrowsing verifies the browsing data API: the session list
// carries every session, most recent first, with its operation count and
// its chat preview, and the tree load reconstructs the recorded operation
// stream. A session that recorded no operations has no tree and reports an
// error instead. See TheoryOfSessionBrowsing.
func TestSessionBrowsing(t *testing.T) {
	scope := dscope.New(
		modes.ForTest(t),
		new(Module),
		stubGetDefaultGenerator,
		stubBuildGenerate,
	).Fork(
		func() DBPath {
			return DBPath(filepath.Join(t.TempDir(), "test.db"))
		},
		func() Enabled {
			return Enabled(true)
		},
	)
	recorder := scope.Get[*Recorder]()
	list := scope.Get[ListSessionInfos]()
	load := scope.Get[LoadSessionTree]()
	recorder.StartSession("first")
	tr := tree.New().WithOpSink(recorder.Sink())
	var err error
	tr, err = tr.Write("root", "user-1", tree.TypeUser, tree.AuthorUser, "hello")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tr.Write("root", "model-1", tree.TypeModel, tree.AuthorModel, "world"); err != nil {
		t.Fatal(err)
	}
	recorder.EndSession(nil)
	recorder.StartSession("second")
	recorder.EndSession(nil)

	infos, err := list()
	if err != nil {
		t.Fatal(err)
	}
	if len(infos) != 2 {
		t.Fatalf("expected 2 sessions, got %d", len(infos))
	}
	if infos[0].Command != "second" || infos[1].Command != "first" {
		t.Fatalf("expected the most recent session first, got %+v", infos)
	}
	if infos[1].OpCount != 2 {
		t.Fatalf("expected 2 operations recorded, got %d", infos[1].OpCount)
	}
	if infos[1].Chat != "hello" {
		t.Fatalf("expected the chat preview to carry the user input alone, got %q", infos[1].Chat)
	}
	if infos[0].Chat != "" {
		t.Fatalf("expected no chat preview for a session without user input, got %q", infos[0].Chat)
	}
	if infos[1].StartTime == "" || infos[1].Status != "success" {
		t.Fatalf("expected the session's metadata, got %+v", infos[1])
	}

	replayed, err := load(infos[1].ID)
	if err != nil {
		t.Fatal(err)
	}
	user, ok := replayed.Node("user-1")
	if !ok || user.Content != "hello" || user.Type != tree.TypeUser {
		t.Fatalf("expected the recorded user node, got %+v", user)
	}
	model, ok := replayed.Node("model-1")
	if !ok || model.Content != "world" {
		t.Fatalf("expected the recorded model node, got %+v", model)
	}

	if _, err := load(infos[0].ID); err == nil {
		t.Fatal("expected an error for a session without recorded operations")
	}
}

// TestSessionChatPreview verifies the record list's chat preview: a
// session's user inputs reduce to one line each, the fenced chat input the
// ai command wraps takes precedence over the node's first line, the
// program's feedback and the model's output never enter the preview, and
// both the input count and each input's length are bounded, with the
// inputs left out marked. See TheoryOfSessionBrowsing.
func TestSessionChatPreview(t *testing.T) {
	user := func(name, content string) tree.WriteOp {
		return tree.WriteOp{
			Parent: "root", Name: name, Type: tree.TypeUser,
			Author: tree.AuthorUser, Content: content,
		}
	}
	preview := func(t *testing.T, ops ...tree.WriteOp) string {
		t.Helper()
		var chat string
		withRecorder(t, true, func(recorder *Recorder) {
			recorder.StartSession("test")
			tr := tree.New().WithOpSink(recorder.Sink())
			if _, err := tr.WriteAll(ops...); err != nil {
				t.Fatal(err)
			}
			recorder.EndSession(nil)
			infos, err := sessionInfos(recorder, 0)
			if err != nil {
				t.Fatal(err)
			}
			if len(infos) != 1 {
				t.Fatalf("expected one session, got %d", len(infos))
			}
			chat = infos[0].Chat
		})
		return chat
	}
	long := strings.Repeat("x", sessionChatPreviewRunes+10)
	for _, c := range []struct {
		name string
		ops  []tree.WriteOp
		want string
	}{
		{
			name: "first non-blank line",
			ops:  []tree.WriteOp{user("u1", "\n\n  first task\nsecond line")},
			want: "first task",
		},
		{
			name: "fenced chat input takes precedence",
			ops: []tree.WriteOp{user("u1",
				"[System note: the rules are restated]\n\n``` begin of user input\n你好，帮我看看\n``` end of user input\n")},
			want: "你好，帮我看看",
		},
		{
			name: "program feedback and model output excluded",
			ops: []tree.WriteOp{
				user("u1", "the task"),
				{Parent: "root", Name: "feedback-1", Type: tree.TypeUser, Author: tree.AuthorProgram, Content: "[System note: retry]"},
				{Parent: "root", Name: "model-1", Type: tree.TypeModel, Author: tree.AuthorModel, Content: "the answer"},
			},
			want: "the task",
		},
		{
			name: "inputs and lengths bounded",
			ops: []tree.WriteOp{
				user("u1", "one"),
				user("u2", long),
				user("u3", "three"),
				user("u4", "four"),
				user("u5", "five"),
			},
			want: "one | " + strings.Repeat("x", sessionChatPreviewRunes) + "… | three | four | …",
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := preview(t, c.ops...); got != c.want {
				t.Fatalf("expected %q, got %q", c.want, got)
			}
		})
	}
}
