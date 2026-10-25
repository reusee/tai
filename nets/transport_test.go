package nets

import (
	"context"
	"net"
	"testing"
)

func TestNewHTTPTransportBoundsConnections(t *testing.T) {
	dialer := DialerFunc(func(ctx context.Context, network, addr string) (net.Conn, error) {
		return nil, nil
	})
	tr := newHTTPTransport(dialer)
	if tr.DialContext == nil {
		t.Fatal("transport dropped the session dialer")
	}
	if tr.Proxy != nil {
		t.Fatal("transport selected a proxy of its own")
	}
	if tr.TLSHandshakeTimeout <= 0 || tr.ExpectContinueTimeout <= 0 || tr.IdleConnTimeout <= 0 {
		t.Fatalf("unbounded transport: tls=%v continue=%v idle=%v",
			tr.TLSHandshakeTimeout, tr.ExpectContinueTimeout, tr.IdleConnTimeout)
	}
	if tr.ResponseHeaderTimeout != 0 {
		t.Fatalf("response header timeout %v would fail a slowly thinking model", tr.ResponseHeaderTimeout)
	}
	if tr.MaxIdleConns <= 0 || tr.MaxIdleConnsPerHost <= 0 {
		t.Fatalf("unbounded pool: total=%d per-host=%d", tr.MaxIdleConns, tr.MaxIdleConnsPerHost)
	}
	if !tr.ForceAttemptHTTP2 {
		t.Fatal("transport does not attempt HTTP/2")
	}

	direct := newDirectDialer()
	if direct.Timeout <= 0 || direct.KeepAlive <= 0 {
		t.Fatalf("unbounded dialer: timeout=%v keepalive=%v", direct.Timeout, direct.KeepAlive)
	}
}
