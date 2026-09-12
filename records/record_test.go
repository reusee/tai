package records

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode"

	"github.com/reusee/dscope"
	"github.com/reusee/tai/blocks"
	"github.com/reusee/tai/generators"
	"github.com/reusee/tai/modes"
	"github.com/reusee/tai/tree"
)

// stubGetDefaultGenerator satisfies dscope.New validation for
// records.Module's RunAnalysis provider, which depends on
// generators.GetDefaultGenerator. The stub returns a nil generator; it is
// never invoked by non-analysis tests, which only need the dependency to
// resolve during scope construction. See TheoryOfInteractionRecording.
func stubGetDefaultGenerator() generators.GetDefaultGenerator {
	return func() (generators.Generator, error) {
		return nil, nil
	}
}

// stubBuildGenerate satisfies dscope.New validation for records.Module's
// RunAnalysis provider, which depends on generators.BuildGenerate. The stub
// returns a pass-through phase; it is never invoked by non-analysis
// tests. See TheoryOfInteractionRecording.
func stubBuildGenerate() generators.BuildGenerate {
	return func(generator generators.Generator, options *generators.GenerateOptions) generators.PhaseBuilder {
		return func(cont generators.Phase) generators.Phase {
			return cont
		}
	}
}

func withRecorder(t *testing.T, enabled bool, fn func(*Recorder)) {
	t.Helper()
	dscope.New(
		modes.ForTest(t),
		new(Module),
		stubGetDefaultGenerator,
		stubBuildGenerate,
	).Fork(
		func() DBPath {
			return DBPath(filepath.Join(t.TempDir(), "test.db"))
		},
		func() Enabled {
			return Enabled(enabled)
		},
	).Call(func(recorder *Recorder) {
		if recorder == nil {
			t.Fatal("recorder is nil")
		}
		fn(recorder)
	})
}

func TestRecorderWritesTreeOperations(t *testing.T) {
	withRecorder(t, true, func(recorder *Recorder) {
		recorder.StartSession("test-command")
		tr := tree.New().WithOpSink(recorder.Sink())
		var err error
		tr, err = tr.Write("root", "user-1", tree.TypeUser, tree.AuthorUser, "hello\nworld")
		if err != nil {
			t.Fatal(err)
		}
		tr, _, err = tr.WriteAuto("user-1", "model", tree.TypeModel, tree.AuthorModel, "the answer")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := tr.Modify("user-1", "hello again"); err != nil {
			t.Fatal(err)
		}
		recorder.EndSession(nil)

		var id int64
		if err := recorder.db.QueryRow(`SELECT id FROM sessions LIMIT 1`).Scan(&id); err != nil {
			t.Fatal(err)
		}

		text, err := Transcript(recorder, id)
		if err != nil {
			t.Fatal(err)
		}
		// Each event renders as a boundary-delimited block of kind
		// "event": the operation's metadata is the URI query of the
		// opening header, the content the operation wrote is the block
		// body. The node types are the structured "prefix::name" form,
		// whose ':' separators are percent-encoded (%3A) by the query
		// encoder. The delimiter is drawn at random per event, so the
		// assertions match the header from its kind onward. See
		// tree.TheoryOfTree and TheoryOfInteractionRecording.
		for _, want := range []string{
			"=== Session", "command=test-command", "status=success",
			"operations=3",
			"event:?name=user-1&kind=write&type=message%3A%3Auser&author=user&parent=root&time=",
			"\nhello\nworld\n",
			"name=user-1&kind=modify",
			"\nhello again\n",
			"name=model-1&kind=write&type=message%3A%3Amodel&author=model&parent=user-1&time=",
			"\nthe answer\n",
		} {
			if !strings.Contains(text, want) {
				t.Fatalf("transcript missing %q:\n%s", want, text)
			}
		}
	})
}

func TestRecorderWritesCommandLine(t *testing.T) {
	withRecorder(t, true, func(recorder *Recorder) {
		recorder.StartSession("test")
		recorder.EndSession(nil)
		var commandLine string
		if err := recorder.db.QueryRow(`SELECT command_line FROM sessions LIMIT 1`).Scan(&commandLine); err != nil {
			t.Fatal(err)
		}
		if commandLine == "" {
			t.Fatal("the process command line must be recorded with the session")
		}
	})
}

