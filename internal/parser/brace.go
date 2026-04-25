package parser

import (
	"strings"
)

// ParseBraceArgs extracts content between brace delimiters from a string.
// Input: "{pattern} {response}" or "pattern response" (fallback to space-separated)
// Returns the extracted parts. If braces are used, content between matching braces is extracted.
// Example: "{k $1} {kill $1; skin corpse}" -> ["k $1", "kill $1; skin corpse"]
func ParseBraceArgs(text string) ([]string, error) {
	var results []string
	remaining := strings.TrimSpace(text)

	for remaining != "" {
		remaining = strings.TrimSpace(remaining)
		if remaining == "" {
			break
		}

		if strings.HasPrefix(remaining, "{") {
			// Find matching closing brace
			depth := 0
			endIdx := -1
			for i, ch := range remaining {
				if ch == '{' {
					depth++
				} else if ch == '}' {
					depth--
					if depth == 0 {
						endIdx = i
						break
					}
				}
			}
			if endIdx > 0 {
				// Extract content between braces (excluding the braces themselves)
				results = append(results, remaining[1:endIdx])
				remaining = remaining[endIdx+1:]
			} else {
				// Unmatched brace, treat rest as one arg
				results = append(results, remaining)
				break
			}
		} else {
			// No brace, take until next space or brace
			nextSpace := strings.IndexAny(remaining, " \t{")
			if nextSpace == -1 {
				results = append(results, remaining)
				break
			} else if remaining[nextSpace] == '{' {
				// Found a brace, take everything before it
				if nextSpace > 0 {
					results = append(results, remaining[:nextSpace])
				}
				remaining = remaining[nextSpace:]
			} else {
				results = append(results, remaining[:nextSpace])
				remaining = remaining[nextSpace+1:]
			}
		}
	}
	return results, nil
}

// SplitCommands splits a command string by semicolons, respecting quoted strings.
// Example: "kill rat; say hello; smile" -> ["kill rat", "say hello", "smile"]
func SplitCommands(text string) []string {
	var results []string
	var current strings.Builder
	inQuote := false
	quoteChar := rune(0)

	for _, ch := range text {
		switch {
		case ch == '"' || ch == '\'':
			if !inQuote {
				inQuote = true
				quoteChar = ch
			} else if ch == quoteChar {
				inQuote = false
			}
			current.WriteRune(ch)
		case ch == ';' && !inQuote:
			cmd := strings.TrimSpace(current.String())
			if cmd != "" {
				results = append(results, cmd)
			}
			current.Reset()
		default:
			current.WriteRune(ch)
		}
	}

	// Add the last command
	cmd := strings.TrimSpace(current.String())
	if cmd != "" {
		results = append(results, cmd)
	}

	return results
}
