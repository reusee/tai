package generators

type OpenAIError struct {
	Err     error
	Request ChatCompletionRequest
}

var _ error = OpenAIError{}

func (o OpenAIError) Error() string {
	return o.Err.Error()
}

// Unwrap exposes the transport error, so the retry classifier reaches the
// underlying network fault through the provider's wrapper. See
// TheoryOfTransientTransportErrors.
func (o OpenAIError) Unwrap() error {
	return o.Err
}
