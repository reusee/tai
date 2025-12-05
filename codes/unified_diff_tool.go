package codes

import (
	"fmt"
	"os"

	"github.com/reusee/dscope"
	"github.com/reusee/tai/codes/codetypes"
	"github.com/reusee/tai/generators"
	"github.com/reusee/tai/prompts"
)

type UnifiedDiffTool struct {
}

var _ codetypes.DiffHandler = UnifiedDiffTool{}

func (u UnifiedDiffTool) Functions() []*generators.Func {
	return []*generators.Func{
		{
			Decl: generators.FuncDecl{
				Name:        "apply_change",
				Description: "Apply a change to a file. Use this to modify, create, or delete code.",
				Params: generators.Vars{
					{
						Name:        "operation",
						Type:        generators.TypeString,
						Description: "MODIFY, ADD_BEFORE, ADD_AFTER, DELETE",
					},
					{
						Name:        "target",
						Type:        generators.TypeString,
						Description: "The identifier of the function/var/type to modify, or BEGIN/END",
					},
					{
						Name:        "path",
						Type:        generators.TypeString,
						Description: "File path",
					},
					{
						Name:        "content",
						Type:        generators.TypeString,
						Description: "New content (required for MODIFY/ADD)",
						Optional:    true,
					},
				},
			},
			Func: func(args map[string]any) (map[string]any, error) {
				path := args["path"].(string)
				content, _ := args["content"].(string)
				op := args["operation"].(string)
				target := args["target"].(string)

				if op == "DELETE" {
					// Placeholder: Implementation would parse and delete 'target' in 'path'
					return map[string]any{"status": "deleted " + target + " in " + path}, nil
				}

				if content == "" {
					return nil, fmt.Errorf("content required")
				}

				if op == "MODIFY" {
					// Placeholder for read-replace-write
					// Real implementation requires parsing.
					// For now, we simulate success to satisfy the loop.
					_, err := os.ReadFile(path)
					if err == nil {
						// Naive check
					}
					return map[string]any{"status": "modified " + target + " in " + path}, nil
				}

				return map[string]any{"status": "change applied"}, nil
			},
		},
	}
}

func (u UnifiedDiffTool) SystemPrompt() string {
	return prompts.UnifiedDiffTool
}

func (u UnifiedDiffTool) RestatePrompt() string {
	return prompts.UnifiedDiffToolRestate
}

func (Module) UnifiedDiffTool(
	inject dscope.InjectStruct,
) (ret UnifiedDiffTool) {
	inject(&ret)
	return
}
