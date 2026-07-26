package nets

import (
	"net"
	"net/url"
	"os"
	"sync"

	"cuelang.org/go/cue"
	"github.com/reusee/tai/configs"
	"github.com/reusee/tai/flags"
	"github.com/reusee/tai/modes"
	"github.com/reusee/tai/vars"
	"golang.org/x/net/proxy"
)

type ProxyAddr string

var _ flags.Flag = ProxyAddr("")

func (p ProxyAddr) Handle(key string, args []string) (newDef any, remainArgs []string, err error) {
	ret := ProxyAddr(args[0])
	return &ret, args[1:], nil
}

func (p ProxyAddr) Keys() map[string]string {
	return map[string]string{
		"-proxy": "set proxy address",
	}
}

var _ configs.Config = ProxyAddr("")

func (p ProxyAddr) ConfigPaths() []string {
	return []string{
		"proxy",
		"proxy_addr",
		"proxy_address",
		"http_proxy",
		"socks_proxy",
	}
}

func (p ProxyAddr) HandleConfig(path string, values []*cue.Value) (any, error) {
	var ret ProxyAddr
	if err := values[0].Decode(&ret); err != nil {
		return nil, err
	}
	return &ret, nil
}

func (Module) ProxyAddr(
	mode modes.Mode,
) (ret ProxyAddr) {
	if mode == modes.ModeDevelopment {
		return ""
	}

	return vars.FirstNonZero(
		ProxyAddr(os.Getenv("ALL_PROXY")),
		ProxyAddr(os.Getenv("all_proxy")),
		ProxyAddr(os.Getenv("HTTP_PROXY")),
		ProxyAddr(os.Getenv("http_proxy")),
		ProxyAddr(os.Getenv("SOCKS_PROXY")),
		ProxyAddr(os.Getenv("socks_proxy")),
	)
}

type GetProxyURL func() (*url.URL, error)

func (Module) GetProxyURL(
	proxyAddr ProxyAddr,
) GetProxyURL {
	return sync.OnceValues(func() (*url.URL, error) {
		if proxyAddr == "" {
			return nil, nil
		}
		u, err := url.Parse(string(proxyAddr))
		if err != nil {
			return nil, err
		}
		if u.Scheme == "socks" {
			u.Scheme = "socks5"
		}
		return u, nil
	})
}

type GetProxyDialer func() (Dialer, error)

func (Module) GetProxyDialer(
	getURL GetProxyURL,
) GetProxyDialer {
	direct := any(&net.Dialer{}).(Dialer)
	return sync.OnceValues(func() (Dialer, error) {
		u, err := getURL()
		if err != nil {
			return nil, err
		}
		if u != nil {
			var proxyDialer proxy.Dialer
			proxyDialer, err = proxy.FromURL(u, direct)
			if err != nil {
				return nil, err
			}
			return proxyDialer.(Dialer), nil
		}
		return direct, nil
	})
}
