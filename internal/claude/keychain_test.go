package claude

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"
)

func TestClassifySecurityErrorNotFoundExitCode(t *testing.T) {
	err := exec.Command("sh", "-c", "exit 44").Run()
	got := classifySecurityError(err, "")
	if !errors.Is(got, errKeychainCredentialsNotFound) {
		t.Fatalf("got %v, want errKeychainCredentialsNotFound", got)
	}
}

func TestClassifySecurityErrorNotFoundMessage(t *testing.T) {
	err := classifySecurityError(errors.New("exit status 1"), "The specified item could not be found in the keychain.")
	if !errors.Is(err, errKeychainCredentialsNotFound) {
		t.Fatalf("got %v, want errKeychainCredentialsNotFound", err)
	}
}

func TestClassifySecurityErrorOther(t *testing.T) {
	err := classifySecurityError(errors.New("exit status 1"), "User interaction is not allowed.")
	if errors.Is(err, errKeychainCredentialsNotFound) {
		t.Fatal("classified a permission error as not found")
	}
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "security unlock-keychain") {
		t.Fatalf("expected unlock-keychain hint, got %v", err)
	}
}

func TestClassifySecurityErrorCanceled(t *testing.T) {
	err := classifySecurityError(context.Canceled, "")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v, want context.Canceled", err)
	}
}

func TestClassifySecurityErrorTimeout(t *testing.T) {
	err := classifySecurityError(errKeychainTimeout, "")
	if !errors.Is(err, errKeychainTimeout) {
		t.Fatalf("got %v, want errKeychainTimeout", err)
	}
	if !strings.Contains(err.Error(), "permission dialog") {
		t.Fatalf("expected permission-dialog hint, got %v", err)
	}
}
