package nets

import (
	"net"
	"net/http"
	"time"
)

const TheoryOfHTTPTransport = `
The HTTP transport of the session is built once, by this package, and every
generator's client shares its dialer. Proxy selection belongs to the dialer,
so the transport installs no Proxy function: a second selection there would
bypass the locality check and the configured proxy address. A generator's
explicit no-proxy override builds a direct transport of its own; that override
predates this constructor and keeps its own settings.

Dialing and TLS are bounded — dial, keepalive, and handshake timeouts — so a
connection that never completes fails and reaches the retry classifier
instead of hanging the run. The response header timeout stays unset: a
reasoning model may think for minutes before its first header arrives, so
bounding that wait would fail a request that is working. The caller's context
remains the way to end a run early.

Idle connections are pooled with a bounded lifetime and a bounded count per
host. A reused connection is then one the server is likely to still hold, and
a pooled connection the server already dropped surfaces as a transport error
the retry path handles, never as a failed run. HTTP/2 is attempted even
though the dialer is custom, so a provider that negotiates it pays one
connection for the whole session.
`

const (
	// dialTimeout bounds one connection attempt, so an unreachable host or
	// a black-holed handshake fails within the run rather than after the
	// operating system gives up.
	dialTimeout = 30 * time.Second

	// dialKeepAlive is the TCP keepalive interval of the dialer: an idle
	// connection is probed often enough that a silently dropped path is
	// detected before a request is written onto it.
	dialKeepAlive = 30 * time.Second

	// tlsHandshakeTimeout bounds the TLS handshake after the TCP connect,
	// the phase where a proxy or a middlebox most often stalls.
	tlsHandshakeTimeout = 15 * time.Second

	// expectContinueTimeout bounds the wait for a 100-continue answer, so a
	// server that ignores the expectation does not hold the request body.
	expectContinueTimeout = time.Second

	// idleConnTimeout bounds how long a pooled connection stays reusable.
	// A connection idle longer is closed by the client before it can be
	// reused against a server that already forgot it.
	idleConnTimeout = 90 * time.Second

	// maxIdleConns and maxIdleConnsPerHost bound the pool, so one session
	// keeps a small set of warm connections instead of holding every
	// connection it ever opened.
	maxIdleConns        = 16
	maxIdleConnsPerHost = 8
)

// newDirectDialer returns the dialer that opens host connections: the bounded
// dialer every non-proxied dial and every proxied forward dial share.
// See TheoryOfHTTPTransport.
func newDirectDialer() net.Dialer {
	return net.Dialer{
		Timeout:   dialTimeout,
		KeepAlive: dialKeepAlive,
	}
}

// newHTTPTransport returns the transport every generator client uses: the
// session's dialer for connection establishment, bounded TLS, continue, and
// idle lifetimes, and an attempted HTTP/2 upgrade. See TheoryOfHTTPTransport.
func newHTTPTransport(dialer Dialer) *http.Transport {
	return &http.Transport{
		DialContext:           dialer.DialContext,
		Proxy:                 nil,
		ForceAttemptHTTP2:     true,
		TLSHandshakeTimeout:   tlsHandshakeTimeout,
		ExpectContinueTimeout: expectContinueTimeout,
		IdleConnTimeout:       idleConnTimeout,
		MaxIdleConns:          maxIdleConns,
		MaxIdleConnsPerHost:   maxIdleConnsPerHost,
	}
}
