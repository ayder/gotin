package network

import "bytes"

// GMCPCallback is invoked for each GMCP subnegotiation received.
// pkg is the package name (e.g. "Room.Info", "Char.Vitals").
// payload is the raw (already IAC-unescaped) bytes after the first space —
// typically UTF-8 JSON but this layer does not parse it.
type GMCPCallback func(pkg string, payload []byte)

// splitGMCP parses a GMCP subnegotiation body into (package, payload).
// Body layout: "<package name><SP><json payload>"; if no space, payload is empty.
func splitGMCP(body []byte) (pkg string, payload []byte) {
	if i := bytes.IndexByte(body, ' '); i >= 0 {
		return string(body[:i]), body[i+1:]
	}
	return string(body), nil
}