func TestRecorderDisabledWritesNothing(t *testing.T) {
	withRecorder(t, false, func(recorder *Recorder) {
		if recorder.Enabled() {
			t.Fatal("recorder should be disabled")
		}
		recorder.StartSession("test")
		tr := tree.New().WithOpSink(recorder.Sink())
		if _, err := tr.Write("root", "user-1", tree.TypeUser, tree.AuthorUser, "hello"); err != nil {
			t.Fatal(err)
		}
		recorder.EndSession(nil)

		var count int
		if err := recorder.db.QueryRow(`SELECT COUNT(*) FROM sessions`).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("disabled recorder must not create sessions, got %d", count)
		}
		if err := recorder.db.QueryRow(`SELECT COUNT(*) FROM tree_ops`).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("disabled recorder must not write tree operations, got %d", count)
		}
	})
}

func TestRecorderNilWhenDBUnavailable(t *testing.T) {
	dscope.New(
		modes.ForTest(t),
		new(Module),
		stubGetDefaultGenerator,
		stubBuildGenerate,
	).Fork(
		func() DBPath { return "" },
		func() Enabled { return Enabled(true) },
	).Call(func(recorder *Recorder) {
		if recorder != nil {
			t.Fatal("expected nil recorder when db path is empty")
		}
		if recorder.Sink() != nil {
			t.Fatal("a nil recorder must yield a nil sink")
		}
	})
}

func TestRecorderRecordsFullContentWithoutTruncation(t *testing.T) {
	withRecorder(t, true, func(recorder *Recorder) {
		recorder.StartSession("test")
		tr := tree.New().WithOpSink(recorder.Sink())
		largeBody := strings.Repeat("x", 200*1024)
		if _, err := tr.Write("root", "model-1", tree.TypeModel, tree.AuthorModel, largeBody); err != nil {
			t.Fatal(err)
		}
		recorder.EndSession(nil)

		var id int64
		if err := recorder.db.QueryRow(`SELECT id FROM sessions LIMIT 1`).Scan(&id); err != nil {
			t.Fatal(err)
		}
		text, err := Transcript(recorder, id)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(text, largeBody) {
			t.Fatal("content must be recorded in full without truncation")
		}
		if strings.Contains(text, "truncated") {
			t.Fatal("recorded detail must not contain a truncation marker")
		}
	})
}

func TestListSessions(t *testing.T) {
	withRecorder(t, true, func(recorder *Recorder) {
		recorder.StartSession("first")
		tr := tree.New().WithOpSink(recorder.Sink())
		if _, err := tr.Write("root", "user-1", tree.TypeUser, tree.AuthorUser, "hello"); err != nil {
			t.Fatal(err)
		}
		recorder.EndSession(nil)
		recorder.StartSession("second")
		recorder.EndSession(nil)

		var buf bytes.Buffer
		if err := listSessions(recorder, 10, &buf); err != nil {
			t.Fatal(err)
		}
		out := buf.String()
		for _, want := range []string{
			"id=1", "command=first", "status=success", "operations=1",
			"id=2", "command=second", "operations=0",
		} {
			if !strings.Contains(out, want) {
				t.Fatalf("listing missing %q:\n%s", want, out)
			}
		}
	})
}

func TestLatestSessionID(t *testing.T) {
	withRecorder(t, true, func(recorder *Recorder) {
		id, err := latestSessionID(recorder)
		if err != nil {
			t.Fatal(err)
		}
		if id != 0 {
			t.Fatalf("expected 0 for empty database, got %d", id)
		}
		recorder.StartSession("test")
		recorder.EndSession(nil)
		id, err = latestSessionID(recorder)
		if err != nil {
			t.Fatal(err)
		}
		if id != 1 {
			t.Fatalf("expected 1, got %d", id)
		}
	})
}

