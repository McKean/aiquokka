package antigravity

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/McKean/aiquokka/internal/usage"
)

type quotaResponse struct {
	Response struct {
		Groups []struct {
			DisplayName string `json:"displayName"`
			Buckets     []struct {
				BucketID          string  `json:"bucketId"`
				DisplayName       string  `json:"displayName"`
				Window            string  `json:"window"`
				RemainingFraction float64 `json:"remainingFraction"`
				ResetTime         string  `json:"resetTime"`
			} `json:"buckets"`
		} `json:"groups"`
	} `json:"response"`
}

func getAgyHttpPorts() ([]string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	
	dir := filepath.Join(home, ".gemini", "antigravity-cli", "log")
	files, _ := filepath.Glob(filepath.Join(dir, "cli*.log"))
	files = append(files, filepath.Join(home, ".gemini", "antigravity-cli", "cli.log"))
	
	re := regexp.MustCompile(`Language server listening on random port at (\d+) for HTTP`)
	var ports []string
	
	for _, file := range files {
		b, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		matches := re.FindAllStringSubmatch(string(b), -1)
		for _, m := range matches {
			ports = append(ports, m[1])
		}
	}
	
	if len(ports) == 0 {
		return nil, usage.NotConfigured("agy language server port not found in logs (is agy running?)")
	}
	
	// Reverse the order to try the newest ports first
	for i, j := 0, len(ports)-1; i < j; i, j = i+1, j-1 {
		ports[i], ports[j] = ports[j], ports[i]
	}
	
	return ports, nil
}

func Fetch(ctx context.Context) (*usage.Report, error) {
	ports, err := getAgyHttpPorts()
	if err != nil {
		return nil, err
	}

	var lastErr error
	for _, port := range ports {
		url := fmt.Sprintf("http://127.0.0.1:%s/exa.language_server_pb.LanguageServerService/RetrieveUserQuotaSummary", port)
		req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader([]byte("{}")))
		if err != nil {
			lastErr = err
			continue
		}
		req.Header.Set("Content-Type", "application/json")

		// Short timeout for fast probing
		client := &http.Client{Timeout: 500 * time.Millisecond}
		resp, err := client.Do(req)
		if err != nil {
			if strings.Contains(err.Error(), "connection refused") || os.IsTimeout(err) {
				lastErr = usage.NotConfigured("agy is not running")
				continue
			}
			lastErr = err
			continue
		}
		
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			lastErr = fmt.Errorf("%s: %s", resp.Status, string(body))
			continue
		}

		var out quotaResponse
		if err := json.Unmarshal(body, &out); err != nil {
			lastErr = fmt.Errorf("decoding usage: %w", err)
			continue
		}

		report := &usage.Report{Provider: "Antigravity", Plan: "Usage"}
		
		for _, group := range out.Response.Groups {
			for _, b := range group.Buckets {
				used := (1.0 - b.RemainingFraction) * 100.0
				var resetTime time.Time
				if b.ResetTime != "" {
					resetTime, _ = time.Parse(time.RFC3339, b.ResetTime)
				}
				
				w := usage.Window{
					Label:       fmt.Sprintf("%s - %s", group.DisplayName, b.DisplayName),
					UsedPercent: &used,
					ResetsAt:    resetTime,
				}
				
				if b.Window == "weekly" {
					w.Duration = 7 * 24 * time.Hour
				} else if b.Window == "5h" {
					w.Duration = 5 * time.Hour
				}
				
				report.Windows = append(report.Windows, w)
			}
		}
		
		if len(report.Windows) == 0 {
			lastErr = fmt.Errorf("no quota buckets found in response")
			continue
		}

		// Success!
		return report, nil
	}

	return nil, lastErr
}
