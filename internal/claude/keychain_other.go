//go:build !darwin || !cgo

package claude

func loadKeychainCredentials() ([]byte, []byte, error) {
	return nil, nil, errKeychainCredentialsNotFound
}

func persistKeychainCredentials([]byte, []byte) error {
	return errKeychainCredentialsNotFound
}

func loadKeychainCredential([]byte) ([]byte, error) {
	return nil, errKeychainCredentialsNotFound
}
