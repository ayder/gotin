package mxp

import (
	"io"
	"strings"
	"testing"

	"github.com/ayder/gotin/internal/network"
)

// captureCtx implements network.Context and records every Send call so tests
// can assert the exact bytes MXP writes in response to server probes.
type captureCtx struct {
	sent [][]byte
}

func (c *captureCtx) Send(b []byte) error {
	c.sent = append(c.sent, append([]byte(nil), b...))
	return nil
}
func (c *captureCtx) SendSubneg(_ byte, _ []byte) error    { return nil }
func (c *captureCtx) SwapReader(_ func(io.Reader) io.Reader) {}
func (c *captureCtx) Debug(_ string, _ ...any)              {}
func (c *captureCtx) OptionActive(_ byte) bool              { return true }
func (c *captureCtx) NotifyProtocolStatus()                 {}

// MXP must accept both server-initiated (IAC WILL MXP, e.g. t2tmud.org) and
// client-initiated (IAC DO MXP) negotiations.
func TestKind_AcceptsBothDirections(t *testing.T) {
	p := New()
	k := p.Kind()
	if k&network.KindHim == 0 {
		t.Fatal("MXP.Kind() must include KindHim so server-sent IAC WILL MXP is accepted")
	}
	if k&network.KindUs == 0 {
		t.Fatal("MXP.Kind() must include KindUs so server-sent IAC DO MXP is accepted")
	}
}

