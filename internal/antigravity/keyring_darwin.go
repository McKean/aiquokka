//go:build darwin

package antigravity

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"syscall"
)

func readKeyringCredential(parent context.Context) ([]byte, error) {
	stdout, stderr, err := runSecurity(parent, "find-generic-password", "-w", "-s", keyringService, "-a", keyringAccount)
	if err != nil {
		return nil, classifySecurityError(err, string(stderr))
	}
	out := decodeKeyringSecret(stdout)
	if len(out) == 0 {
		return nil, errKeyringNotFound
	}
	return out, nil
}

func writeKeyringCredential(parent context.Context, data []byte) error {
	_, stderr, err := runSecurity(parent,
		"add-generic-password",
		"-U",
		"-s", keyringService,
		"-a", keyringAccount,
		"-w", string(data),
	)
	if err != nil {
		return classifySecurityError(err, string(stderr))
	}
	return nil
}

func runSecurity(parent context.Context, args ...string) (stdout, stderr []byte, err error) {
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithTimeout(parent, keyringTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "/usr/bin/security", args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	err = cmd.Run()
	if errors.Is(ctx.Err(), context.Canceled) {
		return outBuf.Bytes(), errBuf.Bytes(), context.Canceled
	}
	if ctx.Err() != nil {
		return outBuf.Bytes(), errBuf.Bytes(), errKeyringTimeout
	}
	return outBuf.Bytes(), errBuf.Bytes(), err
}

func classifySecurityError(err error, stderr string) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, errKeyringTimeout) {
		return err
	}
	if ee, ok := err.(*exec.ExitError); ok && ee.ExitCode() == 44 {
		return errKeyringNotFound
	}
	msg := strings.TrimSpace(stderr)
	if strings.Contains(msg, "could not be found") {
		return errKeyringNotFound
	}
	if strings.Contains(msg, "User interaction is not allowed") ||
		strings.Contains(msg, "because it is locked") ||
		strings.Contains(msg, "Unable to display a dialogue") {
		return fmt.Errorf("%w (run `security unlock-keychain` or sign in via `agy`)", errKeyringLocked)
	}
	if msg == "" {
		return err
	}
	return fmt.Errorf("%s", msg)
}
