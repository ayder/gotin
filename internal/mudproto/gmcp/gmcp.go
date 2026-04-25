// Package gmcp implements GMCP (Generic Mud Communication Protocol) as a
// network.Protocol. Subnegotiation bodies are of the form
// "<package name> <json payload>" and are forwarded to an optional app-level
// callback.
package gmcp

import (
	"bytes"

	"github.com/ayder/gotin/internal/network"
)

// Callback receives parsed GMCP messages.
// pkg is the package name (e.g. "Room.Info", "Char.Vitals").
// payload is the raw (IAC-unescaped) bytes after the first space — typically
// UTF-8 JSON; this layer does not parse it.
type Callback func(pkg string, payload []byte)

// Protocol is the GMCP Protocol implementation.
type Protocol struct {
	cb      Callback
	dataRx  bool // set to true when first subnegotiation arrives
}

// New returns a new GMCP Protocol. cb may be nil.
func New(cb Callback) *Protocol { return &Protocol{cb: cb} }

// SetCallback replaces the GMCP callback at runtime.
func (p *Protocol) SetCallback(cb Callback) { p.cb = cb }

// IsDataActive implements network.DataActiveProtocol. GMCP only appears in
// the active-protocol list after the server has sent at least one GMCP
// subnegotiation, preventing ghost entries when the server negotiates the
// option but doesn't implement any packages.
func (p *Protocol) IsDataActive() bool { return p.dataRx }

func (*Protocol) Name() string       { return "GMCP" }
func (*Protocol) Option() byte       { return network.GMCP }
func (*Protocol) Kind() network.Kind { return network.KindHim }

func (*Protocol) OnEnable(ctx network.Context) error {
	ctx.Debug("GMCP: enabled, sending Core.Hello + Core.Supports")
	// Most GMCP servers require a Core.Hello handshake before they send data.
	hello := []byte(`Core.Hello {"client":"gotin","version":"1.0"}`)
	if err := ctx.SendSubneg(network.GMCP, hello); err != nil {
		ctx.Debug("GMCP: Core.Hello send error: %v", err)
	}
	// Tell the server which packages we want.
	supports := []byte(`Core.Supports.Set ["Room.Info 1", "Char.Vitals 1", "Comm.Channel 1"]`)
	if err := ctx.SendSubneg(network.GMCP, supports); err != nil {
		ctx.Debug("GMCP: Core.Supports send error: %v", err)
	}
	return nil
}

func (*Protocol) OnDisable(ctx network.Context) {
	ctx.Debug("GMCP: disabled")
}

func (p *Protocol) OnSubnegotiation(ctx network.Context, data []byte) error {
	firstRx := !p.dataRx
	p.dataRx = true
	if firstRx {
		ctx.NotifyProtocolStatus()
	}
	pkg, payload := split(data)
	ctx.Debug("GMCP: recv pkg=%q payload=%q", pkg, string(payload))
	if p.cb == nil {
		ctx.Debug("GMCP: no callback installed, dropping")
		return nil
	}
	p.cb(pkg, payload)
	return nil
}

// split parses a GMCP subnegotiation body into (package, payload).
// Body layout: "<package name><SP><json payload>"; if no space, payload is nil.
func split(body []byte) (pkg string, payload []byte) {
	if i := bytes.IndexByte(body, ' '); i >= 0 {
		return string(body[:i]), body[i+1:]
	}
	return string(body), nil
}
