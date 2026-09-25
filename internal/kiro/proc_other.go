//go:build !unix

package kiro

import "os/exec"

func killProcessTree(*exec.Cmd) {}
