package logic

import "regexp"

var ansiRegex = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// StripANSI removes ANSI color codes from the given string.
func StripANSI(text string) string {
	return ansiRegex.ReplaceAllString(text, "")
}
