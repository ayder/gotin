package network

import (
	"golang.org/x/text/encoding/charmap"
	"unicode/utf8"
)

// Decoder handles byte-to-string conversion for MUD server data.
// MUDs often use ISO-8859-1 encoding, so we provide fallback handling.
type Decoder struct {
	iso8859Decoder *charmap.Charmap
	leftover       []byte // leftover bytes from incomplete UTF-8 sequences
}

// NewDecoder creates a new Decoder configured for MUD text.
func NewDecoder() *Decoder {
	return &Decoder{
		iso8859Decoder: charmap.ISO8859_1,
	}
}

// Decode converts raw bytes to a string, handling encoding errors gracefully.
// It first attempts to interpret the bytes as UTF-8. If that fails,
// it falls back to ISO-8859-1 decoding. Invalid characters are replaced
// with the Unicode replacement character (U+FFFD).
func (d *Decoder) Decode(data []byte) string {
	if len(d.leftover) > 0 {
		data = append(d.leftover, data...)
		d.leftover = nil
	}

	if len(data) == 0 {
		return ""
	}

	// If it's valid UTF-8, return it directly
	if utf8.Valid(data) {
		return string(data)
	}

	// Check if it ends with a partial UTF-8 sequence
	if i := findIncompleteUTF8(data); i >= 0 {
		d.leftover = data[i:]
		data = data[:i]
	}

	// Re-check validity after removing potential incomplete tail
	if len(data) > 0 && utf8.Valid(data) {
		return string(data)
	}

	// Otherwise, decode as ISO-8859-1
	decoded, err := d.iso8859Decoder.NewDecoder().Bytes(data)
	if err != nil {
		// Fallback: replace invalid bytes with replacement character
		return replaceInvalidBytes(data)
	}
	return string(decoded)
}

// findIncompleteUTF8 returns the index of the start of an incomplete UTF-8
// sequence at the end of the data, or -1 if none is found.
func findIncompleteUTF8(data []byte) int {
	// A UTF-8 sequence can be up to 4 bytes.
	// Look back up to 3 bytes from the end.
	for i := 1; i <= 3 && len(data)-i >= 0; i++ {
		idx := len(data) - i
		b := data[idx]
		if b&0xC0 == 0x80 {
			// Continuation byte, keep looking back
			continue
		}
		if b&0x80 == 0 {
			// Single-byte ASCII, no incomplete sequence here
			return -1
		}
		// Found a start byte. Check if the sequence is complete.
		expected := 0
		if b&0xE0 == 0xC0 {
			expected = 2
		} else if b&0xF0 == 0xE0 {
			expected = 3
		} else if b&0xF8 == 0xF0 {
			expected = 4
		}
		if expected > i {
			return idx
		}
		return -1
	}
	return -1
}

// replaceInvalidBytes converts bytes to string, replacing invalid UTF-8
// sequences with the Unicode replacement character.
func replaceInvalidBytes(data []byte) string {
	result := make([]rune, 0, len(data))
	for len(data) > 0 {
		r, size := utf8.DecodeRune(data)
		if r == utf8.RuneError && size == 1 {
			// Invalid byte, replace with replacement character
			result = append(result, '�')
			data = data[1:]
		} else {
			result = append(result, r)
			data = data[size:]
		}
	}
	return string(result)
}
