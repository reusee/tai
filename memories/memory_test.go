package memories

import (
	"context"
	"testing"

	"github.com/reusee/dscope"
	"github.com/reusee/tai/generators"
	"github.com/reusee/tai/modes"
)

type mockGenerator struct{}

func (mockGenerator) Spec() generators.Spec { return generators.Spec{} }

func (mockGenerator) CountTokens(string) (int, error) { return 0, nil }

func (mockGenerator) Generate(context.Context, generators.State, *generators.GenerateOptions) (generators.State, error) {
	return nil, nil
}

func TestParseMemoryUpdate(t *testing.T) {
	cases := []struct {
		name    string
		text    string
		adds    []string
		deletes []string
	}{
		{
			name: "first block is memory",
			text: "<<龘靐 memory\n<memory>\n  <memory-item>user likes Go</memory-item>\n</memory>\n龘靐\n",
			adds: []string{"user likes Go"},
		},
		{
			name: "no memory block",
			text: "<<龘靐 continue\ncontinue content\n龘靐\n",
		},
		{
			name: "skips non-memory blocks",
			text: "<<龘靐 continue\ncontinue content\n龘靐\n" +
				"<<齉爩 memory\n<memory>\n  <memory-item>user likes Go</memory-item>\n</memory>\n齉爩\n",
			adds: []string{"user likes Go"},
		},
		{
			name: "skips unclosed blocks",
			text: "<<龘靐 finish\nSome summary.\n<<齉爩 memory\n<memory>\n  <memory-item>user likes Go</memory-item>\n</memory>\n齉爩\n",
			adds: []string{"user likes Go"},
		},
		{
			name: "multiple non-memory blocks",
			text: "<<龘靐 summary\nsummary text\n龘靐\n" +
				"<<齉爩 continue\ncontinue content\n齉爩\n" +
				"<<麤黿 memory\n<memory>\n  <memory-item>item1</memory-item>\n  <memory-item>item2</memory-item>\n</memory>\n麤黿\n",
			adds: []string{"item1", "item2"},
		},
		{
			name:    "adds and deletes",
			text:    "<<龘靐 memory\n<memory>\n  <memory-item>user likes Go</memory-item>\n  <memory-delete>user knows Python</memory-delete>\n</memory>\n龘靐\n",
			adds:    []string{"user likes Go"},
			deletes: []string{"user knows Python"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			update, err := parseMemoryUpdate(tc.text)
			if err != nil {
				t.Fatal(err)
			}
			if len(update.Adds) != len(tc.adds) || len(update.Deletes) != len(tc.deletes) {
				t.Fatalf("got adds %v deletes %v, want adds %v deletes %v",
					update.Adds, update.Deletes, tc.adds, tc.deletes)
			}
			for i, want := range tc.adds {
				if update.Adds[i] != want {
					t.Fatalf("adds[%d] = %q, want %q", i, update.Adds[i], want)
				}
			}
			for i, want := range tc.deletes {
				if update.Deletes[i] != want {
					t.Fatalf("deletes[%d] = %q, want %q", i, update.Deletes[i], want)
				}
			}
		})
	}
}

func TestParsePseudoCallItems(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		expected []string
	}{
		{
			name:     "standard format with items keyword and colon",
			text:     `update_user_profile(items: ["user likes Go", "user knows Python"])`,
			expected: []string{"user likes Go", "user knows Python"},
		},
		{
			name:     "assignment operator instead of colon",
			text:     `update_user_profile(items=["user likes Go"])`,
			expected: []string{"user likes Go"},
		},
		{
			name:     "single quotes",
			text:     `update_user_profile(items=['user likes Go'])`,
			expected: []string{"user likes Go"},
		},
		{
			name:     "without items keyword",
			text:     `update_user_profile(["user likes Go"])`,
			expected: []string{"user likes Go"},
		},
		{
			name:     "no pseudo-call",
			text:     "regular text without any pseudo-call",
			expected: nil,
		},
		{
			name:     "multiple pseudo-calls",
			text:     `update_user_profile(items=["item1"]) and update_user_profile(items=["item2"])`,
			expected: []string{"item1", "item2"},
		},
		{
			name:     "mixed quotes",
			text:     `update_user_profile(items=["double quoted", 'single quoted'])`,
			expected: []string{"double quoted", "single quoted"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parsePseudoCallItems(tt.text)
			if len(got) != len(tt.expected) {
				t.Fatalf("expected %d items, got %d: %v", len(tt.expected), len(got), got)
			}
			for i, expected := range tt.expected {
				if got[i] != expected {
					t.Errorf("item %d: expected %q, got %q", i, expected, got[i])
				}
			}
		})
	}
}

