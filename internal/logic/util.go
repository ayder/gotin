package logic

import "regexp"

// ansiRegex matches ANSI escape sequences:
//   - CSI sequences: ESC [ params [A-Za-z]  (colors, cursor, erase, etc.)
//   - OSC sequences: ESC ] ... BEL or ESC \ (title, hyperlink, etc.)
//   - Two-char sequences: ESC followed by a single char in the C1 range.
var ansiRegex = regexp.MustCompile(`\x1b\[[0-9;:?]*[A-Za-z]|\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)|\x1b[NOcEFGHMNOPVWXYZ^_\\]`)

// StripANSI removes ANSI escape sequences from the given string.
func StripANSI(text string) string {
	return ansiRegex.ReplaceAllString(text, "")
}
