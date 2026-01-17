package logic

import (
	"testing"
)

func TestLineBuffer_Feed(t *testing.T) {
	tests := []struct {
		name           string
		inputs         []string
		expectedLines  [][]string
		expectedBuffer string
	}{
		{
			name:           "single complete line with LF",
			inputs:         []string{"Hello World\n"},
			expectedLines:  [][]string{{"Hello World"}},
			expectedBuffer: "",
		},
		{
			name:           "single complete line with CRLF",
			inputs:         []string{"Hello World\r\n"},
			expectedLines:  [][]string{{"Hello World"}},
			expectedBuffer: "",
		},
		{
			name:           "multiple lines in one feed",
			inputs:         []string{"Line 1\nLine 2\nLine 3\n"},
			expectedLines:  [][]string{{"Line 1", "Line 2", "Line 3"}},
			expectedBuffer: "",
		},
		{
			name:           "partial line buffered",
			inputs:         []string{"Hello"},
			expectedLines:  [][]string{nil},
			expectedBuffer: "Hello",
		},
		{
			name:           "fragmented line across feeds",
			inputs:         []string{"Hello ", "World\n"},
			expectedLines:  [][]string{nil, {"Hello World"}},
			expectedBuffer: "",
		},
		{
			name:           "multiple fragments",
			inputs:         []string{"The gob", "lin hits ", "you!\n"},
			expectedLines:  [][]string{nil, nil, {"The goblin hits you!"}},
			expectedBuffer: "",
		},
		{
			name:           "line with trailing incomplete",
			inputs:         []string{"Complete line\nIncomp"},
			expectedLines:  [][]string{{"Complete line"}},
			expectedBuffer: "Incomp",
		},
		{
			name:           "empty input",
			inputs:         []string{""},
			expectedLines:  [][]string{nil},
			expectedBuffer: "",
		},
		{
			name:           "multiple complete lines with CRLF",
			inputs:         []string{"Line 1\r\nLine 2\r\n"},
			expectedLines:  [][]string{{"Line 1", "Line 2"}},
			expectedBuffer: "",
		},
		{
			name:           "mixed line endings",
			inputs:         []string{"Unix line\nWindows line\r\nAnother\n"},
			expectedLines:  [][]string{{"Unix line", "Windows line", "Another"}},
			expectedBuffer: "",
		},
		{
			name:           "empty lines",
			inputs:         []string{"\n\n\n"},
			expectedLines:  [][]string{{"", "", ""}},
			expectedBuffer: "",
		},
		{
			name:           "realistic MUD fragmentation",
			inputs:         []string{"You are in a dark room.\r\nYou see a goblin", ".\r\nWhat will you do?\r\n"},
			expectedLines:  [][]string{{"You are in a dark room."}, {"You see a goblin.", "What will you do?"}},
			expectedBuffer: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lb := NewLineBuffer()

			for i, input := range tt.inputs {
				lines := lb.Feed([]byte(input))

				// Check expected lines for this feed
				if i < len(tt.expectedLines) {
					expected := tt.expectedLines[i]
					if len(lines) != len(expected) {
						t.Errorf("Feed #%d: got %d lines, want %d lines. Got: %v, Want: %v",
							i, len(lines), len(expected), lines, expected)
						continue
					}
					for j, line := range lines {
						if line != expected[j] {
							t.Errorf("Feed #%d, line %d: got %q, want %q",
								i, j, line, expected[j])
						}
					}
				}
			}

			// Check final buffer state
			remaining := lb.Flush()
			if remaining != tt.expectedBuffer {
				t.Errorf("Final buffer: got %q, want %q", remaining, tt.expectedBuffer)
			}
		})
	}
}

func TestLineBuffer_Flush(t *testing.T) {
	lb := NewLineBuffer()

	// Feed partial data
	lb.Feed([]byte("Incomplete line"))

	// Flush should return the incomplete data
	result := lb.Flush()
	if result != "Incomplete line" {
		t.Errorf("Flush: got %q, want %q", result, "Incomplete line")
	}

	// Buffer should be empty after flush
	result = lb.Flush()
	if result != "" {
		t.Errorf("Second Flush: got %q, want empty string", result)
	}
}

func TestLineBuffer_EmptyFlush(t *testing.T) {
	lb := NewLineBuffer()

	// Flush empty buffer
	result := lb.Flush()
	if result != "" {
		t.Errorf("Flush empty: got %q, want empty string", result)
	}
}
