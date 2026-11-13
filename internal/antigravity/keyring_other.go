//go:build !darwin && !linux

package antigravity

import "context"

func readKeyringCredential(context.Context) ([]byte, error) {
	return nil, errKeyringNotFound
}

func writeKeyringCredential(context.Context, []byte) error {
	return errKeyringNotFound
}
