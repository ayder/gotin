// Package protolog emits structured JSON Lines diagnostic events for MUD
// protocol traffic.
package protolog

import (
	"encoding/hex"
	"encoding/json"
	"io"
	"strconv"
	"sync"
	"time"
)

// Entry is one emitted log record.
type Entry struct {
	TS     string         `json:"ts"`
	Source string         `json:"source"`
	Dir    string         `json:"dir"`
	Event  string         `json:"event"`
	UTF8   string         `json:"utf8,omitempty"`
	Hex    string         `json:"hex,omitempty"`
	Parsed map[string]any `json:"parsed,omitempty"`
}

// Logger emits entries and reports whether logging is currently enabled.
type Logger interface {
	Enabled() bool
	Log(e Entry)
}

// JSONLinesLogger writes one JSON object per line to an io.Writer.
type JSONLinesLogger struct {
	w       io.Writer
	enabled func() bool
	mu      sync.Mutex
}

// NewJSONLinesLogger constructs a logger writing to w. If enabled is nil,
// logging is always enabled.
func NewJSONLinesLogger(w io.Writer, enabled func() bool) *JSONLinesLogger {
	if enabled == nil {
		enabled = func() bool { return true }
	}
	return &JSONLinesLogger{w: w, enabled: enabled}
}

// Enabled reports whether this logger should emit now.
func (l *JSONLinesLogger) Enabled() bool {
	return l != nil && l.w != nil && l.enabled()
}

// Log emits one entry, filling TS when it is empty.
func (l *JSONLinesLogger) Log(e Entry) {
	if !l.Enabled() {
		return
	}
	if e.TS == "" {
		e.TS = time.Now().UTC().Format(time.RFC3339Nano)
	}
	blob, err := json.Marshal(e)
	if err != nil {
		return
	}
	blob = append(blob, '\n')
	l.mu.Lock()
	defer l.mu.Unlock()
	_, _ = l.w.Write(blob)
}

// EncodeHex returns the lowercase hex encoding of b.
func EncodeHex(b []byte) string { return hex.EncodeToString(b) }

// EncodeUTF8 returns the Go-quoted representation of b.
func EncodeUTF8(b []byte) string { return strconv.Quote(string(b)) }
