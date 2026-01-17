package input

import (
	"strings"
)

// MaxAliasExpansionDepth limits recursive alias expansion to prevent infinite loops.
const MaxAliasExpansionDepth = 10

// AliasManager manages user-defined command aliases.
type AliasManager struct {
	aliases map[string]string
}

// NewAliasManager creates a new alias manager.
func NewAliasManager() *AliasManager {
	return &AliasManager{
		aliases: make(map[string]string),
	}
}

// Set adds or updates an alias.
func (am *AliasManager) Set(key, value string) {
	am.aliases[key] = value
}

// Get retrieves an alias value. Returns the value and whether it exists.
func (am *AliasManager) Get(key string) (string, bool) {
	value, ok := am.aliases[key]
	return value, ok
}

// Delete removes an alias. Returns true if it existed.
func (am *AliasManager) Delete(key string) bool {
	if _, ok := am.aliases[key]; ok {
		delete(am.aliases, key)
		return true
	}
	return false
}

// List returns all defined aliases.
func (am *AliasManager) List() map[string]string {
	// Return a copy to prevent external modification
	result := make(map[string]string, len(am.aliases))
	for k, v := range am.aliases {
		result[k] = v
	}
	return result
}

// Expand expands aliases in the given text.
// It replaces the first word with its alias value if one exists.
// Supports recursive expansion up to MaxAliasExpansionDepth.
func (am *AliasManager) Expand(text string) string {
	return am.expandWithDepth(text, 0)
}

// expandWithDepth performs alias expansion with recursion tracking.
func (am *AliasManager) expandWithDepth(text string, depth int) string {
	if depth >= MaxAliasExpansionDepth {
		return text
	}

	text = strings.TrimSpace(text)
	if text == "" {
		return text
	}

	// Split into first word and rest
	parts := strings.SplitN(text, " ", 2)
	firstWord := parts[0]

	// Check if first word is an alias
	if expanded, ok := am.aliases[firstWord]; ok {
		// Replace first word with alias value
		var result string
		if len(parts) > 1 {
			result = expanded + " " + parts[1]
		} else {
			result = expanded
		}
		// Recursively expand in case the expansion contains another alias
		return am.expandWithDepth(result, depth+1)
	}

	return text
}
