package memories

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/reusee/tai/blocks"
	"github.com/reusee/tai/generators"
)

const TheoryOfMemory = `
Memory persistence is implemented as a per-model user profile stored in
ai-memory.json under the user config directory. Each profile entry records
the time, the model that produced it, and the learned items. The profile is
read into the system prompt so the model receives long-term context about the
user, and is updated after each generation round from memory blocks (or a
textual pseudo-call fallback) emitted by the model.

Block parsing scans every block in the output, not just the first, because a
memory block may be preceded by continue, shell, or summary blocks. Only the
memory kind is consumed; other blocks are skipped and the scan advances past
them. Unclosed blocks (opening marker with no matching end marker) are also
skipped during scanning, so a memory block preceded by an unclosed block of
another kind is still found. The pseudo-call fallback extracts textual
update_user_profile(...) calls when the model fails to use the memory block
format, tolerating both colon and assignment separators and both single- and
double-quoted strings, matching common hallucination patterns.

Memory updates are merged additively rather than replaced: new items are
appended to the existing item list, and a deduplication step prevents the same
item from being recorded twice when it appears in both a memory block and a
textual pseudo-call. The merge never prunes items, so once a fact is recorded
it survives future rounds. This conservative policy protects long-term
continuity of the user profile.

File access is guarded by an advisory lock file with PID-based stale detection
and exponential backoff, so concurrent invocations do not corrupt the shared
profile. Writes are atomic: content is written to a temporary file and renamed
over the target, so a crash mid-write cannot leave a truncated profile. The
profile path may be a symlink, in which case the symlink target is resolved so
updates follow the link rather than replacing it.

A fact-only policy governs what is recorded: only information the user
explicitly expresses or that is confirmed by objective facts. The model must
distinguish a user's topical interest (asking about a subject) from their
personal status (undergoing that subject), preventing the profile from being
polluted with unverified assumptions.
`

type Memory struct {
	Entries []*MemoryEntry
}

type MemoryEntry struct {
	Time  time.Time
	Model string
	Items []string
}

type CurrentMemory func() (*MemoryEntry, error)

type AppendMemory func(*MemoryEntry) error

func (Module) Memory(
	generator generators.Generator,
) (CurrentMemory, AppendMemory) {

	const fileName = "ai-memory.json"

	resolvePath := sync.OnceValues(func() (string, error) {
		configDir, err := os.UserConfigDir()
		if err != nil {
			return "", err
		}
		p := filepath.Join(configDir, fileName)
		fi, err := os.Lstat(p)
		if err != nil {
			if os.IsNotExist(err) {
				return p, nil
			}
			return "", err
		}
		if fi.Mode()&os.ModeSymlink != 0 {
			target, err := os.Readlink(p)
			if err != nil {
				return "", err
			}
			if !filepath.IsAbs(target) {
				target = filepath.Join(filepath.Dir(p), target)
			}
			return target, nil
		}
		return p, nil
	})

	readMemory := func(path string) (*Memory, error) {
		content, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				return &Memory{}, nil
			}
			return nil, err
		}
		var m Memory
		if err := json.NewDecoder(bytes.NewReader(content)).Decode(&m); err != nil {
			return nil, err
		}
		return &m, nil
	}

	writeMemory := func(path string, m *Memory) error {
		buf := new(bytes.Buffer)
		encoder := json.NewEncoder(buf)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(m); err != nil {
			return err
		}
		tmpFilePath := path + fmt.Sprintf(".%d.tmp", rand.Int64())
		if err := os.WriteFile(tmpFilePath, buf.Bytes(), 0644); err != nil {
			return err
		}
		if err := os.Rename(tmpFilePath, path); err != nil {
			os.Remove(tmpFilePath)
			return err
		}
		return nil
	}

	acquireLock := func(lockFilePath string) (func(), error) {
		var locked bool
		const maxRetries = 20
		const baseDelay = 100 * time.Millisecond
		const maxDelay = 2 * time.Second

		for attempt := range maxRetries {
			if f, err := os.OpenFile(lockFilePath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600); err == nil {
				// Write PID to lock file
				fmt.Fprintf(f, "%d", os.Getpid())
				f.Close()
				locked = true
				break
			} else if !os.IsExist(err) {
				return nil, fmt.Errorf("failed to create lock file: %w", err)
			}

			// Check if lock is stale: read PID and verify process
			if data, err := os.ReadFile(lockFilePath); err == nil {
				pidStr := strings.TrimSpace(string(data))
				if pid, err := strconv.Atoi(pidStr); err == nil {
					process, err := os.FindProcess(pid)
					if err != nil {
						os.Remove(lockFilePath)
						continue
					}
					if err := process.Signal(os.Signal(nil)); err != nil {
						// Process not found or no permission; assume stale
						os.Remove(lockFilePath)
						continue
					}
				}
			}

			if attempt < maxRetries-1 {
				delay := min(baseDelay*time.Duration(1<<uint(attempt)), maxDelay)
				time.Sleep(delay)
			}
		}

		if !locked {
			return nil, fmt.Errorf("failed to acquire lock for %s after %d attempts", fileName, maxRetries)
		}

		unlock := func() {
			os.Remove(lockFilePath)
		}
		return unlock, nil
	}

	currentMemory := func() (*MemoryEntry, error) {
		filePath, err := resolvePath()
		if err != nil {
			return nil, err
		}
		m, err := readMemory(filePath)
		if err != nil {
			return nil, err
		}
		if len(m.Entries) == 0 {
			return nil, nil
		}
		model := GetModelID(generator.Spec())
		for _, entry := range slices.Backward(m.Entries) {
			if entry.Model == model {
				return entry, nil
			}
		}
		return nil, nil
	}

	appendMemory := func(entry *MemoryEntry) error {
		filePath, err := resolvePath()
		if err != nil {
			return err
		}
		lockFilePath := filePath + ".lock"

		unlock, err := acquireLock(lockFilePath)
		if err != nil {
			return err
		}
		defer unlock()

		m, err := readMemory(filePath)
		if err != nil {
			return err
		}
		m.Entries = append(m.Entries, entry)
		if err := writeMemory(filePath, m); err != nil {
			return err
		}
		return nil
	}

	return currentMemory, appendMemory
}

