package kimi

import (
	"os"
	"strconv"
)

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

func getenv(k string) string { return os.Getenv(k) }
