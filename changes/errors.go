package changes

// ApplyError is returned by a change block handler when a change block fails
// to apply due to model-generated errors (e.g., invalid target, malformed
// code, goimports failure). Callers may use errors.As to distinguish apply
// errors from other error types for logging or diagnostics. The generation
// loop's retry behavior is governed by RetryOnError, which retries any error
// that occurs after the model has output content, regardless of the specific
// error type. The retry feedback for ApplyError instructs the model to
// re-emit every intended change block, because the retry discards all change
// blocks from the failed attempt. See loops.TheoryOfLoops.
type ApplyError struct {
	Err error
}

func (e *ApplyError) Error() string {
	return e.Err.Error()
}

func (e *ApplyError) Unwrap() error {
	return e.Err
}