func TestSessionNotFound(t *testing.T) {
	withRecorder(t, true, func(recorder *Recorder) {
		err := showSession(recorder, 999, &bytes.Buffer{})
		if err == nil {
			t.Fatal("expected error for nonexistent session")
		}
		if !strings.Contains(err.Error(), "not found") {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

// TestTranscriptCarriesNodeMetadata verifies that the transcript renders
// each write event's complete metadata: the write's insert time survives
// the record-render round trip and appears in the event block's URI
// query alongside the parent, type, and author. The node type carries
// the structured "prefix::name" form, whose ':' separators are
// percent-encoded by the query encoder. See tree.TheoryOfTree.
func TestTranscriptCarriesNodeMetadata(t *testing.T) {
	withRecorder(t, true, func(recorder *Recorder) {
		recorder.StartSession("test")
		tr := tree.New().WithOpSink(recorder.Sink())
		insertTime := time.Date(2024, 5, 1, 12, 30, 0, 0, time.UTC)
		tr, err := tr.WriteAll(tree.WriteOp{
			Parent:     "root",
			Name:       "user-1",
			Type:       tree.TypeUser,
			Author:     tree.AuthorUser,
			Content:    "hello",
			InsertTime: insertTime,
		})
		if err != nil {
			t.Fatal(err)
		}
		recorder.EndSession(nil)

		var id int64
		if err := recorder.db.QueryRow(`SELECT id FROM sessions LIMIT 1`).Scan(&id); err != nil {
			t.Fatal(err)
		}
		text, err := Transcript(recorder, id)
		if err != nil {
			t.Fatal(err)
		}
		// The delimiter is drawn at random per event, so the assertion
		// matches the header from its kind onward.
		want := "event:?name=user-1&kind=write&type=message%3A%3Auser&author=user&parent=root&time=" +
			percentEncodeEventValue(insertTime.Format(time.RFC3339Nano))
		if !strings.Contains(text, want) {
			t.Fatalf("transcript must carry the event's complete metadata, got:\n%s", text)
		}
	})
}

// TestTranscriptRendersDeleteEvents verifies that a delete operation
// appears as its own event in the transcript: the surviving node's
// write event carries its content, the deleted node's write event
// carries the content it had when written, and the delete itself
// renders as a delete event, so the transcript shows the applied
// history rather than the tree's final state. The node type carries
// the structured form, percent-encoded in the query. See
// tree.TheoryOfTree.
func TestTranscriptRendersDeleteEvents(t *testing.T) {
	withRecorder(t, true, func(recorder *Recorder) {
		recorder.StartSession("test")
		tr := tree.New().WithOpSink(recorder.Sink())
		var err error
		tr, err = tr.Write("root", "user-1", tree.TypeUser, tree.AuthorUser, "kept")
		if err != nil {
			t.Fatal(err)
		}
		tr, err = tr.Write("root", "user-2", tree.TypeUser, tree.AuthorUser, "removed")
		if err != nil {
			t.Fatal(err)
		}
		tr, err = tr.Delete("user-2")
		if err != nil {
			t.Fatal(err)
		}
		recorder.EndSession(nil)

		var id int64
		if err := recorder.db.QueryRow(`SELECT id FROM sessions LIMIT 1`).Scan(&id); err != nil {
			t.Fatal(err)
		}
		text, err := Transcript(recorder, id)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(text, "event:?name=user-1&kind=write&type=message%3A%3Auser&author=user&parent=root") || !strings.Contains(text, "\nkept\n") {
			t.Fatalf("the surviving node's write event must carry its content, got:\n%s", text)
		}
		if !strings.Contains(text, "name=user-2&kind=write") || !strings.Contains(text, "\nremoved\n") {
			t.Fatalf("the deleted node's write event must carry the content it had when written, got:\n%s", text)
		}
		if !strings.Contains(text, "name=user-2&kind=delete") {
			t.Fatalf("the delete must appear as its own event, got:\n%s", text)
		}
	})
}

// TestTranscriptEventStreamOrder verifies that transcript events appear
// in application order: one write event per node, each followed by the
// content it wrote, ordered as the operations were recorded.
func TestTranscriptEventStreamOrder(t *testing.T) {
	withRecorder(t, true, func(recorder *Recorder) {
		recorder.StartSession("test")
		tr := tree.New().WithOpSink(recorder.Sink())
		_, err := tr.WriteAll(
			tree.WriteOp{Parent: "root", Name: "user-1", Type: tree.TypeUser, Author: tree.AuthorUser, Content: "first"},
			tree.WriteOp{Parent: "root", Name: "attempt-1", Type: tree.TypeAttempt, Author: tree.AuthorProgram, Content: "attempt 1"},
			tree.WriteOp{Parent: "root", Name: "model-1", Type: tree.TypeModel, Author: tree.AuthorModel, Content: "second"},
		)
		if err != nil {
			t.Fatal(err)
		}
		recorder.EndSession(nil)

		var id int64
		if err := recorder.db.QueryRow(`SELECT id FROM sessions LIMIT 1`).Scan(&id); err != nil {
			t.Fatal(err)
		}
		text, err := Transcript(recorder, id)
		if err != nil {
			t.Fatal(err)
		}
		wantOrder := []string{"name=user-1&kind=write", "name=attempt-1&kind=write", "name=model-1&kind=write"}
		prev := -1
		for _, want := range wantOrder {
			idx := strings.Index(text, want)
			if idx < 0 {
				t.Fatalf("transcript must carry the event %q, got:\n%s", want, text)
			}
			if idx < prev {
				t.Fatalf("transcript events must appear in application order, %q appears out of order:\n%s", want, text)
			}
			prev = idx
		}
		for _, want := range []string{"\nfirst\n", "\nattempt 1\n", "\nsecond\n"} {
			if !strings.Contains(text, want) {
				t.Fatalf("transcript must carry the content line %q, got:\n%s", want, text)
			}
		}
	})
}

// TestTranscriptEventsParseAsBlocks verifies the transcript's event
// stream is machine-parseable: each event renders as a block of kind
// "event" with its metadata percent-encoded into the URI query, and
// blocks.ParseBlocks recovers every event's metadata and content in
// application order. Every event's delimiter is a Han pair absent from
// its body, so Han-heavy content never collides with its own closing
// marker. The parsed attributes are decoded, so the type value is the
// structured "prefix::name" string. See TheoryOfInteractionRecording
// and tree.TheoryOfTree.
func TestTranscriptEventsParseAsBlocks(t *testing.T) {
	withRecorder(t, true, func(recorder *Recorder) {
		recorder.StartSession("test")
		tr := tree.New().WithOpSink(recorder.Sink())
		if _, err := tr.Write("root", "user-1", tree.TypeUser, tree.AuthorUser, "plain content"); err != nil {
			t.Fatal(err)
		}
		if _, err := tr.Write("root", "user-2", tree.TypeUser, tree.AuthorUser, "carries 漢字 pairs inside"); err != nil {
			t.Fatal(err)
		}
		recorder.EndSession(nil)

		var id int64
		if err := recorder.db.QueryRow(`SELECT id FROM sessions LIMIT 1`).Scan(&id); err != nil {
			t.Fatal(err)
		}
		text, err := Transcript(recorder, id)
		if err != nil {
			t.Fatal(err)
		}
		parsed, err := blocks.ParseBlocks([]byte(text))
		if err != nil {
			t.Fatal(err)
		}
		if len(parsed) != 2 {
			t.Fatalf("expected 2 event blocks, got %d:\n%s", len(parsed), text)
		}
		first, second := parsed[0], parsed[1]
		if first.Kind != "event" || second.Kind != "event" {
			t.Fatalf("blocks must be events, got %q and %q", first.Kind, second.Kind)
		}
		for _, event := range parsed {
			if strings.Contains(event.Body, event.Boundary) {
				t.Fatalf("the delimiter %q must not occur in the body %q", event.Boundary, event.Body)
			}
		}
		if first.Attributes["name"] != "user-1" || first.Attributes["kind"] != "write" ||
			first.Attributes["type"] != "message::user" || first.Attributes["author"] != "user" ||
			first.Attributes["parent"] != "root" {
			t.Fatalf("first block metadata mismatch: %v", first.Attributes)
		}
		if first.Body != "plain content" {
			t.Fatalf("first block body must carry the node content, got %q", first.Body)
		}
		if second.Attributes["name"] != "user-2" || second.Attributes["kind"] != "write" ||
			second.Body != "carries 漢字 pairs inside" {
			t.Fatalf("second block mismatch: %v %q", second.Attributes, second.Body)
		}
	})
}

// TestSelectEventDelimiterDrawsFreshPairs verifies the delimiter's two
// guarantees: every draw is a fresh pair of Han characters — a fixed
// pair could collide with arbitrary recorded content — and the drawn
// delimiter never occurs in the content it delimits, so a block body
// cannot collide with its own closing marker. See
// TheoryOfInteractionRecording.
func TestSelectEventDelimiterDrawsFreshPairs(t *testing.T) {
	content := "carries 漢字 and 中文 pairs inside"
	seen := make(map[string]bool)
	for range 5 {
		delimiter, err := selectEventDelimiter(content)
		if err != nil {
			t.Fatal(err)
		}
		runes := []rune(delimiter)
		if len(runes) != 2 || !unicode.Is(unicode.Han, runes[0]) || !unicode.Is(unicode.Han, runes[1]) {
			t.Fatalf("the delimiter must be two Han characters, got %q", delimiter)
		}
		if strings.Contains(content, delimiter) {
			t.Fatalf("the delimiter %q must not occur in the content", delimiter)
		}
		seen[delimiter] = true
	}
	if len(seen) < 2 {
		t.Fatalf("the delimiter must be drawn at random, got %v", seen)
	}
}
