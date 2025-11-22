package nets

import (
	"net/http"
)

type HTTPClient = *http.Client

func (Module) HTTPClient(
	dialer Dialer,
) HTTPClient {
	return &http.Client{
		Transport: &http.Transport{
			DialContext: dialer.DialContext,
		},
	}
}
