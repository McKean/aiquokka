package muse

import (
	"errors"
	"strings"
)

// The Muse CLI writes its device-code login to this Keychain item, under this
// service and account, when the token is not inline in auth.json.
const (
	keychainService = "ai.meta.dev.credentials"
	keychainAccount = "meta"
)

// The commands the unreadable-item message hands the user. aiquokka's own
// read is killed after securityTimeout, so a grant can only come from a read
// the user runs with no time limit, answered with "Always Allow".
//
// The second is for an item that still asks afterwards. The CLI creates the
// item with only its own team in the partition list, and `security` belongs
// to the apple-tool: partition; the command adds that, keeping the CLI's team
// so the CLI can still read its login.
const (
	museTeamPartition        = "teamid:V9WTTPBFK9"
	keychainItemArgs         = "-s " + keychainService + " -a " + keychainAccount
	keychainReadCommand      = "security find-generic-password " + keychainItemArgs + " -w >/dev/null"
	keychainPartitionCommand = "security set-generic-password-partition-list -S apple-tool:,apple:," + museTeamPartition + " " + keychainItemArgs
)

// keychainLoader reads the CLI's Keychain item and returns its OAuth access
// token. It is a variable so tests can stub the `security` call, the way
// they stub keyEndpoint for the mint call.
var keychainLoader = loadKeychainToken

// keychainUnreadableError marks a Keychain item that may exist but could not
// be read: a permission dialog nobody answered within the bounded `security`
// read (it times out), a locked Keychain, or any other `security` failure. It unwraps to errKeychainNotFound so callers that only ask "is
// there a login?" keep treating it as absent, while callers that know
// auth.json names a login report the reason instead of "no Muse login".
type keychainUnreadableError struct{ reason string }

func (e *keychainUnreadableError) Error() string {
	return "Muse Keychain login could not be read: " + e.reason
}

func (e *keychainUnreadableError) Unwrap() error { return errKeychainNotFound }

// asKeychainUnreadable reports whether err is an unreadable-Keychain outcome,
// with its reason.
func asKeychainUnreadable(err error) (*keychainUnreadableError, bool) {
	var unread *keychainUnreadableError
	if errors.As(err, &unread) {
		return unread, true
	}
	return nil, false
}

// classifyKeychainFailure maps a failed `security find-generic-password` read
// to its outcome: errKeychainNotFound when the item is not there (exit 44,
// "could not be found"), or a *keychainUnreadableError when it may be there
// but `security` could not read it. timedOut reports the bounded read hit its
// deadline, which is how a permission dialog shows up.
func classifyKeychainFailure(timedOut bool, exitCode int, hasExitCode bool, stderr string, runErr error) error {
	if timedOut {
		return &keychainUnreadableError{reason: "the `security` read timed out"}
	}
	if (hasExitCode && exitCode == 44) || strings.Contains(stderr, "could not be found") {
		return errKeychainNotFound
	}
	if strings.Contains(stderr, "User interaction is not allowed") ||
		strings.Contains(stderr, "because it is locked") ||
		strings.Contains(stderr, "Unable to display a dialogue") {
		return &keychainUnreadableError{reason: "Keychain is locked or needs approval"}
	}
	if stderr == "" {
		if runErr != nil {
			return &keychainUnreadableError{reason: oneLineKeychainReason(runErr.Error())}
		}
		return &keychainUnreadableError{reason: "unknown error"}
	}
	return &keychainUnreadableError{reason: oneLineKeychainReason(stderr)}
}

// oneLineKeychainReason keeps diagnostics from security stderr and process
// errors on the same line as the actionable NotConfigured message.
func oneLineKeychainReason(reason string) string {
	return strings.Join(strings.Fields(reason), " ")
}
