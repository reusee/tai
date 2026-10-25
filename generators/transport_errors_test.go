package generators

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"syscall"
	"testing"
)

func TestIsRetryableTransientTransportError(t *testing.T) {
	closed := OpenAIError{Err: fmt.Errorf("post: %w", net.ErrClosed)}
	if !isRetryable(closed) {
		t.Fatalf("a closed connection must be retryable: %v", closed)
	}

	reset := OpenAIError{Err: &net.OpError{
		Op:  "write",
		Net: "tcp",
		Err: &osSyscallError{syscall.ECONNRESET},
	}}
	if !isRetryable(reset) {
		t.Fatalf("a reset connection must be retryable: %v", reset)
	}

	interrupted := OpenAIError{Err: fmt.Errorf("stream: %w", io.ErrUnexpectedEOF)}
	if !isRetryable(interrupted) {
		t.Fatalf("a broken stream must be retryable: %v", interrupted)
	}

	cancelled := OpenAIError{Err: fmt.Errorf("post: %w", context.Canceled)}
	if isRetryable(cancelled) {
		t.Fatalf("a cancelled request must not be retried: %v", cancelled)
	}

	invalid := OpenAIError{Err: errors.New("invalid api key")}
	if isRetryable(invalid) {
		t.Fatalf("a configuration fault must not be retried: %v", invalid)
	}
}

// osSyscallError adapts a syscall errno to the error interface, so the test
// builds a net.OpError whose cause is a real errno identity.
type osSyscallError struct {
	errno syscall.Errno
}

func (e *osSyscallError) Error() string { return e.errno.Error() }

func (e *osSyscallError) Unwrap() error { return e.errno }
