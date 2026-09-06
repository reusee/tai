package generators

import (
	"errors"
	"strings"
)

var ErrRetryable = errors.New("retryable error")

const TheoryOfContextExceeded = `
context-exceeded classification theory:
- Recognition is a phrase classifier over the rendered error text, not a
  status-code or provider-type match: providers report an over-limit
  request in many shapes — OpenAI compatibles as a maximum context
  length, Anthropic as a too-long prompt, Gemini as an input token
  count, some providers as a too-large request payload, and Chinese
  providers in Chinese — so no single code or type covers them all.
- IsContextExceeded is the one predicate; pipeline flow control calls it
  on the error the generator returned, so classification stays a read
  over the error text. The rendered text carries only the provider
  message — the OpenAI request dump is a struct field, never part of
  the text, and error wrapping preserves the text — so a phrase match
  is the provider's own report, not echoed model output.
- A context-exceeded error is not retryable: the same messages exceed
  the window again, and a retried generation appends handoff feedback
  that grows the input further. The pipeline ends the loop and hands
  the interrupted output to the next loop, whose fresh scope rebuilds
  a smaller context.
`

// contextExceededPhrases lists the message fragments by which model
// providers report that a request exceeds the model's context window.
// Fragments are lowercase; matching is case-insensitive on the error
// text. See TheoryOfContextExceeded.
var contextExceededPhrases = []string{
	"maximum context length",
	"context length exceeded",
	"context window",
	"prompt is too long",
	"input token count",
	"payload size exceeds",
	"request too large",
	"too many tokens",
	"上下文长度超限",
}

// IsContextExceeded reports whether an API error reports that the
// request exceeded the model's context window, by matching the error
// text against the provider phrases. See TheoryOfContextExceeded.
func IsContextExceeded(err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(err.Error())
	for _, phrase := range contextExceededPhrases {
		if strings.Contains(text, phrase) {
			return true
		}
	}
	return false
}
