// Package mccp2 implements the MUD Client Compression Protocol v2 as a
// network.Protocol. After negotiation, the server sends an empty
// IAC SB MCCP2 IAC SE subnegotiation that marks the start of a zlib-compressed
// byte stream — this protocol wraps the Client's reader with zlib.NewReader
// via ctx.SwapReader.
package mccp2

import (
	"compress/zlib"
	"io"

	"github.com/ayder/gotin/internal/network"
)

// Protocol is the MCCP2 Protocol implementation.
type Protocol struct {
	active bool // true once compression has been activated; ignores duplicates
}

// New returns a new MCCP2 Protocol.
func New() *Protocol { return &Protocol{} }

func (*Protocol) Name() string       { return "MCCP2" }
func (*Protocol) Option() byte       { return network.MCCP2 }
func (*Protocol) Kind() network.Kind { return network.KindHim }
func (p *Protocol) OnEnable(ctx network.Context) error {
	p.active = false
	ctx.Debug("MCCP2: accepted; awaiting IAC SB MCCP2 IAC SE to start compression")
	return nil
}

// OnDisable is a no-op: zlib state cannot be unwound mid-stream; in practice
// the server closes the connection when it disables MCCP2.
func (p *Protocol) OnDisable(ctx network.Context) {
	p.active = false
	ctx.Debug("MCCP2: disabled")
}

// errReader is an io.Reader that always returns the stored error.
type errReader struct{ err error }

func (e *errReader) Read(_ []byte) (int, error) { return 0, e.err }

// sniffReader wraps an io.Reader to capture the first bytes for debugging.
// This is invaluable when diagnosing "zlib: invalid header" — it shows exactly
// what bytes zlib.NewReader sees.
type sniffReader struct {
	r   io.Reader
	ctx network.Context
	buf []byte
}

func (s *sniffReader) Read(p []byte) (int, error) {
	n, err := s.r.Read(p)
	if n > 0 && len(s.buf) < 64 {
		s.buf = append(s.buf, p[:n]...)
		if len(s.buf) >= 64 {
			s.ctx.Debug("MCCP2: first compressed bytes (hex): %x", s.buf)
		}
	}
	if err != nil && len(s.buf) > 0 && len(s.buf) < 64 {
		s.ctx.Debug("MCCP2: first compressed bytes (hex, %d bytes before error): %x", len(s.buf), s.buf)
	}
	return n, err
}

// OnSubnegotiation is the activation trigger: the body is empty and simply
// signals that subsequent bytes on the socket are zlib-compressed.
func (p *Protocol) OnSubnegotiation(ctx network.Context, data []byte) error {
	if p.active {
		ctx.Debug("MCCP2: OnSubnegotiation received while already active; ignoring duplicate")
		return nil
	}
	ctx.Debug("MCCP2: OnSubnegotiation received, body len=%d data=%q", len(data), data)
	ctx.SwapReader(func(prev io.Reader) io.Reader {
		sniff := &sniffReader{r: prev, ctx: ctx}
		zr, err := zlib.NewReader(sniff)
		if err != nil {
			ctx.Debug("MCCP2: zlib.NewReader failed: %v", err)
			// Returning prev would stream raw compressed bytes to the screen
			// as binary garbage. Return an error reader so ReadLoop exits
			// cleanly instead.
			return &errReader{err: err}
		}
		ctx.Debug("MCCP2: zlib reader created successfully")
		return zr
	})
	p.active = true
	ctx.Debug("MCCP2: compression active (reader swap requested)")
	return nil
}
