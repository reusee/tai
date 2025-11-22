package generators

import (
	"cloud.google.com/go/ai/generativelanguage/apiv1beta/generativelanguagepb"
)

type FuncDecl struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Params      Vars   `json:"params"`
	Returns     Vars   `json:"returns"`
}

func (f FuncDecl) ToGemini() *generativelanguagepb.FunctionDeclaration {
	return &generativelanguagepb.FunctionDeclaration{
		Name:        f.Name,
		Description: f.Description,
		Parameters:  f.Params.ToGemini(),
		Response:    f.Returns.ToGemini(),
	}
}

func (f FuncDecl) ToOpenAI() Tool {
	return Tool{
		Type: "function",
		Function: &FunctionDefinition{
			Name:        f.Name,
			Description: f.Description,
			Strict:      true,
			Parameters:  f.Params.ToOpenAI(),
		},
	}
}
