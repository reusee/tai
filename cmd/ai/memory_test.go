package main

import (
	"testing"
)

func TestParseMemoryItemsSkipsNonMemoryBlocks(t *testing.T) {
	// Reproduction: when a continue block precedes a memory block,
	// parseMemoryItems must scan past the continue block to find
	// the memory block. Before the fix, only the first block was
	// checked, so the memory block was silently missed.
	text := ":::徕珑 <continue>\ncontinue content\n:::徕珑 </continue>\n" +
		":::栢彣 <memory>\n<memory>\n  <memory-item>user likes Go</memory-item>\n</memory>\n:::栢彣 </memory>\n"

	items, err := parseMemoryItems(text)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 memory item, got %d: %v", len(items), items)
	}
	if items[0] != "user likes Go" {
		t.Fatalf("expected 'user likes Go', got %q", items[0])
	}
}

func TestParseMemoryItemsNoMemoryBlock(t *testing.T) {
	text := ":::徕珑 <continue>\ncontinue content\n:::徕珑 </continue>\n"

	items, err := parseMemoryItems(text)
	if err != nil {
		t.Fatal(err)
	}
	if items != nil {
		t.Fatalf("expected nil items when no memory block exists, got %v", items)
	}
}

func TestParseMemoryItemsFirstBlockIsMemory(t *testing.T) {
	text := ":::徕珑 <memory>\n<memory>\n  <memory-item>user likes Go</memory-item>\n</memory>\n:::徕珑 </memory>\n"

	items, err := parseMemoryItems(text)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 memory item, got %d", len(items))
	}
	if items[0] != "user likes Go" {
		t.Fatalf("expected 'user likes Go', got %q", items[0])
	}
}

func TestParseMemoryItemsMultipleNonMemoryBlocks(t *testing.T) {
	text := ":::徕珑 <summary>\nsummary text\n:::徕珑 </summary>\n" +
		":::栢彣 <continue>\ncontinue content\n:::栢彣 </continue>\n" +
		":::骐骎 <memory>\n<memory>\n  <memory-item>item1</memory-item>\n  <memory-item>item2</memory-item>\n</memory>\n:::骐骎 </memory>\n"

	items, err := parseMemoryItems(text)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 memory items, got %d: %v", len(items), items)
	}
	if items[0] != "item1" || items[1] != "item2" {
		t.Fatalf("expected ['item1', 'item2'], got %v", items)
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

func TestUpdateMemoryFromBlockCombinesBlockAndPseudoCall(t *testing.T) {
	var appended *MemoryEntry
	currentMemory := func() (*MemoryEntry, error) {
		return nil, nil
	}
	appendMemory := func(entry *MemoryEntry) error {
		appended = entry
		return nil
	}

	text := ":::徕珑 <memory>\n<memory>\n  <memory-item>from block</memory-item>\n</memory>\n:::徕珑 </memory>\n" +
		"update_user_profile(items=['from pseudo-call'])"

	err := updateMemoryFromBlock(currentMemory, appendMemory, "test-model", text)
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

	// Same item from both memory block and pseudo-call
	text := ":::徕珑 <memory>\n<memory>\n  <memory-item>duplicate</memory-item>\n</memory>\n:::徕珑 </memory>\n" +
		"update_user_profile(items=['duplicate'])"

	err := updateMemoryFromBlock(currentMemory, appendMemory, "test-model", text)
	if err != nil {
		t.Fatal(err)
	}
	if appended == nil {
		t.Fatal("expected memory entry to be appended")
	}
	if len(appended.Items) != 1 {
		t.Fatalf("expected 1 deduplicated item, got %d: %v", len(appended.Items), appended.Items)
	}
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

	// No memory block, only a textual pseudo-call
	text := `I'll remember that. update_user_profile(items=["user likes Go"])`

	err := updateMemoryFromBlock(currentMemory, appendMemory, "test-model", text)
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
}
