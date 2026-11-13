package antigravity

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"time"
)

const (
	keyringService = "gemini"
	keyringAccount = "antigravity"
	keyringTimeout = 2 * time.Second

	goKeyringBase64Prefix = "go-keyring-base64:"
)

var (
	errKeyringNotFound = errors.New("Antigravity credentials not found in keyring")
	errKeyringLocked   = errors.New("Antigravity keyring is locked or unavailable without user interaction")
	errKeyringTimeout  = errors.New("timed out reading Antigravity credentials from keyring")

	readKeyring  = readKeyringCredential
	writeKeyring = writeKeyringCredential
)

// decodeKeyringSecret unwraps the payload stored by zalando/go-keyring or
// macOS security(1): optional hex encoding (used by security -w for multi-line
// secrets) and the "go-keyring-base64:" prefix.
func decodeKeyringSecret(raw []byte) []byte {
	b := bytes.TrimSpace(raw)
	if len(b) == 0 {
		return nil
	}
	if !bytes.HasPrefix(b, []byte("{")) && !bytes.HasPrefix(b, []byte(goKeyringBase64Prefix)) && len(b)%2 == 0 {
		if decoded, err := hex.DecodeString(string(b)); err == nil {
			trimmed := bytes.TrimSpace(decoded)
			if bytes.HasPrefix(trimmed, []byte("{")) || bytes.HasPrefix(trimmed, []byte(goKeyringBase64Prefix)) {
				b = trimmed
			}
		}
	}
	if bytes.HasPrefix(b, []byte(goKeyringBase64Prefix)) {
		enc := bytes.TrimSpace(b[len(goKeyringBase64Prefix):])
		if decoded, err := base64.StdEncoding.DecodeString(string(enc)); err == nil {
			return bytes.TrimSpace(decoded)
		}
		if decoded, err := base64.RawStdEncoding.DecodeString(string(enc)); err == nil {
			return bytes.TrimSpace(decoded)
		}
	}
	return b
}
