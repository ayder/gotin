// Package mxp implements a v1 MUD eXtension Protocol (option 91) client.
// This pass covers negotiation + tag stripping + MXP mode-switch handling on
// the decoded text stream, plus the <VERSION>/<SUPPORT> probe-response
// handshake required by servers like t2tmud.org. Rich rendering (clickable
// links, element definitions, per-element attribute parsing) is deferred.
//
// Mode semantics follow the Zuggsoft MXP spec as summarized on
// mudstandards.org/mud/mxp:
//
//	ESC [ 0 z   Open line       (temporary; reverts on newline)
//	ESC [ 1 z   Secure line     (temporary)
//	ESC [ 2 z   Locked line     (temporary)
//	ESC [ 3 z   Reset           (mode := defaultMode)
//	ESC [ 4 z   Temp-secure     (next tag only)
//	ESC [ 5 z   Lock-open       (defaultMode := Open)
//	ESC [ 6 z   Lock-secure     (defaultMode := Secure)
//	ESC [ 7 z   Lock-locked     (defaultMode := Locked)
//	ESC [ N z   User line tag   (10 <= N <= 99; consumed, treated as Open)
//
// On a newline, mode reverts to defaultMode.
//
// Probe responses (per Zuggsoft): when the server sends a bare <VERSION> or
// <SUPPORT> tag, the client replies on a SECURE line (ESC [ 1 z prefix,
// trailing newline) with its own <VERSION …>/<SUPPORTS …> tag.
package mxp

import (
	"strings"

	"github.com/ayder/gotin/internal/mudproto/protolog"
	"github.com/ayder/gotin/internal/network"
)

// ClientName and ClientVersion appear in the <VERSION …> response sent to
// the server on a bare <VERSION> probe.
const (
	ClientName    = "gotin"
	ClientVersion = "0.1"
	MXPVersion    = "0.4"
)

// Mode is the MXP security mode.
type Mode int

const (
	Open Mode = iota
	Secure
	Locked
)

// Sink receives interleaved text segments and tag observations from Filter()
// in arrival order. When OnTag fires, all preceding stripped text has already
// been delivered via OnText.
type Sink interface {
	OnText(s string)
	OnTag(name, body string)
}

// Protocol is the MXP Protocol implementation.
type Protocol struct {
	mode        Mode
	defaultMode Mode
	ctx         network.Context // stashed on OnEnable; used to send probe responses
	onRoomName  func(name string)
	sink        Sink
	onReset     func()
	log         protolog.Logger
	pending     string // partial tag carried over from the end of the previous chunk
}

// New returns a new MXP Protocol in Open mode with Open as the default.
func New() *Protocol { return &Protocol{mode: Open, defaultMode: Open} }

// SetRoomNameCallback registers a handler invoked whenever an MXP
// <ROOMNAME>…</ROOMNAME> tag pair is seen in the text stream.
func (p *Protocol) SetRoomNameCallback(cb func(name string)) {
	p.onRoomName = cb
}

// SetSink installs a Sink that receives interleaved text segments and tag
// observations. Nil disables sink delivery. Filter's string return value is
// unaffected.
func (p *Protocol) SetSink(s Sink) { p.sink = s }

// SetSentinelResetCallback registers a callback fired when MXP is disabled.
// This lets higher layers clear per-connection mapper sentinels without making
// the protocol package depend on mapper.
func (p *Protocol) SetSentinelResetCallback(cb func()) { p.onReset = cb }

// SetLogger installs a structured logger; nil disables logging.
func (p *Protocol) SetLogger(l protolog.Logger) { p.log = l }

// Mode reports the current security mode.
func (p *Protocol) Mode() Mode { return p.mode }

// DefaultMode reports the persistent default mode (set by ESC [ 5-7 z).
func (p *Protocol) DefaultMode() Mode { return p.defaultMode }

// SetContext stashes a network Context so probe responses can be sent even
// when Telnet option 91 negotiation hasn't completed. Idempotent: only sets
// if currently nil.
func (p *Protocol) SetContext(ctx network.Context) {
	if p.ctx == nil {
		p.ctx = ctx
	}
}

