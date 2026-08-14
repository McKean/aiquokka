package claude

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestMergeCredentialsPreservesEnvelopeAndOAuthFields(t *testing.T) {
	raw := []byte(`{"claudeAiOauth":{"accessToken":"old","refreshToken":"old-refresh","expiresAt":1,"futureOAuthField":"keep"},"futureTopLevelField":"keep"}`)
	merged, err := mergeCredentials(raw, false, &oauth{AccessToken: "new", RefreshToken: "new-refresh", ExpiresAt: 2})
	if err != nil {
		t.Fatal(err)
	}
	assertJSONField(t, merged, "futureTopLevelField", "keep")
	assertJSONField(t, mustObject(t, merged, "claudeAiOauth"), "futureOAuthField", "keep")
	assertJSONField(t, mustObject(t, merged, "claudeAiOauth"), "accessToken", "new")
}

func TestMergeCredentialsPreservesBareOAuthFields(t *testing.T) {
	raw := []byte(`{"accessToken":"old","refreshToken":"old-refresh","expiresAt":1,"futureOAuthField":"keep"}`)
	merged, err := mergeCredentials(raw, true, &oauth{AccessToken: "new", RefreshToken: "new-refresh", ExpiresAt: 2})
	if err != nil {
		t.Fatal(err)
	}
	assertJSONField(t, merged, "futureOAuthField", "keep")
	assertJSONField(t, merged, "accessToken", "new")
}

func TestLoadCredentialsPrefersFileOverKeychain(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.MkdirAll(filepath.Join(home, ".claude"), 0o700); err != nil {
		t.Fatal(err)
	}
	raw := []byte(`{"claudeAiOauth":{"accessToken":"file-token","refreshToken":"file-refresh"}}`)
	if err := os.WriteFile(filepath.Join(home, ".claude", ".credentials.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}

	original := readKeychainCredentials
	t.Cleanup(func() { readKeychainCredentials = original })
	readKeychainCredentials = func() ([]byte, []byte, error) {
		return []byte(`{"accessToken":"keychain-token"}`), []byte("item"), nil
	}

	o, err := loadCredentials()
	if err != nil {
		t.Fatal(err)
	}
	if o.AccessToken != "file-token" {
		t.Fatalf("AccessToken = %q, want file-token", o.AccessToken)
	}
	if len(o.source.keychainItem) != 0 {
		t.Fatal("expected file credentials, not a Keychain source")
	}
}

func TestLoadCredentialsFallsBackToKeychainWhenFileHasNoToken(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.MkdirAll(filepath.Join(home, ".claude"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".claude", ".credentials.json"), []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}

	original := readKeychainCredentials
	t.Cleanup(func() { readKeychainCredentials = original })
	readKeychainCredentials = func() ([]byte, []byte, error) {
		return []byte(`{"accessToken":"keychain-token"}`), []byte("item"), nil
	}

	o, err := loadCredentials()
	if err != nil {
		t.Fatal(err)
	}
	if o.AccessToken != "keychain-token" {
		t.Fatalf("AccessToken = %q, want keychain-token", o.AccessToken)
	}
	if string(o.source.keychainItem) != "item" {
		t.Fatalf("keychainItem = %q, want item", o.source.keychainItem)
	}
}

func TestLoadCredentialsFallsBackToKeychain(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	original := readKeychainCredentials
	t.Cleanup(func() { readKeychainCredentials = original })
	readKeychainCredentials = func() ([]byte, []byte, error) {
		return []byte(`{"accessToken":"keychain-token"}`), []byte("item"), nil
	}

	o, err := loadCredentials()
	if err != nil {
		t.Fatal(err)
	}
	if o.AccessToken != "keychain-token" {
		t.Fatalf("AccessToken = %q, want keychain-token", o.AccessToken)
	}
	if string(o.source.keychainItem) != "item" {
		t.Fatalf("keychainItem = %q, want item", o.source.keychainItem)
	}
}

func TestPersistKeychainMergesIntoFreshCredential(t *testing.T) {
	originalRead := readKeychainItem
	originalUpdate := updateKeychainItem
	t.Cleanup(func() {
		readKeychainItem = originalRead
		updateKeychainItem = originalUpdate
	})

	readKeychainItem = func([]byte) ([]byte, error) {
		return []byte(`{"claudeAiOauth":{"accessToken":"old","refreshToken":"old-refresh","expiresAt":1,"claudeChangedThis":"keep"}}`), nil
	}
	var written []byte
	updateKeychainItem = func(_ []byte, data []byte) error {
		written = data
		return nil
	}

	err := persist(&oauth{
		AccessToken:  "new",
		RefreshToken: "new-refresh",
		ExpiresAt:    2,
		source:       credentialSource{keychainItem: []byte("exact-item")},
	})
	if err != nil {
		t.Fatal(err)
	}
	assertJSONField(t, mustObject(t, written, "claudeAiOauth"), "claudeChangedThis", "keep")
	assertJSONField(t, mustObject(t, written, "claudeAiOauth"), "accessToken", "new")
}

func mustObject(t *testing.T, data []byte, field string) []byte {
	t.Helper()
	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil {
		t.Fatal(err)
	}
	return object[field]
}

func assertJSONField(t *testing.T, data []byte, field, want string) {
	t.Helper()
	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil {
		t.Fatal(err)
	}
	var got string
	if err := json.Unmarshal(object[field], &got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("%s = %q, want %q", field, got, want)
	}
}
