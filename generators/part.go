package generators

import (
	"google.golang.org/genai"
)

type Part interface {
	isPart()
	ToGemini() (*genai.Part, error)
}

type Text string

func (Text) isPart() {}

func (t Text) ToGemini() (*genai.Part, error) {
	if len(t) == 0 {
		return nil, nil
	}
	return &genai.Part{
		Text: string(t),
	}, nil
}

type Thought string

func (Thought) isPart() {}

func (t Thought) ToGemini() (*genai.Part, error) {
	return nil, nil
}

type FileURL string

func (FileURL) isPart() {}

func (f FileURL) ToGemini() (*genai.Part, error) {
	return &genai.Part{
		FileData: &genai.FileData{
			FileURI: string(f),
		},
	}, nil
}

type FileContent struct {
	Content  []byte
	MimeType string
}

func (FileContent) isPart() {}

func (f FileContent) ToGemini() (*genai.Part, error) {
	return &genai.Part{
		InlineData: &genai.Blob{
			MIMEType: f.MimeType,
			Data:     f.Content,
		},
	}, nil
}

type FuncCall struct {
	ID        string
	Name      string
	Arguments map[string]any
	Origin    any
}

func (FuncCall) isPart() {}

func (f FuncCall) ToGemini() (*genai.Part, error) {
	// If Origin is set, it means this FuncCall came from a Gemini response
	// and we should reuse the original part.
	if f.Origin != nil {
		if part, ok := f.Origin.(*genai.Part); ok {
			return part, nil
		}
	}
	// Otherwise, construct a new FunctionCall part for Gemini
	return &genai.Part{
		FunctionCall: &genai.FunctionCall{
			ID:   f.ID,
			Name: f.Name,
			Args: f.Arguments,
		},
	}, nil
}

type CallResult struct {
	ID      string
	Name    string
	Results map[string]any
}

func (CallResult) isPart() {}

func (c CallResult) ToGemini() (*genai.Part, error) {
	return &genai.Part{
		FunctionResponse: &genai.FunctionResponse{
			ID:       c.ID,
			Name:     c.Name,
			Response: c.Results,
		},
	}, nil
}

type FinishReason string

func (FinishReason) isPart() {}

func (FinishReason) ToGemini() (*genai.Part, error) {
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

func (Usage) ToGemini() (*genai.Part, error) {
	return nil, nil
}

type Error struct {
	Error error
}

func (Error) isPart() {}

func (Error) ToGemini() (*genai.Part, error) {
	return nil, nil
}

func PartFromGemini(part *genai.Part) (Part, error) {
	if part.Text != "" || part.Thought {
		if part.Thought {
			return Thought(part.Text), nil
		} else {
			return Text(part.Text), nil
		}
	}

	if part.FunctionResponse != nil {
		return CallResult{
			ID:      part.FunctionResponse.ID,
			Name:    part.FunctionResponse.Name,
			Results: part.FunctionResponse.Response,
		}, nil
	}

	if part.FunctionCall != nil {
		return FuncCall{
			ID:        part.FunctionCall.ID,
			Name:      part.FunctionCall.Name,
			Arguments: part.FunctionCall.Args,
			Origin:    part,
		}, nil
	}

	if part.InlineData != nil {
		return FileContent{
			Content:  part.InlineData.Data,
			MimeType: part.InlineData.MIMEType,
		}, nil
	}

	if part.FileData != nil {
		return FileURL(part.FileData.FileURI), nil
	}

	if part.ExecutableCode != nil {
		return Text(part.ExecutableCode.Code), nil
	}

	if part.CodeExecutionResult != nil {
		return Text(part.CodeExecutionResult.Output), nil
	}

	// Unknown or metadata-only part, ignore
	return nil, nil
}
