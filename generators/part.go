package generators

import (
	"fmt"

	"cloud.google.com/go/ai/generativelanguage/apiv1beta/generativelanguagepb"
	"google.golang.org/protobuf/types/known/structpb"
)

type Part interface {
	isPart()
	ToGemini() (*generativelanguagepb.Part, error)
}

type Text string

func (Text) isPart() {}

func (t Text) ToGemini() (*generativelanguagepb.Part, error) {
	return &generativelanguagepb.Part{
		Data: &generativelanguagepb.Part_Text{
			Text: string(t),
		},
	}, nil
}

type Thought string

func (Thought) isPart() {}

func (t Thought) ToGemini() (*generativelanguagepb.Part, error) {
	return &generativelanguagepb.Part{
		Data: &generativelanguagepb.Part_Text{
			Text: string(t),
		},
		Thought: true,
	}, nil
}

type FileURL string

func (FileURL) isPart() {}

func (f FileURL) ToGemini() (*generativelanguagepb.Part, error) {
	return &generativelanguagepb.Part{
		Data: &generativelanguagepb.Part_FileData{
			FileData: &generativelanguagepb.FileData{
				FileUri: string(f),
			},
		},
	}, nil
}

type FileContent struct {
	Content  []byte
	MimeType string
}

func (FileContent) isPart() {}

func (f FileContent) ToGemini() (*generativelanguagepb.Part, error) {
	return &generativelanguagepb.Part{
		Data: &generativelanguagepb.Part_InlineData{
			InlineData: &generativelanguagepb.Blob{
				MimeType: f.MimeType,
				Data:     f.Content,
			},
		},
	}, nil
}

type FuncCall struct {
	ID     string
	Name   string
	Args   map[string]any
	Origin any
}

func (FuncCall) isPart() {}

func (f FuncCall) ToGemini() (*generativelanguagepb.Part, error) {
	// If Origin is set, it means this FuncCall came from a Gemini response
	// and we should reuse the original protobuf part.
	if f.Origin != nil {
		if pbPart, ok := f.Origin.(*generativelanguagepb.Part); ok {
			return pbPart, nil
		}
	}
	// Otherwise, construct a new FunctionCall part for Gemini
	s, err := structpb.NewStruct(f.Args)
	if err != nil {
		return nil, err
	}
	return &generativelanguagepb.Part{
		Data: &generativelanguagepb.Part_FunctionCall{
			FunctionCall: &generativelanguagepb.FunctionCall{
				Id:   f.ID,
				Name: f.Name,
				Args: s,
			},
		},
	}, nil
}

type CallResult struct {
	ID      string
	Name    string
	Results map[string]any
}

func (CallResult) isPart() {}

func (c CallResult) ToGemini() (*generativelanguagepb.Part, error) {
	s, err := structpb.NewStruct(c.Results)
	if err != nil {
		return nil, err
	}
	return &generativelanguagepb.Part{
		Data: &generativelanguagepb.Part_FunctionResponse{
			FunctionResponse: &generativelanguagepb.FunctionResponse{
				Id:       c.ID,
				Name:     c.Name,
				Response: s,
			},
		},
	}, nil
}

type FinishReason string

func (FinishReason) isPart() {}

func (FinishReason) ToGemini() (*generativelanguagepb.Part, error) {
	return nil, nil
}

type Usage struct {
	Prompt struct {
		TokenCount       int
		TokenCountCached int
	}
	Candidates struct {
		TokenCount int
	}
	Thoughts struct {
		TokenCount int
	}
}

func (Usage) isPart() {}

func (Usage) ToGemini() (*generativelanguagepb.Part, error) {
	return nil, nil
}

type Error struct {
	Error error
}

func (Error) isPart() {}

func (Error) ToGemini() (*generativelanguagepb.Part, error) {
	return nil, nil
}

func PartFromGemini(part *generativelanguagepb.Part) (Part, error) {
	switch data := part.Data.(type) {

	case *generativelanguagepb.Part_Text:
		if part.Thought {
			return Thought(data.Text), nil
		} else {
			return Text(data.Text), nil
		}

	case *generativelanguagepb.Part_CodeExecutionResult:
		output := data.CodeExecutionResult.GetOutput()
		return Text(output), nil

	case *generativelanguagepb.Part_ExecutableCode:
		code := data.ExecutableCode.GetCode()
		return Text(code), nil

	case *generativelanguagepb.Part_FileData:
		return FileURL(data.FileData.FileUri), nil

	case *generativelanguagepb.Part_FunctionResponse:
		return CallResult{
			ID:      data.FunctionResponse.Id,
			Name:    data.FunctionResponse.Name,
			Results: data.FunctionResponse.GetResponse().AsMap(),
		}, nil

	case *generativelanguagepb.Part_FunctionCall:
		call := data.FunctionCall
		return FuncCall{
			ID:     call.Id,
			Name:   call.Name,
			Args:   call.Args.AsMap(),
			Origin: part,
		}, nil

	case *generativelanguagepb.Part_InlineData:
		inlineData := data.InlineData
		return FileContent{
			Content:  inlineData.Data,
			MimeType: inlineData.MimeType,
		}, nil

	}

	return nil, fmt.Errorf("unknown part type: %T", part)
}
