//go:build !darwin

package claude

import "context"

func loadKeychainCredentials(context.Context) ([]byte, []byte, error) {
	return nil, nil, errKeychainCredentialsNotFound
}

func persistKeychainCredentials(context.Context, []byte, []byte) error {
	return errKeychainCredentialsNotFound
}

func loadKeychainCredential(context.Context, []byte) ([]byte, error) {
	return nil, errKeychainCredentialsNotFound
}
