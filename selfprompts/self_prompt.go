// Package selfprompts implements the model-maintained system prompt: a map
// of directive entries the model writes for its future sessions, stored in
// .tai/self-prompt in the working directory and rendered into the system
// prompt of the next session or the next goal loop.
// See TheoryOfSelfPrompt.
package selfprompts

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/reusee/tai/blocks"
)

const TheoryOfSelfPrompt = `
A self-prompt is the model's own system prompt: directives the model maintains
for its future sessions. It is not memory — memory records facts about the
user, a self-prompt records rules the model follows. It is not session
context — an entry earns its place only when it benefits the next session.

The entries form a map from a short name to a directive, stored as JSON in
.tai/self-prompt under the working directory, so the file diffs well and the
user reviews it like source. The model operates the map through self-prompt
blocks: the header carries the operation and the entry name as parameters,
and the body carries the directive text. A batch is atomic: every operation
is validated against a copy of the map, and one invalid operation discards
the whole batch with every problem fed back, so the model corrects the batch
in the next round.

Timing defines the mechanism. The entries are read once, when the component
set is constructed: once per session for the ai command, and once per goal
loop because the goal runner resets the scope per loop. An update therefore
never changes the system prompt of the running session, nor of a later
attempt inside the same loop; it becomes visible in the next goal loop or
the next session. A rule the current work must follow stays in the
conversation until then.

The write runs during component processing, after the attempt succeeded and
its summary block arrived, so a failed attempt writes nothing and no
feedback ever reports a write that was rolled back.

The rendered directives are the last system prompt section, so the sections
above stay byte-identical across sessions and remain in the LLM prefix cache
when an entry changes. A session with no entries pays nothing: the section
stays empty and the component contributes no tokens.

The file lives in the working tree and the user reviews and commits it, so
the model writes sound directives only. A dry run validates and reports the
batch without touching the tree.
`

// SelfPromptBlockPrompt teaches the self-prompt block kind. The heredoc
// format itself is blocks.BlockFormatSystemPrompt's; this text covers only
// the kind's semantics, its timing, and the review rule.
// See TheoryOfSelfPrompt.
const SelfPromptBlockPrompt = `**Self-Prompt Blocks:**

A self-prompt block edits the self-prompt map stored in .tai/self-prompt: the
directives you maintain for future sessions. The header carries the operation
and the entry name as parameters:

- op=add, name=<entry name>: create a new entry. The name must be new.
- op=modify, name=<entry name>: replace the text of an existing entry.
- op=delete, name=<entry name>: remove an existing entry. The body is ignored.

For add and modify the body is the entry text: one directive, imperative and
short.

Rules:
- Write directives, not knowledge. A directive is a rule you follow, such as
  "Run the tests before claiming a fix is complete." Facts about the user
  belong to the memory mechanism; context this session needs only once does
  not belong in the file.
- Every entry costs system prompt tokens in every future session. Add an
  entry only when it clearly benefits the next session.
- An update never changes this session's system prompt. It takes effect in
  the next session or the next goal loop, so keep a rule the current work
  must follow in the conversation until then.
- The file is source the user reviews and commits. Write directives that are
  sound, safe, and honest, and that keep the user in control.
- One invalid operation rejects the whole batch and writes nothing. The
  feedback lists every problem; correct the batch and re-emit it.
`

// Kind is the block kind this package owns.
const Kind = "self-prompt"

const (
	// selfPromptDir is the directory holding the file, relative to the
	// working directory.
	selfPromptDir = ".tai"
	// selfPromptPath is the file path, relative to the working directory.
	selfPromptPath = selfPromptDir + "/self-prompt"

	// maxNameRunes bounds an entry name: it is a map key the model types
	// in a block header.
	maxNameRunes = 64
	// maxEntryRunes bounds one directive, because an entry becomes system
	// prompt text in every future session.
	maxEntryRunes = 1000
	// maxTotalRunes bounds the whole map: a self-prompt grown past this
	// size taxes every session without paying for itself.
	maxTotalRunes = 8 << 10
)

