// Package webhook provides handler types for HTTP webhook events.
package webhook

import (
	"strings"

	"github.com/dokzlo13/lightd/internal/lua/modules/collect"
)

// Handler is called when a webhook event matches
type Handler struct {
	Method           string
	Path             string
	ActionName       string
	ActionArgs       map[string]any
	CollectorFactory *collect.CollectorFactory // nil = immediate
}

// MatchResult contains a matched handler and extracted path parameters
type MatchResult struct {
	Handler    *Handler
	PathParams map[string]string
}

// MatchPath matches a path pattern against an actual path.
// Pattern: "/group/{id}/toggle"
// Path: "/group/123/toggle"
// Returns extracted params {"id": "123"} and true if matched.
func MatchPath(pattern, path string) (map[string]string, bool) {
	patternParts := strings.Split(strings.Trim(pattern, "/"), "/")
	pathParts := strings.Split(strings.Trim(path, "/"), "/")

	// Must have same number of segments
	if len(patternParts) != len(pathParts) {
		return nil, false
	}

	params := make(map[string]string)

	for i, patternPart := range patternParts {
		pathPart := pathParts[i]

		// Check if this segment is a parameter (e.g., "{id}")
		if len(patternPart) > 2 && patternPart[0] == '{' && patternPart[len(patternPart)-1] == '}' {
			// Extract parameter name and value
			paramName := patternPart[1 : len(patternPart)-1]
			params[paramName] = pathPart
		} else if patternPart != pathPart {
			// Literal segment must match exactly
			return nil, false
		}
	}

	return params, true
}

