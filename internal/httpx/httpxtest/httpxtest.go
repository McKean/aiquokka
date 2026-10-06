// Package httpxtest lets a provider's test answer the shared HTTP client's
// requests itself, so that no test reaches a real endpoint.
package httpxtest

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/McKean/aiquokka/internal/httpx"
)

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// Stub routes every request of httpx.Client to answer, which returns the
// status and body to reply with, until the test ends.
func Stub(t *testing.T, answer func(r *http.Request) (status int, body string)) {
	t.Helper()
	original := httpx.Client.Transport
	t.Cleanup(func() { httpx.Client.Transport = original })
	httpx.Client.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
		status, body := answer(r)
		return &http.Response{
			StatusCode: status,
			Status:     http.StatusText(status),
			Header:     http.Header{"Content-Type": {"application/json"}},
			Body:       io.NopCloser(strings.NewReader(body)),
			Request:    r,
		}, nil
	})
}

// Refuse fails the test on any request: for code that must stay offline.
func Refuse(t *testing.T) {
	t.Helper()
	Stub(t, func(r *http.Request) (int, string) {
		t.Errorf("unexpected request: %s %s", r.Method, r.URL)
		return http.StatusInternalServerError, ""
	})
}
