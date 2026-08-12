//go:build !unix

package cmd

import "fmt"

// enterWatchInput is unavailable off Unix; countdown still works without keys.
func enterWatchInput() (restore func(), err error) {
	return nil, fmt.Errorf("interactive watch keys not supported on this platform")
}

func pollWatchKey() (byte, bool) { return 0, false }
