package sse

// Matcher checks if a value matches a pattern.
// Implementations are immutable and safe for concurrent use.
type Matcher interface {
	// Matches returns true if the value matches this pattern
	Matches(value string) bool
	// String returns a human-readable representation
	String() string
}

// matchAny matches any value (wildcard).
type matchAny struct{}

func (matchAny) Matches(string) bool { return true }
func (matchAny) String() string      { return "*" }

// matchExact matches a single exact value.
type matchExact string

func (m matchExact) Matches(value string) bool { return string(m) == value }
func (m matchExact) String() string            { return string(m) }

// matchOneOf matches any value in a set.
type matchOneOf []string

func (m matchOneOf) Matches(value string) bool {
	for _, v := range m {
		if v == value {
			return true
		}
	}
	return false
}

func (m matchOneOf) String() string {
	if len(m) == 0 {
		return "(none)"
	}
	result := m[0]
	for i := 1; i < len(m); i++ {
		result += "|" + m[i]
	}
	return result
}

// ParseMatcher creates a Matcher from a string pattern.
// - "*" becomes matchAny (matches everything)
// - "a|b|c" becomes matchOneOf{"a", "b", "c"}
// - anything else becomes matchExact (exact match)
func ParseMatcher(pattern string) Matcher {
	if pattern == "*" {
		return matchAny{}
	}
	// Check for pipe-separated values
	for i := 0; i < len(pattern); i++ {
		if pattern[i] == '|' {
			return parseOneOf(pattern)
		}
	}
	return matchExact(pattern)
}

func parseOneOf(pattern string) matchOneOf {
	var result []string
	start := 0
	for i := 0; i <= len(pattern); i++ {
		if i == len(pattern) || pattern[i] == '|' {
			if i > start {
				result = append(result, pattern[start:i])
			}
			start = i + 1
		}
	}
	return matchOneOf(result)
}
