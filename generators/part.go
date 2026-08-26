package generators

import (
	"fmt"
	"time"

	"google.golang.org/genai"
)

const TheoryOfPartInterface = `
The Part interface is a sealed type union for content parts. It uses only a
marker method (isPart) to prevent external implementations. Gemini-specific
conversion is handled by the partToGemini function, which uses a type switch —
mirroring the OpenAI path (stateToOpenAIMessages) that uses type switches.
Metadata types (Thought, FinishReason, Usage, Error) have no Gemini
representation: Thought is skipped by a continue before the conversion call,
and the other three are carried in RoleLog content that is filtered out before
the conversion loop.
`

type Part interface {
	isPart()
}

func partToGemini(part Part) *genai.Part {
	switch p := part.(type) {
	case Text:
		if len(p) == 0 {
			return nil
		}
		return &genai.Part{
			Text: string(p),
		}
	case FileURL:
		return &genai.Part{
			FileData: &genai.FileData{
				FileURI: string(p),
			},
		}
	case FileContent:
		return &genai.Part{
			InlineData: &genai.Blob{
				MIMEType: p.MimeType,
				Data:     p.Content,
			},
		}
	case FuncCall:
		if p.Origin != nil {
			if part, ok := p.Origin.(*genai.Part); ok {
				return part
			}
		}
		return &genai.Part{
			FunctionCall: &genai.FunctionCall{
				ID:   p.ID,
				Name: p.Name,
				Args: p.Arguments,
			},
		}
	case CallResult:
		return &genai.Part{
			FunctionResponse: &genai.FunctionResponse{
				ID:       p.ID,
				Name:     p.Name,
				Response: p.Results,
			},
		}
	}
	return nil
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

const TheoryOfUsageTiming = `
Streaming generators measure generation speed onto the Usage part:
TimeToFirstToken spans from sending the request to the arrival of the
first output content (body text or reasoning thought), and
GenerateDuration spans from that first token to the end of the stream.
Average speed is derived at display time as generated tokens (candidates
plus thoughts) divided by GenerateDuration, rendered with one decimal.
Non-streaming requests leave both durations zero, and every display
site omits the speed section for an unmeasured usage instead of
printing zeros or dividing by zero. Formatting lives in Usage.SpeedSuffix
so all display surfaces render the same fragment.
`

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
	// TimeToFirstToken is the duration from sending the request to the
	// first streamed output token (body text or reasoning thought).
	// Zero when unmeasured, e.g. non-streaming requests.
	// See TheoryOfUsageTiming.
	TimeToFirstToken time.Duration
	// GenerateDuration is the duration from the first output token to
	// the end of the stream. Zero when unmeasured.
	// See TheoryOfUsageTiming.
	GenerateDuration time.Duration
}

// HasSpeed reports whether the usage carries streaming timing
// measurements; display sites consult it before rendering the speed
// section. See TheoryOfUsageTiming.
func (u Usage) HasSpeed() bool {
	return u.TimeToFirstToken > 0 && u.GenerateDuration > 0
}

// GeneratedTokens returns the number of tokens the model generated:
// completion tokens plus reasoning-thought tokens. Both are produced
// during GenerateDuration, so their sum is the numerator of the
// average speed.
func (u Usage) GeneratedTokens() int {
	return u.Candidates.TokenCount + u.Thoughts.TokenCount
}

// SpeedSuffix renders the measured generation speed as a comma-led
// fragment such as ", ttft 1.2s, 45.3 tok/s", empty when the usage
// carries no timing. Display sites append it verbatim to their usage
// lines so the fragment stays identical across outputs.
// See TheoryOfUsageTiming.
func (u Usage) SpeedSuffix() string {
	if !u.HasSpeed() {
		return ""
	}
	return fmt.Sprintf(", ttft %.1fs, %.1f tok/s",
		u.TimeToFirstToken.Seconds(),
		float64(u.GeneratedTokens())/u.GenerateDuration.Seconds(),
	)
}

func (Usage) isPart() {}

type Error struct {
	Error error
}

func (Error) isPart() {}

func PartFromGemini(part *genai.Part) Part {
	if part.Text != "" || part.Thought {
		if part.Thought {
			return Thought(part.Text)
		} else {
			return Text(part.Text)
		}
	}

	if part.FunctionResponse != nil {
		return CallResult{
			ID:      part.FunctionResponse.ID,
			Name:    part.FunctionResponse.Name,
			Results: part.FunctionResponse.Response,
		}
	}

	if part.FunctionCall != nil {
		return FuncCall{
			ID:        part.FunctionCall.ID,
			Name:      part.FunctionCall.Name,
			Arguments: part.FunctionCall.Args,
			Origin:    part,
		}
	}

	if part.InlineData != nil {
		return FileContent{
			Content:  part.InlineData.Data,
			MimeType: part.InlineData.MIMEType,
		}
	}

	if part.FileData != nil {
		return FileURL(part.FileData.FileURI)
	}

	if part.ExecutableCode != nil {
		return Text(part.ExecutableCode.Code)
	}

	if part.CodeExecutionResult != nil {
		return Text(part.CodeExecutionResult.Output)
	}

	// Unknown or metadata-only part, ignore
	return nil
}
