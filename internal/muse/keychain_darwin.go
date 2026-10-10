//go:build darwin

package muse

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

// securityTimeout bounds the /usr/bin/security call so a Keychain permission
// dialog cannot hang the fetch: over SSH nobody can answer one. The bound has
// a cost at the Mac's screen too. The dialog outlives the killed read, and an
// answer given to it grants nothing, since the process that asked is gone. So
// the unreadable-item message in creds.go names commands the user runs with
// no time limit instead of pointing at that dialog.
const securityTimeout = 3 * time.Second

// errKeychainNotFound marks a Keychain item that is not there, or a payload
// without an access token. An item that may be there but could not be read
// (a permission dialog, a locked Keychain) is a *keychainUnreadableError,
// which unwraps to this so "is there a login?" callers keep seeing absence.
var errKeychainNotFound = errors.New("no Muse login in Keychain")

func isKeychainNotFound(err error) bool { return errors.Is(err, errKeychainNotFound) }

// loadKeychainToken reads the CLI's Keychain item and returns its OAuth access
// token. It never prompts: the read is bounded, and a failure classifies as
// missing or unreadable (see classifyKeychainFailure) so the fetch stays
// non-interactive.
func loadKeychainToken(ctx context.Context) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, securityTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/usr/bin/security",
		"find-generic-password", "-s", keychainService, "-a", keychainAccount, "-w")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		timedOut := errors.Is(ctx.Err(), context.DeadlineExceeded)
		var exitErr *exec.ExitError
		code, hasCode := 0, false
		if errors.As(err, &exitErr) {
			code, hasCode = exitErr.ExitCode(), true
		}
		return "", classifyKeychainFailure(timedOut, code, hasCode, strings.TrimSpace(stderr.String()), err)
	}
	return keychainAccessToken(bytes.TrimSpace(stdout.Bytes()))
}