func (*Protocol) Name() string { return "MXP" }
func (*Protocol) Option() byte { return network.MXP }

// Kind: MXP is accepted in either direction. The Zuggsoft MXP spec and most
// MUD servers (e.g. t2tmud) initiate with IAC WILL MXP (server performs MXP),
// but some servers use IAC DO MXP (ask the client to perform). Declaring both
// sides lets the client accept whichever the server sends.
func (*Protocol) Kind() network.Kind { return network.KindHim | network.KindUs }

// AlwaysActive returns true so MXP appears in the client's active protocol
// list as soon as it is installed. MXP can operate inline (tag stripping and
// probe responses) even when the server has not completed Telnet option 91
// negotiation.
func (*Protocol) AlwaysActive() bool { return true }

func (p *Protocol) OnEnable(ctx network.Context) error {
	p.ctx = ctx
	ctx.Debug("MXP: enabled (default=Open)")
	return nil
}

func (p *Protocol) OnDisable(ctx network.Context) {
	ctx.Debug("MXP: disabled")
	p.ctx = nil
	if p.onReset != nil {
		p.onReset()
	}
}

// OnSubnegotiation stubs subneg handling for v1. MXP primarily uses inline
// tags in the text stream rather than telnet subnegotiations, so this path
// is rarely exercised in practice.
func (*Protocol) OnSubnegotiation(ctx network.Context, data []byte) error {
	if len(data) > 0 {
		ctx.Debug("MXP: subneg len=%d (ignored in v1)", len(data))
	}
	return nil
}

// Filter strips MXP tags and mode-switch ESC sequences from a chunk of
// decoded text, while tracking security-mode state across calls and
// responding to <VERSION>/<SUPPORT> probes from the server.
//
// Filter mutates protocol mode state and must be called from a single
// goroutine; it is not safe for concurrent use.
//
// It is deliberately conservative: anything that does not look like a
// complete MXP tag (no matching '>' in this chunk) is passed through so a
// split tag can be completed on the next call.
func (p *Protocol) Filter(s string) string {
	if p.pending != "" {
		s = p.pending + s
		p.pending = ""
	}
	if !strings.ContainsAny(s, "<\x1b\n") {
		if p.sink != nil && s != "" {
			p.sink.OnText(s)
		}
		return s
	}
	logOn := p.logEnabled()
	if logOn && strings.ContainsAny(s, "<\x1b") {
		p.logEvent("chunk", []byte(s), nil)
	}
	var b strings.Builder
	b.Grow(len(s))
	lastFlush := 0
	flush := func() {
		if p.sink == nil {
			return
		}
		if b.Len() > lastFlush {
			p.sink.OnText(b.String()[lastFlush:])
			lastFlush = b.Len()
		}
	}
	for i := 0; i < len(s); {
		ch := s[i]
		switch ch {
		case '<':
			if end := findTagEnd(s[i:]); end >= 0 {
				body := s[i+1 : i+end]
				p.handleProbe(body)
				p.handleRoomName(s, i+1, i+end, &b, logOn)
				if logOn {
					p.logEvent("tag", []byte(s[i:i+end+1]), map[string]any{"name": extractTagName(body), "body": body})
				}
				if p.sink != nil {
					flush()
					p.sink.OnTag(extractTagName(body), body)
				}
				i += end + 1
				continue
			}
			// Tag is split across chunks (or has an unclosed quote). Buffer
			// the remainder so the next Filter call can complete it; flush
			// what we've consumed so far.
			p.pending = s[i:]
			flush()
			return b.String()
		case '\x1b':
			if n, num, ok := parseModeEsc(s[i:]); ok {
				p.applyMode(num)
				i += n
				continue
			}
			b.WriteByte(ch)
			i++
		case '\n':
			b.WriteByte(ch)
			p.mode = p.defaultMode
			i++
		default:
			b.WriteByte(ch)
			i++
		}
	}
	flush()
	return b.String()
}

