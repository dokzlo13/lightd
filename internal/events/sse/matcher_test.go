package sse

import "testing"

func TestParseMatcher_Exact(t *testing.T) {
	m := ParseMatcher("abc123")
	if !m.Matches("abc123") {
		t.Error("Exact should match same value")
	}
	if m.Matches("xyz") {
		t.Error("Exact should not match different value")
	}
	if m.Matches("") {
		t.Error("Exact should not match empty string")
	}
	if m.String() != "abc123" {
		t.Errorf("Exact String() = %q, want %q", m.String(), "abc123")
	}
}

func TestParseMatcher_Any(t *testing.T) {
	m := ParseMatcher("*")
	if !m.Matches("anything") {
		t.Error("Any should match any value")
	}
	if !m.Matches("") {
		t.Error("Any should match empty string")
	}
	if m.String() != "*" {
		t.Errorf("Any String() = %q, want %q", m.String(), "*")
	}
}

func TestParseMatcher_OneOf(t *testing.T) {
	m := ParseMatcher("light|grouped_light")
	if !m.Matches("light") {
		t.Error("OneOf should match 'light'")
	}
	if !m.Matches("grouped_light") {
		t.Error("OneOf should match 'grouped_light'")
	}
	if m.Matches("other") {
		t.Error("OneOf should not match 'other'")
	}
	if m.Matches("") {
		t.Error("OneOf should not match empty string")
	}
}

func TestParseMatcher_SingleValueNotOneOf(t *testing.T) {
	// A single value without pipes should be Exact, not OneOf
	m := ParseMatcher("single")
	if !m.Matches("single") {
		t.Error("Single value should match exactly")
	}
	if m.Matches("other") {
		t.Error("Single value should not match other values")
	}
	// Verify it's an exact matcher, not oneOf
	if _, ok := m.(matchExact); !ok {
		t.Errorf("Single value should be matchExact, got %T", m)
	}
}

func TestParseMatcher_EmptyPipe(t *testing.T) {
	// Edge case: empty segments are skipped
	m := ParseMatcher("a||b")
	if !m.Matches("a") {
		t.Error("Should match 'a'")
	}
	if !m.Matches("b") {
		t.Error("Should match 'b'")
	}
	if m.Matches("") {
		t.Error("Should not match empty string (empty segments skipped)")
	}
}
