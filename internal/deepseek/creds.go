// Package deepseek reads a DeepSeek API key and reports account balance.
package deepseek

import (
	"os"
	"strings"

	"github.com/McKean/aiquokka/internal/usage"
)

// loadKey returns the DeepSeek API key from the environment. DeepSeek has no
// official coding CLI that stores credentials, so the standard API-key env var
// is the only source.
func loadKey() (string, error) {
	if v := strings.TrimSpace(os.Getenv("DEEPSEEK_API_KEY")); v != "" {
		return v, nil
	}
	return "", usage.NotConfigured("no DeepSeek API key found — set DEEPSEEK_API_KEY (sk-…)")
}
