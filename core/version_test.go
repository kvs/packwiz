package core

import "testing"

func TestExtractVersionTokens(t *testing.T) {
	tests := []struct {
		input string
		want  []string
	}{
		// Most-specific-first ordering: 4-component before 3-component
		{"jei-1.21.1-1.4.25.1696.jar", []string{"1.4.25.1696", "1.21.1"}},
		// Equal specificity: ordered by appearance in string
		{"sodium-1.21.1-0.6.0.jar", []string{"1.21.1", "0.6.0"}},
		// Single version token
		{"mod-0.6.0.jar", []string{"0.6.0"}},
		// No version tokens
		{"no-version-here", nil},
		{"", nil},
	}
	for _, tt := range tests {
		got := ExtractVersionTokens(tt.input)
		if len(got) != len(tt.want) {
			t.Errorf("ExtractVersionTokens(%q) = %v, want %v", tt.input, got, tt.want)
			continue
		}
		for i := range got {
			if got[i] != tt.want[i] {
				t.Errorf("ExtractVersionTokens(%q)[%d] = %q, want %q", tt.input, i, got[i], tt.want[i])
			}
		}
	}
}

func TestMatchVersionString(t *testing.T) {
	tests := []struct {
		target string
		source string
		want   bool
	}{
		// High-specificity mod version should take priority over MC version
		{"1.4.25.1696", "jei-1.21.1-1.4.25.1696.jar", true},
		// 3-component MC version should NOT match when high-specificity exists but doesn't match target
		{"1.21.1", "jei-1.21.1-1.4.25.1696.jar", false},
		// Without high-specificity, 3-component should match
		{"1.21.1", "sodium-1.21.1-0.6.0.jar", true},
		// Direct substring match
		{"0.6.0", "sodium-0.6.0.jar", true},
		// No version tokens
		{"anything", "no-version", false},
		// Empty source
		{"1.0.0", "", false},
	}
	for _, tt := range tests {
		got := MatchVersionString(tt.target, tt.source)
		if got != tt.want {
			t.Errorf("MatchVersionString(%q, %q) = %v, want %v", tt.target, tt.source, got, tt.want)
		}
	}
}