func TestFilter_StripsTags(t *testing.T) {
	p := New()
	cases := []struct {
		in, want string
	}{
		{"plain text", "plain text"},
		{"<B>bold</B>", "bold"},
		{"before<SEND href=\"look\">look</SEND>after", "beforelookafter"},
		{"<B>x</B> and <I>y</I>", "x and y"},
		{"partial <B still open", "partial <B still open"}, // no closing '>', pass through
	}
	for _, tc := range cases {
		got := p.Filter(tc.in)
		if got != tc.want {
			t.Errorf("Filter(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestFilter_ModeSwitchEscapeConsumed(t *testing.T) {
	p := New()
	if got := p.Filter("text\x1b[1zmore"); got != "textmore" {
		t.Fatalf("ESC [ 1 z should be stripped; got %q", got)
	}
	if p.Mode() != Secure {
		t.Fatalf("mode should be Secure after ESC [ 1 z, got %v", p.Mode())
	}
	_ = p.Filter("\x1b[0z")
	if p.Mode() != Open {
		t.Fatalf("mode should reset to Open after ESC [ 0 z, got %v", p.Mode())
	}
	_ = p.Filter("\x1b[2z")
	if p.Mode() != Locked {
		t.Fatalf("mode should be Locked after ESC [ 2 z, got %v", p.Mode())
	}
}

func TestFilter_PreservesUnrelatedEscape(t *testing.T) {
	p := New()
	// ANSI color code (ESC [ 31 m) is not an MXP mode-switch; pass through.
	in := "\x1b[31mred\x1b[0m"
	if got := p.Filter(in); got != in {
		t.Fatalf("ANSI color should pass through; got %q", got)
	}
}

// Spec: on newline, mode reverts to the current defaultMode.
func TestFilter_NewlineRevertsToDefault(t *testing.T) {
	p := New()
	// Switch to Secure (temporary), then emit a newline.
	_ = p.Filter("\x1b[1z")
	if p.Mode() != Secure {
		t.Fatalf("mode should be Secure after ESC [ 1 z, got %v", p.Mode())
	}
	_ = p.Filter("text\n")
	if p.Mode() != Open {
		t.Fatalf("mode should revert to Open (defaultMode) after newline, got %v", p.Mode())
	}
}

// Spec: ESC [ 6 z is "Lock Secure" — it sets defaultMode to Secure so that
// subsequent newlines revert into Secure rather than Open.
func TestFilter_Mode6LocksSecureAsDefault(t *testing.T) {
	p := New()
	_ = p.Filter("\x1b[6z")
	if p.DefaultMode() != Secure {
		t.Fatalf("defaultMode should be Secure after ESC [ 6 z, got %v", p.DefaultMode())
	}
	// A temporary Open line followed by newline must land back in Secure.
	_ = p.Filter("\x1b[0zopen text\n")
	if p.Mode() != Secure {
		t.Fatalf("newline should revert to Secure (locked default), got %v", p.Mode())
	}
}

// Spec: ESC [ 3 z is Reset — mode := defaultMode without waiting for newline.
func TestFilter_Mode3Resets(t *testing.T) {
	p := New()
	_ = p.Filter("\x1b[1z")
	_ = p.Filter("\x1b[3z")
	if p.Mode() != Open {
		t.Fatalf("ESC [ 3 z should reset to defaultMode=Open, got %v", p.Mode())
	}
}

// Spec: ESC [ N z for N in [10,99] is a user line tag. v1 just consumes it;
// the important thing is the parser recognizes it and doesn't leak the 5
// bytes into output.
func TestFilter_LineTagConsumed(t *testing.T) {
	p := New()
	got := p.Filter("\x1b[20zAuction: item for sale\n")
	want := "Auction: item for sale\n"
	if got != want {
		t.Fatalf("line-tag 20: want %q got %q", want, got)
	}
}

// Regression: the two-digit parser must reject N=09 (leading zero).
func TestFilter_TwoDigitRequiresNonZeroLead(t *testing.T) {
	p := New()
	// ESC [ 09 z is not a valid MXP mode — it should be passed through
	// (not consumed) and the digits/characters remain in output.
	got := p.Filter("\x1b[09ztext")
	if got == "text" {
		t.Fatalf("ESC [ 09 z must not be treated as a valid line tag; got %q", got)
	}
}

// Spec: when the server sends a bare <VERSION> probe, the client must reply
// on a SECURE line (ESC [ 1 z) with its own <VERSION …> tag plus a trailing
// newline. Regression for t2tmud.org's "No MXP client support" message.
func TestFilter_RespondsToVersionProbe(t *testing.T) {
	p := New()
	cc := &captureCtx{}
	_ = p.OnEnable(cc)

	_ = p.Filter("<VERSION>")

	if len(cc.sent) != 1 {
		t.Fatalf("expected exactly one Send, got %d", len(cc.sent))
	}
	got := string(cc.sent[0])
	// Must be a SECURE line.
	if !strings.HasPrefix(got, "\x1b[1z") {
		t.Errorf("response must start with ESC [ 1 z; got %q", got)
	}
	// Must be a <VERSION …> tag.
	if !strings.Contains(got, "<VERSION ") {
		t.Errorf("response must contain <VERSION …>; got %q", got)
	}
	// Must carry identification.
	if !strings.Contains(got, `CLIENT="gotin"`) {
		t.Errorf("response must name the client; got %q", got)
	}
	if !strings.Contains(got, `MXP="`) {
		t.Errorf("response must advertise MXP version; got %q", got)
	}
	// Must end with a newline (ends the SECURE line).
	if !strings.HasSuffix(got, "\n") {
		t.Errorf("response must end with newline; got %q", got)
	}
}

// Spec: <SUPPORT> probe yields a <SUPPORTS …> reply (note plural) on a
// SECURE line listing tags we accept without breaking.
func TestFilter_RespondsToSupportProbe(t *testing.T) {
	p := New()
	cc := &captureCtx{}
	_ = p.OnEnable(cc)

	_ = p.Filter("<SUPPORT>")

	if len(cc.sent) != 1 {
		t.Fatalf("expected exactly one Send, got %d", len(cc.sent))
	}
	got := string(cc.sent[0])
	if !strings.HasPrefix(got, "\x1b[1z<SUPPORTS ") {
		t.Errorf("SUPPORTS response must start with ESC [ 1 z <SUPPORTS; got %q", got)
	}
	if !strings.HasSuffix(got, ">\n") {
		t.Errorf("SUPPORTS response must end with >\\n; got %q", got)
	}
}

// The server's own VERSION response form (<VERSION MXP="..." CLIENT="...">)
// must NOT trigger another reply — it already contains CLIENT=.
func TestFilter_DoesNotRespondToVersionResponseForm(t *testing.T) {
	p := New()
	cc := &captureCtx{}
	_ = p.OnEnable(cc)

	_ = p.Filter(`<VERSION MXP="0.4" CLIENT="some-server" VERSION="1.0">`)

	if len(cc.sent) != 0 {
		t.Fatalf("must not respond to a tag with attributes; sent %v", cc.sent)
	}
}

// Some MUDs send <VERSION MXP="0.4"> as a probe (announcing their version
// and asking for ours). The client should still respond.
func TestFilter_RespondsToVersionProbeWithMXPAttr(t *testing.T) {
	p := New()
	cc := &captureCtx{}
	_ = p.OnEnable(cc)

	_ = p.Filter(`<VERSION MXP="0.4">`)

	if len(cc.sent) != 1 {
		t.Fatalf("expected exactly one Send, got %d", len(cc.sent))
	}
	got := string(cc.sent[0])
	if !strings.HasPrefix(got, "\x1b[1z") {
		t.Errorf("response must start with ESC [ 1 z; got %q", got)
	}
	if !strings.Contains(got, "<VERSION ") {
		t.Errorf("response must contain <VERSION …>; got %q", got)
	}
}

// After OnDisable, any further probe must be silently ignored (no panic,
// no Send on a stale context).
func TestFilter_NoSendAfterDisable(t *testing.T) {
	p := New()
	cc := &captureCtx{}
	_ = p.OnEnable(cc)
	p.OnDisable(cc)

	_ = p.Filter("<VERSION>")

	if len(cc.sent) != 0 {
		t.Fatalf("must not Send after OnDisable; got %v", cc.sent)
	}
}

// Regression: a tag split across two calls must not be dropped.
func TestFilter_PartialTagAcrossCalls(t *testing.T) {
	p := New()
	got1 := p.Filter("before<B")
	got2 := p.Filter("old>after")
	// First chunk has no closing '>', so "<B" should pass through.
	if got1 != "before<B" {
		t.Fatalf("partial tag chunk 1: got %q", got1)
	}
	// Second chunk has the completion — but since Filter is stateless across
	// tag boundaries, the second chunk sees "old>after" with no leading '<'
	// and just passes through. This documents current v1 behavior: a full
	// tag must fit within a single Filter call. Upstream buffering (e.g.
	// linebuffer) is responsible for whole-line delivery.
	if got2 != "old>after" {
		t.Fatalf("partial tag chunk 2: got %q", got2)
	}
}

// When OnEnable hasn't fired (no telnet negotiation), SetContext gives the
// protocol enough state to send probe responses.
func TestFilter_RespondsViaSetContext(t *testing.T) {
	p := New()
	cc := &captureCtx{}
	// Don't call OnEnable — simulate a MUD that sends MXP inline without
	// completing telnet option 91 negotiation.
	p.SetContext(cc)

	_ = p.Filter("<VERSION>")

	if len(cc.sent) != 1 {
		t.Fatalf("expected exactly one Send after SetContext, got %d", len(cc.sent))
	}
	if !strings.HasPrefix(string(cc.sent[0]), "\x1b[1z<VERSION ") {
		t.Fatalf("expected VERSION response; got %q", cc.sent[0])
	}
}

// TestFilter_CapturesRoomName verifies that <ROOMNAME>…</ROOMNAME> tags
// trigger the registered callback and are stripped from output.
func TestFilter_CapturesRoomName(t *testing.T) {
	p := New()
	var got string
	p.SetRoomNameCallback(func(name string) { got = name })

	out := p.Filter("You enter <ROOMNAME>The Foyer</ROOMNAME>.\nIt is dark.")
	if got != "The Foyer" {
		t.Fatalf("room name callback: want %q got %q", "The Foyer", got)
	}
	if strings.Contains(out, "ROOMNAME") {
		t.Fatalf("output should not contain ROOMNAME tag: %q", out)
	}
}

// TestFilter_CapturesRoomNameWithAttributes verifies room-name extraction
// when the open tag carries attributes (e.g. <ROOMNAME desc="foo">).
func TestFilter_CapturesRoomNameWithAttributes(t *testing.T) {
	p := New()
	var got string
	p.SetRoomNameCallback(func(name string) { got = name })

	_ = p.Filter(`<ROOMNAME desc="A hall">Grand Hall</ROOMNAME>`)
	if got != "Grand Hall" {
		t.Fatalf("room name callback: want %q got %q", "Grand Hall", got)
	}
}

// TestFilter_RoomNameCaseInsensitive verifies case-insensitive matching.
func TestFilter_RoomNameCaseInsensitive(t *testing.T) {
	p := New()
	var got string
	p.SetRoomNameCallback(func(name string) { got = name })

	_ = p.Filter("<roomname>Kitchen</roomname>")
	if got != "Kitchen" {
		t.Fatalf("room name callback: want %q got %q", "Kitchen", got)
	}
}

// TestFilter_NoCallbackWhenRoomNameMissing verifies nothing fires without a
// close tag (the open tag is simply stripped).
func TestFilter_NoCallbackWhenRoomNameMissing(t *testing.T) {
	p := New()
	fired := false
	p.SetRoomNameCallback(func(name string) { fired = true })

	out := p.Filter("<ROOMNAME>unfinished")
	if fired {
		t.Fatal("callback should not fire without close tag")
	}
	if out != "unfinished" {
		t.Fatalf("unexpected output: %q", out)
	}
}
