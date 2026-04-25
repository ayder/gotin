package network

import (
	"fmt"
	"io"
	"sort"
)

// Kind reports which side(s) of a Telnet option the Protocol participates in.
// A Protocol may accept server-performed (Him), client-performed (Us), or
// both — MXP is client-performed (server sends IAC DO MXP, client replies
// IAC WILL MXP) while MCCP2 and GMCP are server-performed.
type Kind uint8

const (
	// KindHim — the server performs the option; client sees IAC WILL and
	// replies IAC DO. Examples: MCCP2, GMCP, MSDP, MSSP.
	KindHim Kind = 1 << iota
	// KindUs — the client performs the option; client sees IAC DO from the
	// server and replies IAC WILL. Examples: MXP, NAWS, TTYPE.
	KindUs
)

// Protocol is implemented by each MUD subprotocol (GMCP, MCCP2, MXP, ...).
// One Protocol handles one Telnet option byte.
type Protocol interface {
	// Name is the identifier used in config ("MCCP2") and the /proto command.
	Name() string
	// Option is the Telnet option byte this protocol owns (e.g. MCCP2 = 86).
	Option() byte
	// Kind declares which side(s) of the option this Protocol handles.
	Kind() Kind
	// OnEnable fires after the relevant Q Method state (Him for KindHim,
	// Us for KindUs) flips to YES.
	OnEnable(ctx Context) error
	// OnDisable fires after the state flips back to NO.
	OnDisable(ctx Context)
	// OnSubnegotiation is called with the IAC-unescaped body of each
	// IAC SB <option> ... IAC SE block for this protocol's option.
	OnSubnegotiation(ctx Context, data []byte) error
}

// AlwaysActiveProtocol is an optional interface that Protocols can implement
// to indicate they should appear in ActiveProtocols() whenever installed,
// regardless of Telnet Q-method negotiation state. This is useful for
// protocols like MXP that can operate inline without full option negotiation.
type AlwaysActiveProtocol interface {
	Protocol
	AlwaysActive() bool
}

// DataActiveProtocol is an optional interface that Protocols can implement
// to indicate they should only appear active after receiving actual data.
// This prevents protocols like GMCP from cluttering the status bar when the
// server negotiates the option but never sends any subnegotiations.
type DataActiveProtocol interface {
	Protocol
	IsDataActive() bool
}

// Context is the narrow facade a Protocol uses to act on the Client without
// importing Client internals. One Context is bound to one Client.
type Context interface {
	// Send writes raw bytes to the server. Caller is responsible for
	// Telnet IAC escaping.
	Send(raw []byte) error
	// SendSubneg writes IAC SB <option> <escaped body> IAC SE.
	SendSubneg(option byte, body []byte) error
	// SwapReader replaces the inbound reader. The transform is applied to
	// the current reader (including any unread bytes from the current
	// buffer) and the result becomes the new reader. Intended for
	// stream-modifying protocols such as MCCP2.
	SwapReader(transform func(io.Reader) io.Reader)
	// Debug emits a debug line when the Client has debug enabled.
	Debug(format string, args ...any)
	// OptionActive reports whether the server-side Q Method state for opt
	// is YES.
	OptionActive(opt byte) bool
	// NotifyProtocolStatus refreshes the active-protocol status callback.
	// Protocols that implement DataActiveProtocol should call this when
	// their data-active state changes.
	NotifyProtocolStatus()
}

// clientContext is the concrete Context implementation backed by *Client.
type clientContext struct {
	c *Client
}

func (cc *clientContext) Send(raw []byte) error {
	return cc.c.Send(raw)
}

func (cc *clientContext) SendSubneg(option byte, body []byte) error {
	escaped := cc.c.escapeIAC(body)
	frame := make([]byte, 0, 5+len(escaped))
	frame = append(frame, IAC, SB, option)
	frame = append(frame, escaped...)
	frame = append(frame, IAC, SE)
	return cc.c.Send(frame)
}

func (cc *clientContext) SwapReader(transform func(io.Reader) io.Reader) {
	cc.c.pendingSwap = transform
}

func (cc *clientContext) Debug(format string, args ...any) {
	cc.c.debugf(format, args...)
}

func (cc *clientContext) OptionActive(opt byte) bool {
	if cc.c.options == nil {
		return false
	}
	return cc.c.options.getHim(opt) == optYes
}

func (cc *clientContext) NotifyProtocolStatus() {
	cc.c.notifyProtocolStatus()
}

