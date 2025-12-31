package nets

import (
	"net"
	"net/url"
	"os"
	"sync"

	"github.com/reusee/tai/configs"
	"github.com/reusee/tai/logs"
	"github.com/reusee/tai/modes"
	"github.com/reusee/tai/vars"
	"golang.org/x/net/proxy"
)

type ProxyAddr string

func (p ProxyAddr) ConfigExpr() string {
	return "ProxyAddr"
}

var _ configs.Configurable = ProxyAddr("")

func (Module) ProxyAddr(
	mode modes.Mode,
	loader configs.Loader,
	logger logs.Logger,
) (ret ProxyAddr) {
	defer func() {
		logger.Info("proxy", "addr", ret)
	}()

	if mode == modes.ModeDevelopment {
		return ""
	}

	return vars.FirstNonZero(
		configs.First[ProxyAddr](loader, "proxy_addr"),
		configs.First[ProxyAddr](loader, "proxy_address"),
		configs.First[ProxyAddr](loader, "http_proxy"),
		configs.First[ProxyAddr](loader, "socks_proxy"),
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
