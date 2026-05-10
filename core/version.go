package core

import (
	"regexp"
	"slices"
	"strings"
)

// versionTokenRegex matches version-like tokens such as "1.21.1", "4.10.0", "1.4.25.1696".
var versionTokenRegex = regexp.MustCompile(`\d+\.\d+(?:\.\d+)*`)

// ExtractVersionTokens finds all version-like tokens in s and returns them
// ordered from most specific (most components) to least specific.
// For example, "sodium-1.21.1-0.6.0.jar" returns ["0.6.0", "1.21.1"].
func ExtractVersionTokens(s string) []string {
	matches := versionTokenRegex.FindAllString(s, -1)
	if len(matches) == 0 {
		return nil
	}
	slices.SortFunc(matches, func(a, b string) int {
		aParts := len(strings.Split(a, "."))
		bParts := len(strings.Split(b, "."))
		return bParts - aParts // most specific first
	})
	return matches
}

// MatchVersionString checks whether the version tokens extracted from source
// can be found as substrings in target. It uses specificity-aware matching:
// tokens with 4+ components (like "1.4.25.1696") take priority over 3-component
// tokens (like "1.21.1") that are likely just Minecraft version prefixes.
// If any high-specificity token exists in source but not in target, 3-component
// tokens in source are ignored to avoid false matches on MC version numbers.
func MatchVersionString(target, source string) bool {
	tokens := ExtractVersionTokens(source)
	if len(tokens) == 0 {
		return false
	}

	// Check if any high-specificity (4+ components) token exists
	hasHighSpecificity := false
	for _, m := range tokens {
		if len(strings.Split(m, ".")) >= 4 {
			hasHighSpecificity = true
			break
		}
	}

	for _, m := range tokens {
		numComponents := len(strings.Split(m, "."))
		if hasHighSpecificity && numComponents < 4 {
			continue // skip MC version prefixes when mod version is more specific
		}
		if strings.Contains(target, m) {
			return true
		}
	}
	return false
}