// handleRoomName checks whether the tag at the given position is a <ROOMNAME>
// open tag and, if so, looks ahead for the matching </ROOMNAME> close tag.
// When found, the room name text is extracted and the onRoomName callback is
// fired.  The tag pair is consumed (not written to b).  If the close tag is
// not present in this chunk, the open tag alone is consumed and the name is
// lost; this is acceptable for v1 because the tag would be stripped anyway.
func (p *Protocol) handleRoomName(s string, tagStart, tagEnd int, b *strings.Builder, logOn bool) {
	name := extractTagName(s[tagStart:tagEnd])
	if !strings.EqualFold(name, "ROOMNAME") {
		return
	}
	// Look for </ROOMNAME> after the closing '>'.
	closeTag := "</ROOMNAME>"
	if idx := strings.Index(strings.ToUpper(s[tagEnd+1:]), strings.ToUpper(closeTag)); idx >= 0 {
		room := s[tagEnd+1 : tagEnd+1+idx]
		if p.onRoomName != nil && room != "" {
			p.onRoomName(room)
		}
		if logOn && room != "" {
			p.logEvent("room_name", []byte(s[tagStart-1:tagEnd+1+idx+len(closeTag)]), map[string]any{"name": room})
		}
	}
}

// extractTagName returns the tag name from the inside of an MXP tag,
// ignoring attributes.  "ROOMNAME desc" → "ROOMNAME".
func extractTagName(body string) string {
	body = strings.TrimSpace(body)
	if i := strings.IndexAny(body, " \t"); i >= 0 {
		return body[:i]
	}
	return body
}

// handleProbe inspects the body of a tag and issues the spec-mandated
// VERSION / SUPPORTS response when the server sends a bare probe.
func (p *Protocol) handleProbe(body string) {
	t := strings.TrimSpace(body)
	t = strings.TrimSuffix(t, "/")
	t = strings.TrimSpace(t)

	// Extract the tag name (first word before any space).
	tagName := t
	if i := strings.IndexAny(t, " \t"); i >= 0 {
		tagName = t[:i]
	}

	switch strings.ToUpper(tagName) {
	case "VERSION":
		// Respond to bare <VERSION> and <VERSION MXP="…"> probes.
		// Avoid echoing a client response that already contains CLIENT=.
		if !strings.Contains(strings.ToUpper(t), "CLIENT=") {
			p.sendVersion()
		}
	case "SUPPORT":
		p.sendSupports()
	}
}

// sendVersion writes a SECURE-line <VERSION ...> response. Per the Zuggsoft
// spec: "<VERSION> tag is sent back to the MUD as a SECURE-tagged line. This
// means the MXP command is prefixed by ESC[1z and followed by a newline."
func (p *Protocol) sendVersion() {
	if p.ctx == nil {
		return
	}
	msg := "\x1b[1z<VERSION MXP=\"" + MXPVersion +
		"\" CLIENT=\"" + ClientName +
		"\" VERSION=\"" + ClientVersion +
		"\" REGISTERED=\"no\">\n"
	_ = p.ctx.Send([]byte(msg))
	if p.logEnabled() {
		p.logTx("probe", []byte(msg), map[string]any{"kind": "version"})
	}
	p.ctx.Debug("MXP: sent VERSION response")
}

// sendSupports writes a SECURE-line <SUPPORTS ...> listing the tags this
// client accepts without breaking. v1 strips all tags, so "support" here
// means "won't choke" rather than "will render richly". The list is sized
// to match what servers like t2tmud.org probe for in <SUPPORT>; advertising
// every queried capability prevents the server from downgrading the session
// to plain text when it sees a partial reply.
func (p *Protocol) sendSupports() {
	if p.ctx == nil {
		return
	}
	const list = "+B +I +U +S +COLOR +C +FONT +HIGH +NOBR +P +BR +SBR " +
		"+A +SEND +EXPIRE +IMAGE +GAUGE " +
		"+FONT.FACE +FONT.SIZE +FONT.COLOR +FONT.BACK"
	msg := "\x1b[1z<SUPPORTS " + list + ">\n"
	_ = p.ctx.Send([]byte(msg))
	if p.logEnabled() {
		p.logTx("probe", []byte(msg), map[string]any{"kind": "supports"})
	}
	p.ctx.Debug("MXP: sent SUPPORTS response")
}

