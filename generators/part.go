package generators

import (
	"google.golang.org/genai"
)

const TheoryOfPartInterface = `
The Part interface is a sealed type union for content parts. It uses only a
marker method (isPart) to prevent external implementations, rather than
exposing backend-specific conversion methods. Gemini-specific conversion is
handled by the partToGemini function, which uses a type switch — mirroring the
OpenAI path (stateToOpenAIMessages) that uses type switches rather than
interface methods. Metadata types (Thought, FinishReason, Usage, Error) have
no Gemini representation: Thought is skipped by a continue before the
conversion call, and the other three are carried in RoleLog content that is
filtered out before the conversion loop.
`

type Part interface {
	isPart()
}

// partToGemini converts a Part to a *genai.Part for the Gemini API.
// Returns nil for Part types that have no Gemini representation
// (Thought, FinishReason, Usage, Error). Thought is handled separately
// by the caller (PreservedThinking), and the others are internal
// metadata types carried in RoleLog content that is filtered out before
// reaching this function. See TheoryOfPartInterface.
func partToGemini(part Part) (*genai.Part, error) {
	switch p := part.(type) {
	case Text:
		if len(p) == 0 {
			return nil, nil
		}
		return &genai.Part{
			Text: string(p),
		}, nil
	case FileURL:
		return &genai.Part{
			FileData: &genai.FileData{
				FileURI: string(p),
			},
		}, nil
	case FileContent:
		return &genai.Part{
			InlineData: &genai.Blob{
				MIMEType: p.MimeType,
				Data:     p.Content,
			},
		}, nil
	case FuncCall:
		if p.Origin != nil {
			if part, ok := p.Origin.(*genai.Part); ok {
				return part, nil
			}
		}
		return &genai.Part{
			FunctionCall: &genai.FunctionCall{
				ID:   p.ID,
				Name: p.Name,
				Args: p.Arguments,
			},
		}, nil
	case CallResult:
		return &genai.Part{
			FunctionResponse: &genai.FunctionResponse{
				ID:       p.ID,
				Name:     p.Name,
				Response: p.Results,
			},
		}, nil
	}
	return nil, nil
}

type Text string

func (Text) isPart() {}

type Thought string

func (Thought) isPart() {}

type FileURL string

func (FileURL) isPart() {}

type FileContent struct {
	Content  []byte
	MimeType string
}

func (FileContent) isPart() {}

type FuncCall struct {
	ID        string
	Name      string
	Arguments map[string]any
	Origin    any

	// Handled is set to true by a FuncMap layer after the function has been
	// executed, so that inner layers skip it and avoid double execution.
	Handled bool
}

func (FuncCall) isPart() {}

type CallResult struct {
	ID      string
	Name    string
	Results map[string]any
}

func (CallResult) isPart() {}

type FinishReason string

func (FinishReason) isPart() {}

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

type Error struct {
	Error error
}

func (Error) isPart() {}

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
