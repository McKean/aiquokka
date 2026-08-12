//go:build unix

package cmd

import (
	"os"

	"golang.org/x/sys/unix"
	"golang.org/x/term"
)

// enterWatchInput switches stdin to raw + non-blocking so we can poll keys
// without stalling the countdown ticker. restore returns the terminal to its
// previous state.
func enterWatchInput() (restore func(), err error) {
	fd := int(os.Stdin.Fd())
	old, err := term.MakeRaw(fd)
	if err != nil {
		return nil, err
	}
	if err := unix.SetNonblock(fd, true); err != nil {
		_ = term.Restore(fd, old)
		return nil, err
	}
	return func() {
		_ = unix.SetNonblock(fd, false)
		_ = term.Restore(fd, old)
	}, nil
}

// pollWatchKey returns one pending key if available.
func pollWatchKey() (byte, bool) {
	var buf [1]byte
	n, err := unix.Read(int(os.Stdin.Fd()), buf[:])
	if n == 1 {
		return buf[0], true
	}
	_ = err // EAGAIN / EWOULDBLOCK when nothing is pending
	return 0, false
}
