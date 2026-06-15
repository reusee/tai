package codes

import (
	"bytes"
	"fmt"
	"os"

	"github.com/reusee/dscope"
	"github.com/reusee/prompts"
	"github.com/reusee/tai/codes/codetypes"
	"github.com/reusee/tai/generators"
)

type UnifiedDiff struct {
}

var _ codetypes.DiffHandler = UnifiedDiff{}

func (u UnifiedDiff) Functions() []*generators.Function {
	return nil
}

func (u UnifiedDiff) SystemPrompt() string {
	return prompts.UnifiedDiff
}

func (u UnifiedDiff) RestatePrompt() string {
	return prompts.UnifiedDiffRestate
}

func (Module) UnifiedDiff(
	inject dscope.InjectStruct,
) (ret UnifiedDiff) {
	inject(&ret)
	return ret
}

func (u UnifiedDiff) Apply(root *os.Root, diffFilePath string) error {
	content, err := os.ReadFile(diffFilePath)
	if err != nil {
		return err
	}

	for {
		h, start, end, ok := parseFirstHunk(content)
		if !ok {
			break
		}
		if err := applyHunk(root, h); err != nil {
			return fmt.Errorf("hunk %s %s: %w", h.Op, h.Target, err)
		}
		newContent := append(content[:start], content[end:]...)
		if err := os.WriteFile(diffFilePath, bytes.TrimSpace(newContent), 0644); err != nil {
			return err
		}
		content, err = os.ReadFile(diffFilePath)
		if err != nil {
			return err
		}
	}

	return nil
}
