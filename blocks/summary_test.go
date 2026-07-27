package blocks

import (
	"strings"
	"testing"
)

func TestProcessSummaryBlocks(t *testing.T) {
	blocks := []Block{
		{Kind: "summary", Body: "- Analyzed the code\n- Fixed the Foo function"},
	}
	summaries := ProcessSummaryBlocks(blocks)
	if len(summaries) != 1 {
		t.Fatalf("expected 1 summary, got %d", len(summaries))
	}
	if !strings.Contains(summaries[0], "Fixed the Foo function") {
		t.Fatalf("expected summary to contain the description, got %q", summaries[0])
	}
}

func TestProcessSummaryBlocksMultiple(t *testing.T) {
	blocks := []Block{
		{Kind: "summary", Body: "- Round 1 analysis\n- Round 1 fix"},
		{Kind: "summary", Body: "- Round 2 verification"},
	}
	summaries := ProcessSummaryBlocks(blocks)
	if len(summaries) != 2 {
		t.Fatalf("expected 2 summaries, got %d", len(summaries))
	}
	if !strings.Contains(summaries[0], "Round 1 analysis") {
		t.Fatalf("expected first summary to contain 'Round 1 analysis', got %q", summaries[0])
	}
	if !strings.Contains(summaries[1], "Round 2 verification") {
		t.Fatalf("expected second summary to contain 'Round 2 verification', got %q", summaries[1])
	}
}

func TestProcessSummaryBlocksNoBlock(t *testing.T) {
	summaries := ProcessSummaryBlocks(nil)
	if len(summaries) != 0 {
		t.Fatalf("expected 0 summaries, got %d", len(summaries))
	}
}