// applyMode updates mode/defaultMode for an ESC [ N z with the given N.
func (p *Protocol) applyMode(n int) {
	modeName := "User"
	switch n {
	case 0:
		p.mode = Open
		modeName = "Open"
	case 1:
		p.mode = Secure
		modeName = "Secure"
	case 2:
		p.mode = Locked
		modeName = "Locked"
	case 3:
		// Reset: close all tags (no-op for v1 strip) and revert to default.
		p.mode = p.defaultMode
		modeName = "Reset"
	case 4:
		// Temp-secure: next tag only. v1 strips all tags anyway, so behave
		// like Secure for state-tracking purposes.
		p.mode = Secure
		modeName = "TempSecure"
	case 5:
		p.defaultMode = Open
		p.mode = Open
		modeName = "LockOpen"
	case 6:
		p.defaultMode = Secure
		p.mode = Secure
		modeName = "LockSecure"
	case 7:
		p.defaultMode = Locked
		p.mode = Locked
		modeName = "LockLocked"
	default:
		// 10..99: user line tag. Treat the remainder of the line as
		// whatever the tag represents; for v1 that's just Open.
	}
	if p.logEnabled() {
		p.logEvent("mode", nil, map[string]any{"n": n, "mode": modeName})
	}
}

func (p *Protocol) logEnabled() bool {
	return p.log != nil && p.log.Enabled()
}

func (p *Protocol) logEvent(event string, raw []byte, parsed map[string]any) {
	e := protolog.Entry{Source: "mxp", Dir: "rx", Event: event, Parsed: parsed}
	if len(raw) > 0 {
		e.UTF8 = protolog.EncodeUTF8(raw)
		e.Hex = protolog.EncodeHex(raw)
	}
	p.log.Log(e)
}

func (p *Protocol) logTx(event string, raw []byte, parsed map[string]any) {
	e := protolog.Entry{Source: "mxp", Dir: "tx", Event: event, Parsed: parsed}
	if len(raw) > 0 {
		e.UTF8 = protolog.EncodeUTF8(raw)
		e.Hex = protolog.EncodeHex(raw)
	}
	p.log.Log(e)
}

// findTagEnd returns the index of the '>' that terminates the MXP tag
// starting at s[0] (which must be '<'), respecting single- and double-quoted
// attribute values so nested tags inside element definitions like
// `<!el x '<send …>'>` are not closed prematurely. Returns -1 if no
// terminating '>' is present in s.
func findTagEnd(s string) int {
	var quote byte
	for i := 1; i < len(s); i++ {
		c := s[i]
		if quote != 0 {
			if c == quote {
				quote = 0
			}
			continue
		}
		switch c {
		case '\'', '"':
			quote = c
		case '>':
			return i
		}
	}
	return -1
}

// parseModeEsc recognizes `ESC [ N z` (single-digit, 4 bytes) and
// `ESC [ N N z` (two-digit 10..99, 5 bytes) at the start of s.
// Returns (bytes consumed, N, true) on match; (0, 0, false) otherwise.
func parseModeEsc(s string) (int, int, bool) {
	if len(s) < 4 || s[0] != '\x1b' || s[1] != '[' {
		return 0, 0, false
	}
	if !isDigit(s[2]) {
		return 0, 0, false
	}
	// Single-digit: ESC [ N z
	if s[3] == 'z' {
		return 4, int(s[2] - '0'), true
	}
	// Two-digit: ESC [ N N z (requires N >= 10, i.e. first digit != 0)
	if len(s) >= 5 && isDigit(s[3]) && s[4] == 'z' && s[2] != '0' {
		return 5, int(s[2]-'0')*10 + int(s[3]-'0'), true
	}
	return 0, 0, false
}

func isDigit(b byte) bool { return b >= '0' && b <= '9' }
