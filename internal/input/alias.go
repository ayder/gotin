package input

import (
	"strings"

	"dmud/internal/pkg/parser"
)

// MaxAliasExpansionDepth limits recursive alias expansion to prevent infinite loops.
const MaxAliasExpansionDepth = 5

// Alias represents an advanced alias with pattern matching and variable substitution.
type Alias struct {
	Pattern   string // e.g., "k $1" - the pattern to match with variables
	Expansion string // e.g., "kill $1; skin corpse" - the expansion with variables and multi-commands
}

// AliasManager manages user-defined command aliases.
type AliasManager struct {
	aliases map[string]*Alias // key is the first word of the pattern (the trigger)
}

// NewAliasManager creates a new alias manager.
func NewAliasManager() *AliasManager {
	return &AliasManager{
		aliases: make(map[string]*Alias),
	}
}

// Set adds or updates an alias using the new pattern/expansion format.
func (am *AliasManager) Set(pattern, expansion string) {
	// Extract the trigger word (first word of the pattern)
	trigger := extractTrigger(pattern)
	if trigger == "" {
		return
	}

	am.aliases[trigger] = &Alias{
		Pattern:   pattern,
		Expansion: expansion,
	}
}

// Get retrieves an alias by its trigger word. Returns the alias and whether it exists.
func (am *AliasManager) Get(trigger string) (*Alias, bool) {
	alias, ok := am.aliases[trigger]
	return alias, ok
}

// Delete removes an alias by its trigger word. Returns true if it existed.
func (am *AliasManager) Delete(trigger string) bool {
	if _, ok := am.aliases[trigger]; ok {
		delete(am.aliases, trigger)
		return true
	}
	return false
}

// List returns all defined aliases as a map of pattern -> expansion for display/persistence.
func (am *AliasManager) List() map[string]string {
	result := make(map[string]string, len(am.aliases))
	for _, alias := range am.aliases {
		result[alias.Pattern] = alias.Expansion
	}
	return result
}

// ListAliases returns all aliases as structured objects.
func (am *AliasManager) ListAliases() []*Alias {
	result := make([]*Alias, 0, len(am.aliases))
	for _, alias := range am.aliases {
		result = append(result, alias)
	}
	return result
}

// Expand expands aliases in the given text and returns a list of commands.
// It matches the input against alias patterns, performs variable substitution,
// and splits by semicolons for multi-command sequences.
func (am *AliasManager) Expand(text string) []string {
	return am.expandWithDepth(text, 0)
}

// expandWithDepth performs alias expansion with recursion tracking.
func (am *AliasManager) expandWithDepth(text string, depth int) []string {
	if depth >= MaxAliasExpansionDepth {
		return []string{text}
	}

	text = strings.TrimSpace(text)
	if text == "" {
		return []string{}
	}

	// Split into first word and rest to find the trigger
	parts := strings.SplitN(text, " ", 2)
	trigger := parts[0]

	// Check if the trigger matches an alias
	alias, ok := am.aliases[trigger]
	if !ok {
		// No alias match, return original text as single command
		return []string{text}
	}

	// Match the input against the pattern and extract variables
	vars := matchPattern(alias.Pattern, text)
	if vars == nil {
		// Pattern didn't match (shouldn't happen if trigger matched, but just in case)
		return []string{text}
	}

	// Substitute variables in the expansion
	expanded := substituteVariables(alias.Expansion, vars)

	// Split by semicolons into multiple commands
	commands := parser.SplitCommands(expanded)

	// Recursively expand each command (in case expansion contains another alias)
	var result []string
	for _, cmd := range commands {
		expanded := am.expandWithDepth(cmd, depth+1)
		result = append(result, expanded...)
	}

	return result
}

// extractTrigger extracts the first word (trigger) from a pattern.
// Example: "k $1" -> "k", "setup" -> "setup"
func extractTrigger(pattern string) string {
	pattern = strings.TrimSpace(pattern)
	parts := strings.SplitN(pattern, " ", 2)
	if len(parts) == 0 {
		return ""
	}
	return parts[0]
}

// matchPattern matches input text against a pattern and extracts variable values.
// Returns a map of variable name -> value, or nil if no match.
// Example: pattern="k $1", input="k rat" -> {"$1": "rat", "$*": "rat"}
func matchPattern(pattern, input string) map[string]string {
	patternParts := strings.Fields(pattern)
	inputParts := strings.Fields(input)

	if len(patternParts) == 0 {
		return nil
	}

	// First word must match exactly (the trigger)
	if len(inputParts) == 0 || patternParts[0] != inputParts[0] {
		return nil
	}

	vars := make(map[string]string)
	inputIdx := 1 // Start after the trigger word

	// Process remaining pattern parts
	for i := 1; i < len(patternParts); i++ {
		part := patternParts[i]
		if strings.HasPrefix(part, "$") {
			// This is a variable placeholder
			if inputIdx < len(inputParts) {
				vars[part] = inputParts[inputIdx]
				inputIdx++
			} else {
				// No more input for this variable, use empty string
				vars[part] = ""
			}
		} else {
			// Literal part must match exactly
			if inputIdx >= len(inputParts) || inputParts[inputIdx] != part {
				return nil
			}
			inputIdx++
		}
	}

	// $* captures all remaining arguments (everything after what pattern consumed)
	if inputIdx < len(inputParts) {
		vars["$*"] = strings.Join(inputParts[inputIdx:], " ")
	} else {
		vars["$*"] = ""
	}

	// Also set $* to all args after trigger if no explicit variables were in pattern
	if len(patternParts) == 1 && len(inputParts) > 1 {
		vars["$*"] = strings.Join(inputParts[1:], " ")
		// For patterns without explicit variables, map $1, $2, etc. to positional args
		for i := 1; i < len(inputParts); i++ {
			vars["$"+string(rune('0'+i))] = inputParts[i]
		}
	}

	return vars
}

// substituteVariables replaces $1, $2, $*, etc. in the expansion with actual values.
// If the expansion doesn't contain any variable references and there are remaining
// arguments ($*), append them to the end of the expansion.
func substituteVariables(expansion string, vars map[string]string) string {
	result := expansion
	hasVariableRef := false

	// Check if expansion contains any variable references
	if strings.Contains(expansion, "$") {
		hasVariableRef = true
	}

	// Replace specific numbered variables first ($1, $2, ... $9)
	for i := 9; i >= 1; i-- {
		varName := "$" + string(rune('0'+i))
		if val, ok := vars[varName]; ok {
			result = strings.ReplaceAll(result, varName, val)
		}
	}

	// Replace $* (all remaining arguments)
	if val, ok := vars["$*"]; ok {
		result = strings.ReplaceAll(result, "$*", val)
	}

	// If expansion had no variable references and there are remaining args,
	// append them to the end (for simple aliases like "k" -> "kill")
	if !hasVariableRef {
		if val, ok := vars["$*"]; ok && val != "" {
			result = result + " " + val
		}
	}

	return result
}
