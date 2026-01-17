package logic

import "bytes"

// LineBuffer accumulates incoming data and splits it into complete lines.
// It handles fragmented packets by keeping incomplete line data until
// the next Feed call provides the rest.
type LineBuffer struct {
	buffer bytes.Buffer
}

// NewLineBuffer creates a new LineBuffer instance.
func NewLineBuffer() *LineBuffer {
	return &LineBuffer{}
}

// Feed appends data to the buffer and returns any complete lines.
// Complete lines are those ending with \n or \r\n.
// Any incomplete suffix (data after the last newline) remains in the buffer.
func (lb *LineBuffer) Feed(data []byte) []string {
	lb.buffer.Write(data)

	var lines []string
	for {
		// Look for newline in current buffer
		content := lb.buffer.Bytes()
		idx := bytes.IndexByte(content, '\n')
		if idx == -1 {
			// No complete line yet
			break
		}

		// Extract the line (excluding the \n)
		line := content[:idx]

		// Handle \r\n by stripping trailing \r
		if len(line) > 0 && line[len(line)-1] == '\r' {
			line = line[:len(line)-1]
		}

		lines = append(lines, string(line))

		// Remove the processed line (including the \n) from buffer
		remaining := content[idx+1:]
		lb.buffer.Reset()
		lb.buffer.Write(remaining)
	}

	return lines
}

// Flush returns any remaining data in the buffer as a final line.
// This is useful when the connection closes to get any trailing data.
func (lb *LineBuffer) Flush() string {
	if lb.buffer.Len() == 0 {
		return ""
	}
	line := lb.buffer.String()
	lb.buffer.Reset()
	return line
}

// GetPending returns the current content of the incomplete line buffer.
// This allow peeking at data (like prompts) without consuming it.
func (lb *LineBuffer) GetPending() string {
	return lb.buffer.String()
}