type memoryRoot struct {
	Items []string `xml:"memory-item"`
}

func parseMemoryItems(text string) ([]string, error) {
	content := []byte(text)
	for {
		block, _, end, ok, err := blocks.ParseFirstBlock(content)
		if err != nil {
			// Unclosed block: skip past the opening marker and continue
			// scanning for a memory block. See TheoryOfMemory.
			if end > 0 && end <= len(content) {
				content = content[end:]
				continue
			}
			return nil, err
		}
		if !ok {
			return nil, nil
		}
		if block.Kind == "memory" {
			var mem memoryRoot
			if err := xml.Unmarshal([]byte(block.Body), &mem); err != nil {
				return nil, err
			}
			return mem.Items, nil
		}
		// Skip non-memory blocks (e.g., continue, shell, summary) and
		// continue scanning for a memory block. Without this, a memory
		// block preceded by any other block would be silently missed.
		content = content[end:]
	}
}

// UpdateMemoryFromBlock extracts memory items from memory blocks and textual
// pseudo-calls in the assistant output, then merges them with the current
// profile and persists the result. See TheoryOfMemory.
func UpdateMemoryFromBlock(
	currentMemory CurrentMemory,
	appendMemory AppendMemory,
	model string,
	assistantText string,
) error {
	items, err := parseMemoryItems(assistantText)
	if err != nil {
		return err
	}

	// Pseudo-call recovery: detect textual update_user_profile(...) calls
	// that the model emits instead of using the memory block format.
	// See TheoryOfMemory.
	items = append(items, parsePseudoCallItems(assistantText)...)

	if len(items) == 0 {
		return nil
	}

	// Deduplicate items from memory blocks and pseudo-calls.
	seen := make(map[string]bool)
	var uniqueItems []string
	for _, item := range items {
		if !seen[item] {
			seen[item] = true
			uniqueItems = append(uniqueItems, item)
		}
	}
	items = uniqueItems

	current, err := currentMemory()
	if err != nil {
		return err
	}
	var currentItems []string
	if current != nil {
		currentItems = current.Items
	}

	finalItems := slices.Clone(items)
	for _, currentItem := range currentItems {
		found := slices.Contains(items, currentItem)
		if !found {
			finalItems = append(finalItems, currentItem)
		}
	}

	if err := appendMemory(&MemoryEntry{
		Time:  time.Now(),
		Model: model,
		Items: finalItems,
	}); err != nil {
		return err
	}
	return nil
}

var pseudoCallRegex = regexp.MustCompile(`update_user_profile\s*\(\s*(?:items\s*[=:])?\s*(\[[\s\S]*?\])\s*\)`)

var quotedItemRegex = regexp.MustCompile(`"([^"]*)"|'([^']*)'`)

// parsePseudoCallItems scans text for textual update_user_profile(...)
// pseudo-calls and extracts the quoted items from the array argument.
// This is a fallback for when the model fails to use the memory block
// format and instead writes the call as plain text. The extraction
// handles both double-quoted and single-quoted strings, matching the
// robustness requirements described in TheoryOfMemory.
func parsePseudoCallItems(text string) []string {
	matches := pseudoCallRegex.FindAllStringSubmatch(text, -1)
	var items []string
	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		quotedMatches := quotedItemRegex.FindAllStringSubmatch(match[1], -1)
		for _, qm := range quotedMatches {
			if len(qm) >= 3 {
				if qm[1] != "" {
					items = append(items, qm[1])
				} else if qm[2] != "" {
					items = append(items, qm[2])
				}
			}
		}
	}
	return items
}

// GetModelID derives a stable model identifier from a generator spec,
// preferring the family name and falling back to the model name.
func GetModelID(spec generators.Spec) string {
	if spec.Family != "" {
		return filepath.Base(spec.Family)
	}
	return filepath.Base(spec.Model)
}
