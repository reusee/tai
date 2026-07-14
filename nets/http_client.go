package nets

import (
	"net/http"
)

type HTTPClient struct {
	*http.Client
}

func (Module) HTTPClient(
	dialer Dialer,
) HTTPClient {
	return HTTPClient{
		Client: &http.Client{
			Transport: &http.Transport{
				DialContext: dialer.DialContext,
			},
		},
	}
}
