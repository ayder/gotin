package network

import (
	"golang.org/x/text/encoding/charmap"
	"unicode/utf8"
)

// Decoder handles byte-to-string conversion for MUD server data.
// MUDs often use ISO-8859-1 encoding, so we provide fallback handling.
type Decoder struct {
	iso8859Decoder *charmap.Charmap
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
	// If it's valid UTF-8, return it directly
	if utf8.Valid(data) {
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