func TestParsePseudoCallDeletes(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		expected []string
	}{
		{
			name:     "standard format with items keyword and colon",
			text:     `delete_user_profile(items: ["stale item", "wrong fact"])`,
			expected: []string{"stale item", "wrong fact"},
		},
		{
			name:     "assignment operator instead of colon",
			text:     `delete_user_profile(items=["stale item"])`,
			expected: []string{"stale item"},
		},
		{
			name:     "single quotes",
			text:     `delete_user_profile(items=['stale item'])`,
			expected: []string{"stale item"},
		},
		{
			name:     "without items keyword",
			text:     `delete_user_profile(["stale item"])`,
			expected: []string{"stale item"},
		},
		{
			name:     "update call is not a delete call",
			text:     `update_user_profile(items=["user likes Go"])`,
			expected: nil,
		},
		{
			name:     "no pseudo-call",
			text:     "regular text without any pseudo-call",
			expected: nil,
		},
		{
			name:     "multiple delete pseudo-calls",
			text:     `delete_user_profile(items=["item1"]) and delete_user_profile(items=["item2"])`,
			expected: []string{"item1", "item2"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parsePseudoCallDeletes(tt.text)
			if len(got) != len(tt.expected) {
				t.Fatalf("expected %d items, got %d: %v", len(tt.expected), len(got), got)
			}
			for i, expected := range tt.expected {
				if got[i] != expected {
					t.Errorf("item %d: expected %q, got %q", i, expected, got[i])
				}
			}
		})
	}
}

func TestUpdateMemoryFromBlockCombinesBlockAndPseudoCall(t *testing.T) {
	var appended *MemoryEntry
	currentMemory := func() (*MemoryEntry, error) {
		return nil, nil
	}
	appendMemory := func(entry *MemoryEntry) error {
		appended = entry
		return nil
	}

	text := "<<龘靐 memory\n<memory>\n  <memory-item>from block</memory-item>\n</memory>\n龘靐\n" +
		"update_user_profile(items=['from pseudo-call'])"

	dscope.New(
		modes.ForTest(t),
		new(Module),
	).Fork(
		func() generators.GetDefaultGenerator {
			return func() (generators.Generator, error) {
				return mockGenerator{}, nil
			}
		},
		func() CurrentMemory { return currentMemory },
		func() AppendMemory { return appendMemory },
	).Call(func(
		updateFn UpdateMemoryFromBlock,
	) {
		err := updateFn("test-model", text)
		if err != nil {
			t.Fatal(err)
		}
		if appended == nil {
			t.Fatal("expected memory entry to be appended")
		}
		if len(appended.Items) != 2 {
			t.Fatalf("expected 2 items, got %d: %v", len(appended.Items), appended.Items)
		}
		if appended.Items[0] != "from block" || appended.Items[1] != "from pseudo-call" {
			t.Fatalf("unexpected items: %v", appended.Items)
		}
	})
}

func TestUpdateMemoryFromBlockDeduplicates(t *testing.T) {
	var appended *MemoryEntry
	currentMemory := func() (*MemoryEntry, error) {
		return nil, nil
	}
	appendMemory := func(entry *MemoryEntry) error {
		appended = entry
		return nil
	}

	text := "<<龘靐 memory\n<memory>\n  <memory-item>duplicate</memory-item>\n</memory>\n龘靐\n" +
		"update_user_profile(items=['duplicate'])"

	dscope.New(
		modes.ForTest(t),
		new(Module),
	).Fork(
		func() generators.GetDefaultGenerator {
			return func() (generators.Generator, error) {
				return mockGenerator{}, nil
			}
		},
		func() CurrentMemory { return currentMemory },
		func() AppendMemory { return appendMemory },
	).Call(func(
		updateFn UpdateMemoryFromBlock,
	) {
		err := updateFn("test-model", text)
		if err != nil {
			t.Fatal(err)
		}
		if appended == nil {
			t.Fatal("expected memory entry to be appended")
		}
		if len(appended.Items) != 1 {
			t.Fatalf("expected 1 deduplicated item, got %d: %v", len(appended.Items), appended.Items)
		}
	})
}