// Operation names accepted in the header's op parameter.
const (
	OpAdd    = "add"
	OpModify = "modify"
	OpDelete = "delete"
)

// selfPromptSectionHeader introduces the rendered directives in the system
// prompt. It names the file so the model can point its own blocks at the
// entries it reads here.
const selfPromptSectionHeader = `**Your Self-Prompt:**

Directives you wrote for yourself in earlier sessions, stored in
.tai/self-prompt. Follow every directive in this session. Use a self-prompt
block with an entry name to modify or delete an entry.

`

var namePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// Entries is the self-prompt map: entry name to directive text.
type Entries map[string]string

// Operation is one parsed self-prompt block.
type Operation struct {
	Op   string
	Name string
	Text string
}

// RejectedError reports the validation failures of a batch. Nothing was
// written; the caller feeds every problem back to the model.
type RejectedError struct {
	Problems []error
}

func (e *RejectedError) Error() string {
	var sb strings.Builder
	sb.WriteString("self-prompt batch rejected")
	for _, problem := range e.Problems {
		sb.WriteString("; ")
		sb.WriteString(problem.Error())
	}
	return sb.String()
}

// Content renders the self-prompt of the process working directory for the
// system prompt. A missing file renders nothing; an unreadable or malformed
// file renders nothing too, so a broken file never breaks a session. The
// entries are read at call time, so the caller decides when an update
// becomes visible. See TheoryOfSelfPrompt.
func Content() string {
	entries, err := Load("")
	if err != nil {
		return ""
	}
	return Render(entries)
}

// Load reads the map stored under dir. An empty dir means the process
// working directory. A missing file is an empty map.
func Load(dir string) (Entries, error) {
	path := selfPromptPath
	if dir != "" {
		path = filepath.Join(dir, selfPromptPath)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Entries{}, nil
		}
		return nil, err
	}
	return decode(bytes.NewReader(content))
}

// Render renders the entries as a system prompt section with the names in
// sorted order, so equal maps produce byte-identical text. An empty map
// renders nothing: a session without entries pays no tokens.
func Render(entries Entries) string {
	if len(entries) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString(selfPromptSectionHeader)
	for _, name := range slices.Sorted(maps.Keys(entries)) {
		sb.WriteString(renderEntry(name, entries[name]))
	}
	return sb.String()
}

// Size reports the rune count of the map's names and texts, the quantity
// maxTotalRunes bounds.
func (e Entries) Size() int {
	n := 0
	for name, text := range e {
		n += utf8.RuneCountInString(name) + utf8.RuneCountInString(text)
	}
	return n
}

// Apply applies a batch of self-prompt blocks as one atomic update. Every
// block is parsed and every parsed operation is validated against a copy of
// the stored map, so one round reports every problem at once and the model
// corrects the batch in a single pass. Any problem discards the whole batch
// through a RejectedError: a partially applied batch would leave the map in
// a state the model did not describe. write tells whether the validated map
// reaches the file: a dry run validates and reports without touching the
// tree. On success Apply returns the operations in block order.
// See TheoryOfSelfPrompt.
func Apply(root *os.Root, bs []blocks.Block, write bool) ([]Operation, error) {
	entries, err := loadRoot(root)
	if err != nil {
		return nil, err
	}
	next := make(Entries, len(entries))
	for name, text := range entries {
		next[name] = text
	}

	ops := make([]Operation, 0, len(bs))
	var problems []error
	for _, b := range bs {
		op, err := parseOperation(b)
		if err != nil {
			problems = append(problems, err)
			continue
		}
		ops = append(ops, op)
	}
	// The parsed operations still validate against the working copy when
	// another block failed to parse: the copy is discarded either way, and
	// the model reads every problem of the batch, not only the first.
	for _, op := range ops {
		if err := applyOperation(next, op); err != nil {
			problems = append(problems, err)
		}
	}
	if len(problems) > 0 {
		return nil, &RejectedError{Problems: problems}
	}
	if size := next.Size(); size > maxTotalRunes {
		return nil, &RejectedError{Problems: []error{fmt.Errorf(
			"the map would hold %d runes, over the %d-rune limit; delete entries or shorten their text",
			size, maxTotalRunes,
		)}}
	}
	if write {
		if err := save(root, next); err != nil {
			return nil, err
		}
	}
	return ops, nil
}

