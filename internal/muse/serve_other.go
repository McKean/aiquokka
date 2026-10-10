//go:build !unix

package muse

import (
	"context"
	"errors"
)

// startMuseServe has no process-group handling off Unix, where the Muse CLI
// keeps the token inline in auth.json and the HTTP path serves.
func startMuseServe(ctx context.Context) (*serveConn, error) {
	return nil, errors.New("not supported on this platform")
}
