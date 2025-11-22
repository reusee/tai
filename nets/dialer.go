package nets

import (
	"context"
	"net"
)

type Dialer interface {
	Dial(network, addr string) (net.Conn, error)
	DialContext(ctx context.Context, network, addr string) (net.Conn, error)
}

func (Module) Dialer(
	getProxyDialer GetProxyDialer,
	isLocalAddr IsLocalAddr,
) Dialer {
	var direct net.Dialer
	return DialerFunc(func(ctx context.Context, network, addr string) (ret net.Conn, err error) {
		if isLocal, err := isLocalAddr(addr); err != nil {
			return nil, err
		} else if isLocal {
			return direct.DialContext(ctx, network, addr)
		}
		proxyDialer, err := getProxyDialer()
		if err != nil {
			return nil, err
		}
		return proxyDialer.DialContext(ctx, network, addr)
	})
}

type DialerFunc func(context.Context, string, string) (net.Conn, error)

var _ Dialer = DialerFunc(nil)

func (d DialerFunc) DialContext(ctx context.Context, network string, addr string) (net.Conn, error) {
	return d(ctx, network, addr)
}

func (d DialerFunc) Dial(network string, addr string) (net.Conn, error) {
	return d(context.Background(), network, addr)
}
