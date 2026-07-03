package main

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

	"github.com/reusee/tai/codes"
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

type memoryRoot struct {
	Items []string `xml:"memory-item"`
}

func parseMemoryItems(text string) ([]string, error) {
	block, _, _, ok, err := codes.ParseFirstBlock([]byte(text))
	if err != nil {
		return nil, err
	}
	if !ok || block.Kind != "memory" {
		return nil, nil
	}
	var mem memoryRoot
	if err := xml.Unmarshal([]byte(block.Body), &mem); err != nil {
		return nil, err
	}
	return mem.Items, nil
}

func updateMemoryFromBlock(
	currentMemory CurrentMemory,
	appendMemory AppendMemory,
	model string,
	assistantText string,
) error {
	items, err := parseMemoryItems(assistantText)
	if err != nil {
		return err
	}
	if len(items) == 0 {
		return nil
	}

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

func getModelID(spec generators.Spec) string {
	if spec.Family != "" {
		return filepath.Base(spec.Family)
	}
	return filepath.Base(spec.Model)
}
