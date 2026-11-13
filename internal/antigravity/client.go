package antigravity

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	clientIDEnvVar     = "ANTIGRAVITY_CLIENT_ID"
	clientSecretEnvVar = "ANTIGRAVITY_CLIENT_SECRET"

	clientIDDomain    = ".apps.googleusercontent.com"
	clientSecretPfx   = "GOCSPX-"
	clientSecretTailN = 28
	scanChunkSize     = 1 << 20
	scanOverlap       = 256
)

type oauthClient struct {
	ID     string `json:"client_id"`
	Secret string `json:"client_secret"`
}

type cachedClient struct {
	BinaryPath string      `json:"binary_path"`
	Size       int64       `json:"size"`
	ModTimeNS  int64       `json:"mod_time_ns"`
	Client     oauthClient `json:"client"`
}

var (
	findAgyBinaryFn   = findAgyBinary
	clientCachePathFn = defaultClientCachePath
)

func findAgyBinary() (string, error) {
	if p, err := exec.LookPath("agy"); err == nil {
		return p, nil
	}
	if home, err := os.UserHomeDir(); err == nil {
		p := filepath.Join(home, ".local", "bin", "agy")
		if info, err := os.Stat(p); err == nil && !info.IsDir() {
			return p, nil
		}
	}
	return "", errors.New("agy binary not found on PATH")
}

func defaultClientCachePath() (string, error) {
	dir, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "aiquokka", "antigravity-oauth-client.json"), nil
}

type clientSource struct {
	binPath string
	info    os.FileInfo
	cached  *oauthClient
}

func openClientSource() (*clientSource, []oauthClient, error) {
	if id := strings.TrimSpace(os.Getenv(clientIDEnvVar)); id != "" {
		if sec := strings.TrimSpace(os.Getenv(clientSecretEnvVar)); sec != "" {
			return nil, []oauthClient{{ID: id, Secret: sec}}, nil
		}
	}

	binPath, err := findAgyBinaryFn()
	if err != nil {
		return nil, nil, fmt.Errorf("cannot refresh Antigravity token without `agy` (or %s/%s): %w", clientIDEnvVar, clientSecretEnvVar, err)
	}
	info, err := os.Stat(binPath)
	if err != nil {
		return nil, nil, err
	}

	src := &clientSource{binPath: binPath, info: info}
	if c := loadCachedClient(binPath, info); c != nil {
		src.cached = c
		return src, []oauthClient{*c}, nil
	}
	scanned, err := src.scan()
	if err != nil {
		return nil, nil, err
	}
	return src, scanned, nil
}

func (s *clientSource) scan() ([]oauthClient, error) {
	if s == nil {
		return nil, nil
	}
	ids, secrets, err := scanBinaryOAuthTokens(s.binPath)
	if err != nil {
		return nil, fmt.Errorf("scanning %s for OAuth client: %w", s.binPath, err)
	}
	var out []oauthClient
	seen := make(map[oauthClient]bool)
	for _, id := range ids {
		for _, sec := range secrets {
			c := oauthClient{ID: id, Secret: sec}
			if s.cached != nil && *s.cached == c {
				continue
			}
			if !seen[c] {
				seen[c] = true
				out = append(out, c)
			}
		}
	}
	if len(out) == 0 && s.cached == nil {
		return nil, fmt.Errorf("no OAuth client credentials found in %s", s.binPath)
	}
	return out, nil
}

func loadCachedClient(binPath string, info os.FileInfo) *oauthClient {
	cachePath, err := clientCachePathFn()
	if err != nil {
		return nil
	}
	data, err := os.ReadFile(cachePath)
	if err != nil {
		return nil
	}
	var c cachedClient
	if err := json.Unmarshal(data, &c); err != nil {
		return nil
	}
	if c.BinaryPath == binPath && c.Size == info.Size() && c.ModTimeNS == info.ModTime().UnixNano() && c.Client.ID != "" && c.Client.Secret != "" {
		return &c.Client
	}
	return nil
}

func saveCachedClient(src *clientSource, client oauthClient) {
	if src == nil || src.binPath == "" || src.info == nil {
		return
	}
	cachePath, err := clientCachePathFn()
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o700); err != nil {
		return
	}
	payload, err := json.Marshal(cachedClient{
		BinaryPath: src.binPath,
		Size:       src.info.Size(),
		ModTimeNS:  src.info.ModTime().UnixNano(),
		Client:     client,
	})
	if err != nil {
		return
	}
	_ = writeAtomic0600(cachePath, payload)
}

func scanBinaryOAuthTokens(path string) (ids, secrets []string, err error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()

	idSeen := make(map[string]bool)
	secSeen := make(map[string]bool)
	buf := make([]byte, scanChunkSize+scanOverlap)
	carry := 0

	for {
		n, readErr := f.Read(buf[carry:])
		total := carry + n
		if total > 0 {
			window := buf[:total]
			extractClientIDs(window, idSeen, &ids)
			extractClientSecrets(window, secSeen, &secrets)
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return nil, nil, readErr
		}
		if total > scanOverlap {
			copy(buf[:scanOverlap], buf[total-scanOverlap:total])
			carry = scanOverlap
		} else {
			carry = total
		}
	}
	return ids, secrets, nil
}

func extractClientIDs(data []byte, seen map[string]bool, out *[]string) {
	dom := []byte(clientIDDomain)
	for pos := 0; pos < len(data); {
		idx := bytes.Index(data[pos:], dom)
		if idx < 0 {
			return
		}
		end := pos + idx
		// Scan backward over "<digits>-<alnum>" so preceding concatenated .rodata
		// lowercase letters (e.g. "...error1071006...") do not get included.
		dash := end
		for dash > 0 && isLowerAlnum(data[dash-1]) {
			dash--
		}
		if end-dash >= 12 && dash > 0 && data[dash-1] == '-' {
			start := dash - 1
			for start > 0 && data[start-1] >= '0' && data[start-1] <= '9' {
				start--
			}
			if (dash-1)-start >= 9 {
				full := string(data[start:end]) + clientIDDomain
				if !seen[full] {
					seen[full] = true
					*out = append(*out, full)
				}
			}
		}
		pos = end + len(dom)
	}
}

func extractClientSecrets(data []byte, seen map[string]bool, out *[]string) {
	pfx := []byte(clientSecretPfx)
	for pos := 0; pos < len(data); {
		idx := bytes.Index(data[pos:], pfx)
		if idx < 0 {
			return
		}
		start := pos + idx
		end := start + len(pfx) + clientSecretTailN
		if end <= len(data) && isAllSecretTail(data[start+len(pfx):end]) {
			sec := string(data[start:end])
			if !seen[sec] {
				seen[sec] = true
				*out = append(*out, sec)
			}
		}
		pos = start + len(pfx)
	}
}

func isLowerAlnum(b byte) bool {
	return (b >= '0' && b <= '9') || (b >= 'a' && b <= 'z')
}

func isAllSecretTail(b []byte) bool {
	for _, c := range b {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c == '-' || c == '_') {
			return false
		}
	}
	return true
}
