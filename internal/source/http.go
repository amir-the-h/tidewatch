package source

import (
	"net/http"
	"time"
)

// UserAgent is sent by the shared HTTP client unless a request sets its own.
const UserAgent = "Tidewatch (+https://github.com/amir-the-h/tidewatch)"

// NewHTTPClient returns the client connectors get from Runtime.HTTPClient:
// a 60 s overall timeout and the Tidewatch User-Agent.
func NewHTTPClient() *http.Client {
	return &http.Client{
		Timeout:   60 * time.Second,
		Transport: userAgentTransport{base: http.DefaultTransport},
	}
}

type userAgentTransport struct{ base http.RoundTripper }

func (t userAgentTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Header.Get("User-Agent") == "" {
		req = req.Clone(req.Context())
		req.Header.Set("User-Agent", UserAgent)
	}
	return t.base.RoundTrip(req)
}
