package nets

import (
	"net"
	"sync"
)

const TheoryOfLocalAddrCache = `
Local address determination resolves the host of every dialed address via
net.LookupIP to decide whether the connection should bypass the proxy.
Long-running sessions (interactive ai chat, goal loops) repeatedly dial the
same API hosts, and every new connection would otherwise perform a fresh
DNS lookup. Successful lookups are cached per host so repeated dials reuse
the cached locality determination; failed lookups are not cached so a
transient DNS failure is retried on the next call instead of being sticky.
The cache holds one entry per distinct host the process dials, which is
tiny in practice (a few API providers plus local addresses).
`

type IsLocalAddr func(addr string) (bool, error)

func (Module) IsLocalAddr() IsLocalAddr {
	// Per-host locality determinations are cached so repeated dials to the
	// same host reuse the cached result instead of resolving the host on
	// every new connection. Successful lookups are cached; failures are
	// not, so a transient DNS failure is retried on the next call. A host's
	// locality is stable for the lifetime of a session, so the cache never
	// returns a stale result in practice. See TheoryOfLocalAddrCache.
	cache := newLocalAddrCache(net.LookupIP)
	return cache.isLocalAddr
}

// localAddrCache caches per-host locality determinations so repeated dials
// to the same host reuse the cached result. The DNS lookup function is
// injectable for testing. See TheoryOfLocalAddrCache.
type localAddrCache struct {
	lookupIP func(string) ([]net.IP, error)
	cache    sync.Map // host -> bool
}

// newLocalAddrCache creates a locality cache using the given DNS lookup
// function. Production uses net.LookupIP; tests inject a fake lookup to
// verify caching behavior without network access.
func newLocalAddrCache(lookupIP func(string) ([]net.IP, error)) *localAddrCache {
	return &localAddrCache{lookupIP: lookupIP}
}

// isLocalAddr reports whether the host of addr resolves to a local address
// (loopback or private network). Successful lookups are cached per host;
// failed lookups are not cached, so a transient DNS failure is retried on
// the next call instead of remaining sticky. A host whose lookup fails is
// reported as not local so the proxy is used for unknown hosts, matching
// the original behavior.
func (c *localAddrCache) isLocalAddr(addr string) (bool, error) {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		// If SplitHostPort fails, it might be an address without a port (e.g., "localhost" or "127.0.0.1")
		// or an invalid address. In such cases, treat the entire addr as the host.
		host = addr
	}

	if local, ok := c.cache.Load(host); ok {
		return local.(bool), nil
	}

	ips, err := c.lookupIP(host)
	if err != nil {
		// If DNS lookup fails, assume it's not a local address to ensure proxy is used for unknown hosts.
		return false, nil
	}

	for _, ip := range ips {
		// Check if the IP is a loopback address (e.g., 127.0.0.1, ::1) or a private network address (RFC 1918, RFC 4193).
		if ip.IsLoopback() || ip.IsPrivate() {
			c.cache.Store(host, true)
			return true, nil
		}
	}

	c.cache.Store(host, false)
	return false, nil
}
