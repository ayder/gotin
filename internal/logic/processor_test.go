package logic

import (
	"bytes"
	"testing"
)

func TestProcessIncoming(t *testing.T) {
	testCases := []struct {
		name     string
		input    []byte
		expected []byte
	}{
		{
			name:     "pass-through data",
			input:    []byte("Hello World"),
			expected: []byte("Hello World"),
		},
		{
			name:     "empty data",
			input:    []byte{},
			expected: []byte{},
		},
		{
			name:     "binary data",
			input:    []byte{0x01, 0x02, 0x03},
			expected: []byte{0x01, 0x02, 0x03},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := ProcessIncoming(tc.input)
			if !bytes.Equal(result, tc.expected) {
				t.Errorf("ProcessIncoming(%v) = %v, want %v", tc.input, result, tc.expected)
			}
		})
	}
}
