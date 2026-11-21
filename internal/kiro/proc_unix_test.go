//go:build unix

package kiro

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/McKean/aiquokka/internal/usage"
)

func TestRunCommandKillsChildrenOnTimeout(t *testing.T) {
	const marker = "37.4242"
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := runCommand(ctx, "sh", "-c", "sleep "+marker+" & sleep "+marker)
	if err == nil {
		t.Fatal("expected the timed-out command to fail")
	}
	if elapsed := time.Since(start); elapsed > commandWaitDelay+time.Second {
		t.Fatalf("runCommand waited %s for a child that holds the output pipe", elapsed)
	}

	time.Sleep(200 * time.Millisecond)
	out, _ := exec.Command("pgrep", "-f", "sleep "+marker).Output()
	if left := strings.TrimSpace(string(out)); left != "" {
		_ = exec.Command("pkill", "-f", "sleep "+marker).Run()
		t.Fatalf("child processes left running after timeout: %s", left)
	}
}

func TestRunCommandDetachesControllingTTYAndBlocksBrowser(t *testing.T) {
	out, err := runCommand(context.Background(), "sh", "-c",
		`printf '%s|%s|' "$Q_FAKE_IS_REMOTE" "$BROWSER"; if (: < /dev/tty) 2>/dev/null; then printf 'tty'; else printf 'no-tty'; fi`)
	if err != nil {
		t.Fatalf("runCommand failed: %v (%s)", err, out)
	}
	if got := string(out); got != "1|false|no-tty" {
		t.Fatalf("got %q, want %q", got, "1|false|no-tty")
	}
}

func writeFakeKiro(t *testing.T, dir, name, script string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"+script), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestFetchSkipsChatWhenLoggedOut(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "chat-called")
	writeFakeKiro(t, dir, "kiro-cli", `
if [ "$1" = "whoami" ]; then
  echo "Not logged in"
  exit 1
fi
touch "`+marker+`"
echo "Welcome to Kiro CLI, let's get you signed in!"
exit 1
`)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("KIRO_API_KEY", "")

	_, err := Fetch(context.Background())
	if !usage.IsNotConfigured(err) {
		t.Fatalf("err = %v, want NotConfigured", err)
	}
	if _, statErr := os.Stat(marker); statErr == nil {
		t.Fatal("kiro-cli chat was invoked even though whoami reported not logged in")
	}
}

func TestFetchRunsUsageWhenLoggedIn(t *testing.T) {
	dir := t.TempDir()
	writeFakeKiro(t, dir, "kiro-cli", `
if [ "$1" = "whoami" ]; then
  echo "Logged in with Builder ID"
  exit 0
fi
if [ "$Q_FAKE_IS_REMOTE" != "1" ] || [ "$BROWSER" != "false" ]; then
  echo "missing browser-disable env"
  exit 1
fi
echo "Estimated Usage | resets on 09/01 | KIRO PRO"
echo "Credits (2.5 of 100 covered in plan)"
`)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("KIRO_API_KEY", "")

	report, err := Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch failed: %v", err)
	}
	if report.Plan != "KIRO PRO" || len(report.Windows) != 1 {
		t.Fatalf("unexpected report: %#v", report)
	}
}

func TestFetchSkipsWhoamiWhenAPIKeySet(t *testing.T) {
	dir := t.TempDir()
	writeFakeKiro(t, dir, "kiro-cli", `
if [ "$1" = "whoami" ]; then
  echo "Not logged in"
  exit 1
fi
echo "Estimated Usage | resets on 09/01 | KIRO PRO"
echo "Credits (1 of 50 included)"
`)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("KIRO_API_KEY", "ksk_test")

	report, err := Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch failed with KIRO_API_KEY: %v", err)
	}
	if report.Plan != "KIRO PRO" {
		t.Fatalf("unexpected report: %#v", report)
	}
}

func TestFetchTreatsBrowserLoginFallbackAsNotConfigured(t *testing.T) {
	dir := t.TempDir()
	writeFakeKiro(t, dir, "kiro-cli", `
if [ "$1" = "whoami" ]; then
  echo "Logged in with Builder ID"
  exit 0
fi
echo "Welcome to Kiro CLI, let's get you signed in!"
echo "Press enter to continue to the browser or esc to cancel"
echo "error: No such device or address (os error 6)"
exit 1
`)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("KIRO_API_KEY", "")

	_, err := Fetch(context.Background())
	if !usage.IsNotConfigured(err) {
		t.Fatalf("err = %v, want NotConfigured", err)
	}
}
