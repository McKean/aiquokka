package usage

import (
	"context"
	"errors"
	"fmt"
)

// ErrNotConfigured marks an error as "this provider isn't set up for this user"
// (no credential file, no API key). The aggregate `aiquokka` view skips these
// providers silently, while single-provider commands still surface them so the
// user learns why.
var ErrNotConfigured = errors.New("provider not configured")

// NotConfigured builds an error that reads as msg but matches ErrNotConfigured
// under errors.Is, so its user-facing text stays clean (no sentinel suffix).
//
//	return nil, usage.NotConfigured("no Claude credentials — run `claude`")
func NotConfigured(format string, args ...any) error {
	return &notConfiguredError{msg: fmt.Sprintf(format, args...)}
}

type notConfiguredError struct{ msg string }

func (e *notConfiguredError) Error() string { return e.msg }
func (e *notConfiguredError) Unwrap() error { return ErrNotConfigured }

// IsNotConfigured reports whether err indicates an unconfigured provider.
func IsNotConfigured(err error) bool {
	return errors.Is(err, ErrNotConfigured)
}

// ErrNoRefresh marks a fetch that stopped because the stored token has expired
// (or was rejected) and the caller forbade refreshing it.
var ErrNoRefresh = errors.New("token not refreshed")

type noRefreshKey struct{}

// WithoutRefresh returns a context under which no provider renews a token or
// writes to a stored login. A program that polls aiquokka beside the agents'
// own CLIs uses it so that it is never a second writer of their credentials.
func WithoutRefresh(ctx context.Context) context.Context {
	return context.WithValue(ctx, noRefreshKey{}, true)
}

// RefreshAllowed reports whether ctx lets a provider renew its token.
func RefreshAllowed(ctx context.Context) bool {
	off, _ := ctx.Value(noRefreshKey{}).(bool)
	return !off
}

// NoRefresh builds the error a provider returns instead of refreshing: it
// names the provider and the CLI that renews the login, and matches
// ErrNoRefresh under errors.Is.
//
//	return usage.NoRefresh("Claude", "claude")
func NoRefresh(provider, cli string) error {
	return &noRefreshError{msg: fmt.Sprintf("%s token has expired or was rejected and --no-refresh is set — run `%s` to renew it", provider, cli)}
}

type noRefreshError struct{ msg string }

func (e *noRefreshError) Error() string { return e.msg }
func (e *noRefreshError) Unwrap() error { return ErrNoRefresh }
