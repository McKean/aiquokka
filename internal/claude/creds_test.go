package claude

import (
	"encoding/json"
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
