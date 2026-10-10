//go:build unix

package muse

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"
)

// startMuseServe starts `muse serve --no-session-log` in its own process
// group, so stop ends the launcher script's children with it.
func startMuseServe(ctx context.Context) (*serveConn, error) {
	path, err := exec.LookPath("muse")
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(path, "serve", "--no-session-log")
	// The launcher checks for an update in the background now and then. It
	// would do so in this process group and be killed mid-download a moment
	// later, so the check is left to the user's own runs of the CLI.
	cmd.Env = append(os.Environ(), "MUSE_NO_AUTO_UPDATE=1")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	// Anything still holding the pipes after the kill must not hold up Wait.
	cmd.WaitDelay = time.Second
	stderr := &bytes.Buffer{}
	cmd.Stderr = stderr
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	var once sync.Once
	stop := func() {
		once.Do(func() {
			_ = stdin.Close()
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
			_ = cmd.Wait()
		})
	}
	// stderr is read only after Wait: the copy into it has finished then, so
	// a host that ended on its own is quoted in full.
	diagnostics := func() string {
		stop()
		return stderr.String()
	}
	return &serveConn{in: stdin, out: stdout, stop: stop, diagnostics: diagnostics}, nil
}
