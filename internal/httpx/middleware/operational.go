package middleware

import "strings"

func operationalPath(path string) bool {
	return strings.HasSuffix(path, "/health/live") ||
		strings.HasSuffix(path, "/health/ready") ||
		strings.HasSuffix(path, "/metrics")
}
