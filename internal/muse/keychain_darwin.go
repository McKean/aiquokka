//go:build darwin

package muse

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

// The Muse CLI stores its device-code login in the Keychain under this
// service/account when it is not inline in auth.json.
const (
	keychainService = "ai.meta.dev.credentials"
	keychainAccount = "meta"
)

// securityTimeout bounds the /usr/bin/security call so a Keychain permission
// dialog (unconfirmable over SSH) cannot hang the fetch.
const securityTimeout = 3 * time.Second

// errKeychainNotFound marks every "no usable Keychain login" outcome: a
// missing item, an unreadable one, or a payload without an access token. The
// caller then reports NotConfigured instead of failing.
var errKeychainNotFound = errors.New("no Muse login in Keychain")

func isKeychainNotFound(err error) bool { return errors.Is(err, errKeychainNotFound) }

// loadKeychainToken reads the CLI's Keychain item and returns its OAuth access
// token. It never prompts: a locked Keychain or a permission dialog reads as
// "not found" so the fetch stays non-interactive.
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
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return "", fmt.Errorf("%w: timed out (a permission dialog may be waiting on the Mac desktop)", errKeychainNotFound)
		}
		msg := strings.TrimSpace(stderr.String())
		if ee, ok := err.(*exec.ExitError); ok && ee.ExitCode() == 44 || strings.Contains(msg, "could not be found") {
			return "", errKeychainNotFound
		}
		if strings.Contains(msg, "User interaction is not allowed") ||
			strings.Contains(msg, "because it is locked") ||
			strings.Contains(msg, "Unable to display a dialogue") {
			return "", fmt.Errorf("%w: Keychain is locked or needs approval — run `security unlock-keychain`, then retry", errKeychainNotFound)
		}
		if msg == "" {
			return "", fmt.Errorf("%w: %v", errKeychainNotFound, err)
		}
		return "", fmt.Errorf("%w: %s", errKeychainNotFound, msg)
	}
	return keychainAccessToken(bytes.TrimSpace(stdout.Bytes()))
}