// parseOperation reads one block's header and body into an Operation,
// rejecting anything the map cannot hold. See TheoryOfSelfPrompt.
func parseOperation(b blocks.Block) (Operation, error) {
	op := Operation{
		Op:   strings.TrimSpace(b.Attributes["op"]),
		Name: strings.TrimSpace(b.Attributes["name"]),
		Text: strings.TrimSpace(b.Body),
	}
	switch op.Op {
	case OpAdd, OpModify, OpDelete:
	default:
		return Operation{}, fmt.Errorf("%s: the header needs op=add, op=modify, or op=delete, got %q", Kind, op.Op)
	}
	if op.Name == "" {
		return Operation{}, fmt.Errorf("%s %s: the header needs a non-empty name", Kind, op.Op)
	}
	if utf8.RuneCountInString(op.Name) > maxNameRunes || !namePattern.MatchString(op.Name) {
		return Operation{}, fmt.Errorf("%s %q: a name starts with a letter or digit and holds letters, digits, '.', '_', and '-'", Kind, op.Name)
	}
	if op.Op == OpDelete {
		return op, nil
	}
	if op.Text == "" {
		return Operation{}, fmt.Errorf("%s %s %q: the block body is the entry text and must not be empty", Kind, op.Op, op.Name)
	}
	if utf8.RuneCountInString(op.Text) > maxEntryRunes {
		return Operation{}, fmt.Errorf("%s %s %q: the text holds over %d runes; keep one directive short", Kind, op.Op, op.Name, maxEntryRunes)
	}
	return op, nil
}

// applyOperation applies one operation to a working copy of the map.
func applyOperation(entries Entries, op Operation) error {
	_, exists := entries[op.Name]
	switch op.Op {
	case OpAdd:
		if exists {
			return fmt.Errorf("%s add %q: the entry exists; use op=modify", Kind, op.Name)
		}
		entries[op.Name] = op.Text
	case OpModify:
		if !exists {
			return fmt.Errorf("%s modify %q: no such entry; use op=add", Kind, op.Name)
		}
		entries[op.Name] = op.Text
	case OpDelete:
		if !exists {
			return fmt.Errorf("%s delete %q: no such entry", Kind, op.Name)
		}
		delete(entries, op.Name)
	}
	return nil
}

// loadRoot reads the map through root, so the block-driven path stays inside
// the caller's filesystem root.
func loadRoot(root *os.Root) (Entries, error) {
	f, err := root.Open(selfPromptPath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Entries{}, nil
		}
		return nil, err
	}
	defer f.Close()
	return decode(f)
}

// save writes the map to the file through root, creating the .tai directory
// on first use. JSON keeps the file reviewable: one entry per line, keys
// sorted.
func save(root *os.Root, entries Entries) error {
	content, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}
	content = append(content, '\n')
	if err := root.Mkdir(selfPromptDir, 0o755); err != nil && !errors.Is(err, fs.ErrExist) {
		return err
	}
	f, err := root.OpenFile(selfPromptPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.Write(content); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// decode reads the JSON map. An empty file is an empty map.
func decode(r io.Reader) (Entries, error) {
	var entries Entries
	if err := json.NewDecoder(r).Decode(&entries); err != nil {
		if errors.Is(err, io.EOF) {
			return Entries{}, nil
		}
		return nil, err
	}
	if entries == nil {
		entries = Entries{}
	}
	return entries, nil
}

// renderEntry renders one entry as a bullet, indenting continuation lines so
// a multi-line directive stays inside its bullet.
func renderEntry(name, text string) string {
	var sb strings.Builder
	sb.WriteString("- ")
	sb.WriteString(name)
	sb.WriteString(": ")
	for i, line := range strings.Split(text, "\n") {
		if i > 0 {
			sb.WriteString("\n  ")
		}
		sb.WriteString(line)
	}
	sb.WriteString("\n")
	return sb.String()
}
