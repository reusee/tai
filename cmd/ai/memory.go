package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"iter"
	"math/rand/v2"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/reusee/tai/generators"
)

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
		model := getModelID(generator.Spec())
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

type UpdateMemoryFunc *generators.Func

var pseudoCallRegex = regexp.MustCompile(`update_user_profile\s*\(\s*(?:items\s*[=:])?\s*(\[[\s\S]*?\])\s*\)`)

type PseudoCallState struct {
	upstream generators.State
}

func NewPseudoCallState(upstream generators.State) PseudoCallState {
	return PseudoCallState{
		upstream: upstream,
	}
}

func (p PseudoCallState) AppendContent(content *generators.Content) (generators.State, error) {
	if content == nil {
		return p.upstream.AppendContent(content)
	}
	var newParts []generators.Part
	found := false
	for _, part := range content.Parts {
		newParts = append(newParts, part)
		if text, ok := part.(generators.Text); ok {
			matches := pseudoCallRegex.FindAllStringSubmatch(string(text), -1)
			for _, match := range matches {
				var items []any
				data := []byte(match[1])
				if err := json.Unmarshal(data, &items); err != nil {
					// Fallback for single quotes common in hallucinations
					data = bytes.ReplaceAll(data, []byte("'"), []byte("\""))
					if err := json.Unmarshal(data, &items); err != nil {
						continue
					}
				}
				found = true
				newParts = append(newParts, generators.FuncCall{
					ID:   fmt.Sprintf("pseudo_%d", rand.Int64()),
					Name: "update_user_profile",
					Arguments: map[string]any{
						"items": items,
					},
				})
			}
		}
	}
	if !found {
		next, err := p.upstream.AppendContent(content)
		if err != nil {
			return nil, err
		}
		return PseudoCallState{upstream: next}, nil
	}
	newContent := *content
	newContent.Parts = newParts
	next, err := p.upstream.AppendContent(&newContent)
	if err != nil {
		return nil, err
	}
	return PseudoCallState{upstream: next}, nil
}

func (p PseudoCallState) Contents() iter.Seq[*generators.Content] {
	return p.upstream.Contents()
}

func (p PseudoCallState) Flush() (generators.State, error) {
	next, err := p.upstream.Flush()
	if err != nil {
		return nil, err
	}
	return PseudoCallState{upstream: next}, nil
}

func (p PseudoCallState) FuncMap() map[string]*generators.Func {
	return p.upstream.FuncMap()
}

func (p PseudoCallState) SystemPrompt() string {
	return p.upstream.SystemPrompt()
}

func (p PseudoCallState) Unwrap() generators.State {
	return p.upstream
}

var _ generators.State = PseudoCallState{}

func (Module) UpdateMemoryFunc(
	currentMemory CurrentMemory,
	appendMemory AppendMemory,
	generator generators.Generator,
) UpdateMemoryFunc {
	return &generators.Func{
		Decl: generators.FuncDecl{
			Name:        "update_user_profile",
			Description: "update user profile with new or changed information",
			Params: generators.Vars{
				{
					Name:        "items",
					Description: "user profile items",
					Type:        generators.TypeArray,
					ItemType: &generators.Var{
						Type: generators.TypeString,
					},
				},
			},
		},
		Func: func(args map[string]any) (map[string]any, error) {
			var items []string
			if v, ok := args["items"].([]any); ok {
				for _, val := range v {
					items = append(items, val.(string))
				}
			}
			current, err := currentMemory()
			if err != nil {
				return nil, err
			}
			var currentItems []string
			if current != nil {
				currentItems = current.Items
			}

			// Start with the items provided by the model
			finalItems := slices.Clone(items)

			// Ensure no deletions: verify every current item is still present
			for _, currentItem := range currentItems {
				found := false
				for _, item := range items {
					if item == currentItem {
						found = true
						break
					}
				}
				// If missing, add it back to ensure history preservation
				if !found {
					finalItems = append(finalItems, currentItem)
				}
			}

			model := getModelID(generator.Spec())
			if err := appendMemory(&MemoryEntry{
				Time:  time.Now(),
				Model: model,
				Items: finalItems,
			}); err != nil {
				return nil, err
			}
			return map[string]any{
				"result": "updated",
			}, nil
		},
	}
}

func getModelID(spec generators.Spec) string {
	name := spec.Name
	if name == "" {
		name = spec.Model
	}
	return filepath.Base(name)
}
