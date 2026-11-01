package cmd

import (
	"strings"
	"testing"
)

func TestVersionFlagPrintsVersion(t *testing.T) {
	root := newRootCmd()
	var out strings.Builder
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"--version"})
	if err := root.Execute(); err != nil {
		t.Fatalf("--version exited with error: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, versionString()) {
		t.Fatalf("--version printed %q, want it to contain %q", got, versionString())
	}
}

func TestVersionStringPrefersStampedVersion(t *testing.T) {
	defer func(v string) { version = v }(version)
	version = "v1.2.3"
	if got := versionString(); got != "v1.2.3" {
		t.Fatalf("versionString() = %q, want %q", got, "v1.2.3")
	}
}
