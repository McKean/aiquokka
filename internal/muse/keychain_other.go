//go:build !darwin

package muse

import (
	"context"
	"errors"
)

// errKeychainNotFound marks the absence of a Keychain login. Off macOS the
// Muse CLI keeps the token inline in auth.json, so there is never one.
var errKeychainNotFound = errors.New("no Muse login in Keychain")

func isKeychainNotFound(err error) bool { return errors.Is(err, errKeychainNotFound) }

func loadKeychainToken(ctx context.Context) (string, error) { return "", errKeychainNotFound }
