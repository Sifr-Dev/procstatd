package api

import (
	"fmt"
	"net/url"
	"strings"
)

// ParseAllowedOrigins parses a comma-separated exact-origin allowlist.
// Wildcards, credentials, paths, queries, and fragments are rejected.
func ParseAllowedOrigins(raw string) ([]string, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}

	parts := strings.Split(raw, ",")
	origins := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))

	for _, part := range parts {
		origin := strings.TrimSpace(part)
		if origin == "" {
			return nil, fmt.Errorf("allowed origin list contains an empty value")
		}
		if origin == "*" || strings.Contains(origin, "*") {
			return nil, fmt.Errorf("allowed origin %q must not contain a wildcard", origin)
		}

		parsed, err := url.Parse(origin)
		if err != nil {
			return nil, fmt.Errorf("parse allowed origin %q: %w", origin, err)
		}
		if parsed.Scheme != "http" && parsed.Scheme != "https" {
			return nil, fmt.Errorf("allowed origin %q must use http or https", origin)
		}
		if parsed.Host == "" || parsed.Hostname() == "" {
			return nil, fmt.Errorf("allowed origin %q must include a host", origin)
		}
		if parsed.User != nil {
			return nil, fmt.Errorf("allowed origin %q must not include credentials", origin)
		}
		if parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Opaque != "" {
			return nil, fmt.Errorf("allowed origin %q must not include a path, query, or fragment", origin)
		}

		normalized := strings.ToLower(parsed.Scheme) + "://" + strings.ToLower(parsed.Host)
		if _, duplicate := seen[normalized]; duplicate {
			continue
		}
		seen[normalized] = struct{}{}
		origins = append(origins, normalized)
	}

	return origins, nil
}
