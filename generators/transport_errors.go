package generators

import (
	"context"
	"errors"
	"io"
	"net"
	"syscall"
)

const TheoryOfTransientTransportErrors = `
A transport fault is transient by nature: a closed connection, a reset, a
broken stream, or an unreachable host says the network dropped the request,
not that the request is invalid. The same request sent again can succeed, so
the retry machinery must treat such a fault as retryable. Otherwise one
dropped connection aborts an unattended run that the next attempt would have
continued.

Recognition reads the error chain, so a provider wrapper must expose its
transport error: OpenAIError.Unwrap is what lets the classifier see the
network fault through the provider's own error value. The classifier matches
the identity of the net and syscall errors, never their text, because a
message is a provider's wording while the identity is the operating system's
report. A socket-level timeout reaches the classifier as a net.Error, which
is why the deadline identities are absent from the list: a deadline the
caller set belongs to the caller.

A caller's own cancellation or deadline is not a transport fault: the
decision belongs to the caller, and retrying would repeat it against that
decision. A prematurely ended stream does count as transport — a generator
treats a normal end through its own protocol, so an error carrying io.EOF
means the response stopped mid-transfer.
`

// isTransientTransportError reports whether err reports a transport fault that
// a retry can plausibly repair. See TheoryOfTransientTransportErrors.
func isTransientTransportError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	switch {
	case errors.Is(err, net.ErrClosed),
		errors.Is(err, io.EOF),
		errors.Is(err, io.ErrUnexpectedEOF),
		errors.Is(err, syscall.ECONNRESET),
		errors.Is(err, syscall.ECONNABORTED),
		errors.Is(err, syscall.EPIPE),
		errors.Is(err, syscall.ETIMEDOUT),
		errors.Is(err, syscall.ENETUNREACH),
		errors.Is(err, syscall.EHOSTUNREACH):
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	return false
}
