package generators

type OpenAIError struct {
	Err     error
	Request ChatCompletionRequest
}

var _ error = OpenAIError{}

func (o OpenAIError) Error() string {
	return o.Err.Error()
}
