package usage

import (
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