func TestUpdateMemoryFromBlockWithPseudoCallOnly(t *testing.T) {
	var appended *MemoryEntry
	currentMemory := func() (*MemoryEntry, error) {
		return nil, nil
	}
	appendMemory := func(entry *MemoryEntry) error {
		appended = entry
		return nil
	}

	text := `I'll remember that. update_user_profile(items=["user likes Go"])`

	dscope.New(
		modes.ForTest(t),
		new(Module),
	).Fork(
		func() generators.GetDefaultGenerator {
			return func() (generators.Generator, error) {
				return mockGenerator{}, nil
			}
		},
		func() CurrentMemory { return currentMemory },
		func() AppendMemory { return appendMemory },
	).Call(func(
		updateFn UpdateMemoryFromBlock,
	) {
		err := updateFn("test-model", text)
		if err != nil {
			t.Fatal(err)
		}
		if appended == nil {
			t.Fatal("expected memory entry to be appended")
		}
		if len(appended.Items) != 1 {
			t.Fatalf("expected 1 item from pseudo-call, got %d: %v", len(appended.Items), appended.Items)
		}
		if appended.Items[0] != "user likes Go" {
			t.Fatalf("expected 'user likes Go', got %q", appended.Items[0])
		}
	})
}

func TestUpdateMemoryFromBlockDeletesItems(t *testing.T) {
	var appended *MemoryEntry
	currentMemory := func() (*MemoryEntry, error) {
		return &MemoryEntry{
			Model: "test-model",
			Items: []string{"user likes Go", "user knows Python", "stale item"},
		}, nil
	}
	appendMemory := func(entry *MemoryEntry) error {
		appended = entry
		return nil
	}

	text := "<<龘靐 memory\n<memory>\n  <memory-item>user likes Go</memory-item>\n  <memory-delete>stale item</memory-delete>\n</memory>\n龘靐\n"

	dscope.New(
		modes.ForTest(t),
		new(Module),
	).Fork(
		func() generators.GetDefaultGenerator {
			return func() (generators.Generator, error) {
				return mockGenerator{}, nil
			}
		},
		func() CurrentMemory { return currentMemory },
		func() AppendMemory { return appendMemory },
	).Call(func(
		updateFn UpdateMemoryFromBlock,
	) {
		err := updateFn("test-model", text)
		if err != nil {
			t.Fatal(err)
		}
		if appended == nil {
			t.Fatal("expected memory entry to be appended")
		}
		if len(appended.Items) != 2 {
			t.Fatalf("expected 2 items after deletion, got %d: %v", len(appended.Items), appended.Items)
		}
		if appended.Items[0] != "user likes Go" || appended.Items[1] != "user knows Python" {
			t.Fatalf("unexpected items: %v", appended.Items)
		}
	})
}

func TestUpdateMemoryFromBlockDeleteWinsOverAdd(t *testing.T) {
	var appended *MemoryEntry
	currentMemory := func() (*MemoryEntry, error) {
		return nil, nil
	}
	appendMemory := func(entry *MemoryEntry) error {
		appended = entry
		return nil
	}

	text := "<<龘靐 memory\n<memory>\n  <memory-item>item</memory-item>\n  <memory-delete>item</memory-delete>\n</memory>\n龘靐\n"

	dscope.New(
		modes.ForTest(t),
		new(Module),
	).Fork(
		func() generators.GetDefaultGenerator {
			return func() (generators.Generator, error) {
				return mockGenerator{}, nil
			}
		},
		func() CurrentMemory { return currentMemory },
		func() AppendMemory { return appendMemory },
	).Call(func(
		updateFn UpdateMemoryFromBlock,
	) {
		err := updateFn("test-model", text)
		if err != nil {
			t.Fatal(err)
		}
		if appended == nil {
			t.Fatal("expected memory entry to be appended")
		}
		if len(appended.Items) != 0 {
			t.Fatalf("expected deletion to win over addition, got %v", appended.Items)
		}
	})
}

func TestUpdateMemoryFromBlockDeleteOnlyPersistsEntry(t *testing.T) {
	var appended *MemoryEntry
	currentMemory := func() (*MemoryEntry, error) {
		return &MemoryEntry{
			Model: "test-model",
			Items: []string{"stale item"},
		}, nil
	}
	appendMemory := func(entry *MemoryEntry) error {
		appended = entry
		return nil
	}

	text := `That fact is outdated. delete_user_profile(items=["stale item"])`

	dscope.New(
		modes.ForTest(t),
		new(Module),
	).Fork(
		func() generators.GetDefaultGenerator {
			return func() (generators.Generator, error) {
				return mockGenerator{}, nil
			}
		},
		func() CurrentMemory { return currentMemory },
		func() AppendMemory { return appendMemory },
	).Call(func(
		updateFn UpdateMemoryFromBlock,
	) {
		err := updateFn("test-model", text)
		if err != nil {
			t.Fatal(err)
		}
		if appended == nil {
			t.Fatal("expected a deletion-only round to persist an entry")
		}
		if len(appended.Items) != 0 {
			t.Fatalf("expected 0 items after deleting the only item, got %v", appended.Items)
		}
	})
}
