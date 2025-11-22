package nets

import "net"

type IsLocalAddr func(addr string) (bool, error)

func (Module) IsLocalAddr() IsLocalAddr {
	return func(addr string) (bool, error) {
		host, _, err := net.SplitHostPort(addr)
		if err != nil {
			// If SplitHostPort fails, it might be an address without a port (e.g., "localhost" or "127.0.0.1")
			// or an invalid address. In such cases, treat the entire addr as the host.
			host = addr
		}

		ips, err := net.LookupIP(host)
		if err != nil {
			// If DNS lookup fails, assume it's not a local address to ensure proxy is used for unknown hosts.
			return false, nil
		}

		for _, ip := range ips {
			// Check if the IP is a loopback address (e.g., 127.0.0.1, ::1) or a private network address (RFC 1918, RFC 4193).
			if ip.IsLoopback() || ip.IsPrivate() {
				return true, nil
			}
		}

		return false, nil
	}
}
