//go:build unix

package muse

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// fakeMuse puts a stand-in `muse` first on PATH: a shell script, as the real
// launcher is.
func fakeMuse(t *testing.T, script string) string {
	t.Helper()
	t.Setenv("AIQUOKKA_MUSE_CLI_TIMEOUT", defaultServeTimeout.String())
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "muse"), []byte("#!/bin/sh\n"+script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+"/usr/bin:/bin")
	t.Setenv("MUSE_AUTH_PATH", filepath.Join(dir, "auth.json"))
	return dir
}

// The real start against a host that answers only what it is asked, in order:
// it proves the arguments, the handshake and the line framing together.
func TestStartMuseServeExchange(t *testing.T) {
	dir := fakeMuse(t, `echo "$@" >"$(dirname "$0")/args"
echo "$MUSE_NO_AUTO_UPDATE" >"$(dirname "$0")/noupdate"
while IFS= read -r line; do
  case "$line" in
    *'"method":"initialize"'*) initialize=1; echo '{"jsonrpc":"2.0","id":1,"result":{}}' ;;
    *'"method":"initialized"'*) [ -n "$initialize" ] && initialized=1 ;;
    *'"method":"usage/read"'*)
      if [ -n "$initialized" ]; then
        echo '{"jsonrpc":"2.0","method":"usage/changed","params":{}}'
        echo '{"jsonrpc":"2.0","id":2,"result":{"usage":{"observedAtMs":1,"tier":"t","window":{"resetsAtMs":1789568145000,"usedPercent":7,"windowDurationMins":300},"weekly":{"resetsAtMs":1789948800000,"usedPercent":9}}}}'
      else
        echo '{"jsonrpc":"2.0","id":2,"error":{"code":-32600,"message":"Not initialized","data":{"kind":"notInitialized"}}}'
      fi ;;
  esac
done
`)
	report, err := Fetch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Windows) != 2 || *report.Windows[0].UsedPercent != 7 || *report.Windows[1].UsedPercent != 9 {
		t.Fatalf("report = %+v, want both windows", report)
	}
	if args, _ := os.ReadFile(filepath.Join(dir, "args")); strings.TrimSpace(string(args)) != "serve --no-session-log" {
		t.Errorf("muse was started with %q, want serve --no-session-log", args)
	}
	if v, _ := os.ReadFile(filepath.Join(dir, "noupdate")); strings.TrimSpace(string(v)) != "1" {
		t.Errorf("MUSE_NO_AUTO_UPDATE = %q, want 1", v)
	}
}

// A host that never answers fails the fetch at the deadline.
func TestStartMuseServeTimeout(t *testing.T) {
	fakeMuse(t, `sleep 60`)
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := Fetch(ctx)
	if err == nil || !strings.Contains(err.Error(), "did not answer") {
		t.Fatalf("err = %v, want a timeout error", err)
	}
	if took := time.Since(start); took > 3*time.Second {
		t.Fatalf("Fetch took %s, want it to stop at the deadline", took)
	}
}

// A host that never answers is killed when the fetch ends, with the children
// it started: the launcher is a script, so the host itself may be one of them.
// The fetch is cancelled only after the host's marker proves it started, so
// the kill never races startup the way a fixed deadline would under load.
func TestStartMuseServeCancelKillsGroup(t *testing.T) {
	// The marker moves into place, so the test below reads all of it or none.
	dir := fakeMuse(t, `d="$(dirname "$0")"
sleep 60 &
echo $! >"$d/child.tmp"
mv "$d/child.tmp" "$d/child"
wait
`)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := Fetch(ctx)
		done <- err
	}()
	// The host may wait a while to be scheduled under load; the kill below
	// waits for it instead of racing it.
	var pid int
	for deadline := time.Now().Add(10 * time.Second); ; time.Sleep(10 * time.Millisecond) {
		raw, err := os.ReadFile(filepath.Join(dir, "child"))
		if err == nil {
			pid, err = strconv.Atoi(strings.TrimSpace(string(raw)))
			if err != nil {
				t.Fatal(err)
			}
			break
		}
		if !os.IsNotExist(err) {
			t.Fatal(err)
		}
		if time.Now().After(deadline) {
			t.Fatal("the host never wrote its child marker")
		}
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v, want context.Canceled", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Fetch still waits for a host that was cancelled")
	}
	// The kill is sent before Fetch returns; the child may take a moment to go.
	for deadline := time.Now().Add(2 * time.Second); ; time.Sleep(10 * time.Millisecond) {
		if syscall.Kill(pid, 0) == syscall.ESRCH {
			break
		}
		if time.Now().After(deadline) {
			_ = syscall.Kill(pid, syscall.SIGKILL)
			t.Fatalf("child %d of the host outlived the cancel", pid)
		}
	}
}

func TestStartMuseServeEndsEarly(t *testing.T) {
	fakeMuse(t, `echo "error: installed binary is missing; rerun the installer" >&2
exit 1
`)
	_, err := Fetch(context.Background())
	if err == nil || !strings.Contains(err.Error(), "installed binary is missing") {
		t.Fatalf("err = %v, want the host's stderr", err)
	}
}
