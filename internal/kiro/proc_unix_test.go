//go:build unix

package kiro

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"
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
