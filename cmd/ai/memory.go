package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"slices"
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
	var memory Memory
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

	load := sync.OnceValue(func() error {
		filePath, err := resolvePath()
		if err != nil {
			return err
		}

		content, err := os.ReadFile(filePath)
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}

		if err := json.NewDecoder(bytes.NewReader(content)).Decode(&memory); err != nil {
			return err
		}

		return nil
	})

	currentMemory := func() (*MemoryEntry, error) {
		if err := load(); err != nil {
			return nil, err
		}

		if len(memory.Entries) == 0 {
			return nil, nil
		}

		model := filepath.Base(generator.Args().Model)
		for _, entry := range slices.Backward(memory.Entries) {
			if entry.Model == model {
				return entry, nil
			}
		}

		return nil, nil
	}

	appendMemory := func(entry *MemoryEntry) error {
		if err := load(); err != nil {
			return err
		}

		filePath, err := resolvePath()
		if err != nil {
			return err
		}
		lockFilePath := filePath + ".lock"

		// Enhanced lock acquisition with exponential backoff
		var locked bool
		const maxRetries = 20
		const baseDelay = 100 * time.Millisecond
		const maxDelay = 2 * time.Second

		for attempt := range maxRetries {
			if f, err := os.OpenFile(lockFilePath, os.O_CREATE|os.O_EXCL, 0600); err == nil {
				f.Close()
				locked = true
				break
			} else if !os.IsExist(err) {
				return fmt.Errorf("failed to create lock file: %w", err)
			}

			// Exponential backoff with jitter
			if attempt < maxRetries-1 {
				delay := min(baseDelay*time.Duration(1<<uint(attempt)), maxDelay)
				time.Sleep(delay)
			}
		}

		if !locked {
			return fmt.Errorf("failed to acquire lock for %s after %d attempts", fileName, maxRetries)
		}

		// Ensure lock file is cleaned up
		defer func() {
			if removeErr := os.Remove(lockFilePath); removeErr != nil {
				// Log error but don't fail the operation
				fmt.Fprintf(os.Stderr, "Warning: failed to remove lock file %s: %v\n", lockFilePath, removeErr)
			}
		}()

		memory.Entries = append(memory.Entries, entry)

		// Save to file atomically with validation
		buf := new(bytes.Buffer)
		encoder := json.NewEncoder(buf)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(memory); err != nil {
			return fmt.Errorf("failed to encode memory: %w", err)
		}

		tmpFilePath := filePath + fmt.Sprintf(".%d.tmp", rand.Int64())

		if err := os.WriteFile(tmpFilePath, buf.Bytes(), 0644); err != nil {
			return fmt.Errorf("failed to write temporary file: %w", err)
		}

		if err := os.Rename(tmpFilePath, filePath); err != nil {
			os.Remove(tmpFilePath)
			return fmt.Errorf("failed to rename temporary file: %w", err)
		}

		return nil
	}

	return currentMemory, appendMemory
}

type UpdateMemoryFunc *generators.Func

func (Module) UpdateMemoryFunc(
	appendMemory AppendMemory,
	generator generators.Generator,
) UpdateMemoryFunc {
	return &generators.Func{
		Decl: generators.FuncDecl{
			Name:        "set_user_profile",
			Description: "update user profile",
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
			for _, v := range args["items"].([]any) {
				items = append(items, v.(string))
			}
			model := filepath.Base(generator.Args().Model)
			if err := appendMemory(&MemoryEntry{
				Time:  time.Now(),
				Model: model,
				Items: items,
			}); err != nil {
				return nil, err
			}
			return map[string]any{
				"result": "updated",
			}, nil
		},
	}
}