// Install registers a Protocol and offers negotiation to the server based on
// the Protocol's Kind: IAC DO for KindHim, IAC WILL for KindUs (both if the
// Protocol declares both). If a Protocol is already installed for that
// option byte it is replaced (OnDisable is called on the previous one).
func (c *Client) Install(p Protocol) error {
	if p == nil {
		return fmt.Errorf("Install: protocol is nil")
	}
	if c.protocols == nil {
		c.protocols = make(map[byte]Protocol)
	}
	opt := p.Option()
	kind := p.Kind()
	if prev, ok := c.protocols[opt]; ok {
		prev.OnDisable(c.ctx())
	}
	c.protocols[opt] = p
	alreadyActive := false
	if c.options != nil {
		if kind&KindHim != 0 && c.options.getHim(opt) == optYes {
			alreadyActive = true
		}
		if kind&KindUs != 0 && c.options.getUs(opt) == optYes {
			alreadyActive = true
		}
	}
	if alreadyActive {
		if err := p.OnEnable(c.ctx()); err != nil {
			return err
		}
	}

	// Always notify so the UI reflects protocols that implement
	// AlwaysActiveProtocol immediately, even before Telnet negotiation completes.
	c.notifyProtocolStatus()

	if alreadyActive || c.conn == nil {
		return nil
	}
	var offer []byte
	if kind&KindHim != 0 {
		offer = append(offer, IAC, DO, opt)
	}
	if kind&KindUs != 0 {
		offer = append(offer, IAC, WILL, opt)
	}
	if len(offer) > 0 {
		if err := c.Send(offer); err != nil {
			return err
		}
	}
	return nil
}

// Uninstall removes a Protocol by name. Sends IAC DONT / IAC WONT per its
// Kind if the corresponding side is currently active. Returns nil if no such
// protocol is installed.
func (c *Client) Uninstall(name string) error {
	for opt, p := range c.protocols {
		if p.Name() != name {
			continue
		}
		delete(c.protocols, opt)
		if c.conn != nil && c.options != nil {
			var bye []byte
			kind := p.Kind()
			if kind&KindHim != 0 && c.options.getHim(opt) == optYes {
				bye = append(bye, IAC, DONT, opt)
			}
			if kind&KindUs != 0 && c.options.getUs(opt) == optYes {
				bye = append(bye, IAC, WONT, opt)
			}
			if len(bye) > 0 {
				_ = c.Send(bye)
			}
		}
		p.OnDisable(c.ctx())
		c.notifyProtocolStatus()
		return nil
	}
	return nil
}

// Installed returns all installed protocols in no particular order.
func (c *Client) Installed() []Protocol {
	out := make([]Protocol, 0, len(c.protocols))
	for _, p := range c.protocols {
		out = append(out, p)
	}
	return out
}

// ActiveProtocols returns the names of installed protocols whose Q Method
// state is currently YES on the side(s) declared by the Protocol's Kind.
// Protocols implementing AlwaysActiveProtocol are included whenever installed.
// Protocols implementing DataActiveProtocol are skipped unless IsDataActive()
// returns true. The return value is sorted for stable display.
func (c *Client) ActiveProtocols() []string {
	if c.options == nil {
		return nil
	}
	var names []string
	for opt, p := range c.protocols {
		// If the protocol implements DataActiveProtocol, only show it after
		// it has actually received data.
		if dap, ok := p.(DataActiveProtocol); ok && !dap.IsDataActive() {
			continue
		}

		k := p.Kind()
		active := false
		if k&KindHim != 0 && c.options.getHim(opt) == optYes {
			active = true
		}
		if k&KindUs != 0 && c.options.getUs(opt) == optYes {
			active = true
		}
		if active {
			names = append(names, p.Name())
		} else if aap, ok := p.(AlwaysActiveProtocol); ok && aap.AlwaysActive() {
			names = append(names, p.Name())
		}
	}
	sort.Strings(names)
	return names
}

// SetProtocolStatusCallback registers a handler invoked whenever the set of
// currently-negotiated protocols may have changed. The callback receives the
// current sorted list of active protocol names.
func (c *Client) SetProtocolStatusCallback(cb func(active []string)) {
	c.protocolStatusCb = cb
}

// notifyProtocolStatus fires the registered callback (if any) with the
// current ActiveProtocols() snapshot.
func (c *Client) notifyProtocolStatus() {
	if c.protocolStatusCb != nil {
		c.protocolStatusCb(c.ActiveProtocols())
	}
}

// ctx returns a fresh Context bound to this client.
func (c *Client) ctx() Context {
	return &clientContext{c: c}
}

// Context returns a Context bound to this client.
func (c *Client) Context() Context {
	return c.ctx()
}
