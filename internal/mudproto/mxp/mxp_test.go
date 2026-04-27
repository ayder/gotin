package mxp

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/ayder/gotin/internal/mudproto/protolog"
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
func (c *captureCtx) SendSubneg(_ byte, _ []byte) error      { return nil }
func (c *captureCtx) SwapReader(_ func(io.Reader) io.Reader) {}
func (c *captureCtx) Debug(_ string, _ ...any)               {}
func (c *captureCtx) OptionActive(_ byte) bool               { return true }
func (c *captureCtx) NotifyProtocolStatus()                  {}

type captureSink struct {
	events []string
}

func (s *captureSink) OnText(t string)         { s.events = append(s.events, "T:"+t) }
func (s *captureSink) OnTag(name, body string) { s.events = append(s.events, "G:"+name) }

func TestSetSink_InterleavesTextAndTags(t *testing.T) {
	p := New()
	s := &captureSink{}
	p.SetSink(s)

	_ = p.Filter("hello<expire>world<x>east</x>!")

	want := []string{"T:hello", "G:expire", "T:world", "G:x", "T:east", "G:/x", "T:!"}
	if len(s.events) != len(want) {
		t.Fatalf("got %v, want %v", s.events, want)
	}
	for i := range want {
		if s.events[i] != want[i] {
			t.Errorf("events[%d] = %q, want %q", i, s.events[i], want[i])
		}
	}
}

func TestSetSink_NilSafe(t *testing.T) {
	p := New()
	out := p.Filter("<x>east</x>")
	if out != "east" {
		t.Errorf("Filter(\"<x>east</x>\") = %q, want \"east\"", out)
	}
}

func TestSetSink_FiresOnceAfterSplitCompletion(t *testing.T) {
	p := New()
	s := &captureSink{}
	p.SetSink(s)

	_ = p.Filter("<x")
	for _, e := range s.events {
		if strings.HasPrefix(e, "G:") {
			t.Errorf("tag event fired on partial tag: %q", e)
		}
	}
	_ = p.Filter(">east</x>")

	var tagEvents []string
	for _, e := range s.events {
		if strings.HasPrefix(e, "G:") {
			tagEvents = append(tagEvents, e[2:])
		}
	}
	want := []string{"x", "/x"}
	if len(tagEvents) != len(want) {
		t.Fatalf("got %v, want %v", tagEvents, want)
	}
	for i := range want {
		if tagEvents[i] != want[i] {
			t.Errorf("tagEvents[%d] = %q, want %q", i, tagEvents[i], want[i])
		}
	}
}

func TestSentinelResetCallback_FiresOnDisable(t *testing.T) {
	p := New()
	called := false
	p.SetSentinelResetCallback(func() { called = true })
	p.OnDisable(&captureCtx{})
	if !called {
		t.Fatalf("reset callback did not fire")
	}
}

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
		{"partial <B still open", "partial "}, // no closing '>', remainder buffered for next call
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

func TestFilter_LoggerEmitsExpectedEvents(t *testing.T) {
	var buf bytes.Buffer
	lg := protolog.NewJSONLinesLogger(&buf, func() bool { return true })
	p := New()
	p.SetLogger(lg)

	in := "<VERSION><ROOMNAME>Hall</ROOMNAME>\x1b[1z<NOBR>\nAfter\n"
	_ = p.Filter(in)

	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) == 0 {
		t.Fatalf("no log lines")
	}

	var sawChunk, sawTag, sawRoomName, sawMode bool
	for _, line := range lines {
		var e protolog.Entry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			t.Fatalf("malformed line %q: %v", line, err)
		}
		if e.Source != "mxp" {
			t.Errorf("source = %q, want mxp", e.Source)
		}
		switch e.Event {
		case "chunk":
			sawChunk = true
		case "tag":
			sawTag = true
		case "room_name":
			sawRoomName = true
			if got, _ := e.Parsed["name"].(string); got != "Hall" {
				t.Errorf("room_name parsed.name = %v, want Hall", e.Parsed["name"])
			}
		case "mode":
			sawMode = true
		}
	}
	if !sawChunk || !sawTag || !sawRoomName || !sawMode {
		t.Errorf("missing events: chunk=%v tag=%v room_name=%v mode=%v", sawChunk, sawTag, sawRoomName, sawMode)
	}
}

func TestFilter_FullStreamLogIsParseableJSONLines(t *testing.T) {
	var buf bytes.Buffer
	lg := protolog.NewJSONLinesLogger(&buf, func() bool { return true })
	p := New()
	p.SetLogger(lg)
	p.SetContext(&captureCtx{})

	in := "<VERSION><ROOMNAME>Plaza</ROOMNAME>\x1b[1z<NOBR>\nDone\n"
	_ = p.Filter(in)

	rdr := bufio.NewScanner(&buf)
	var n int
	for rdr.Scan() {
		var e protolog.Entry
		if err := json.Unmarshal(rdr.Bytes(), &e); err != nil {
			t.Errorf("line %d not valid JSON: %v (%q)", n, err, rdr.Text())
		}
		n++
	}
	if err := rdr.Err(); err != nil {
		t.Fatalf("scan log: %v", err)
	}
	if n == 0 {
		t.Fatalf("no lines written")
	}
}

// Regression: a tag split across two calls must be buffered and stripped on
// completion, not leaked as raw bytes. Real t2tmud sessions chunk MXP tags at
// arbitrary byte boundaries (e.g. `<x>east\x1b[4z<` then `/x>...`), and any
// leak produces visible `e</x>` artifacts in the UI.
func TestFilter_PartialTagAcrossCalls(t *testing.T) {
	p := New()
	got1 := p.Filter("before<B")
	got2 := p.Filter("old>after")
	// First call: only "before" is emitted; "<B" is buffered for next call.
	if got1 != "before" {
		t.Fatalf("partial tag chunk 1: got %q, want %q", got1, "before")
	}
	// Second call: buffered "<B" + "old>" forms a complete <Bold> tag, gets
	// stripped, and the trailing "after" is emitted.
	if got2 != "after" {
		t.Fatalf("partial tag chunk 2: got %q, want %q", got2, "after")
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

// TestFilter_StripsElementDefinitionWithNestedTag verifies that an MXP
// element definition like `<!el x '<send …>'>` is consumed entirely, not
// truncated at the inner '>' (which would leak the trailing `'>` to the UI).
// Captured from a real t2tmud session.
func TestFilter_StripsElementDefinitionWithNestedTag(t *testing.T) {
	p := New()
	in := `<!el x '<send href="&text;|l &text;" hint="go &text;|look &text;" expire="room_exits">'>HELLO`
	out := p.Filter(in)
	if out != "HELLO" {
		t.Fatalf("element definition leaked: got %q, want %q", out, "HELLO")
	}
}

func TestFilter_StripsBurstOfElementDefinitions(t *testing.T) {
	p := New()
	in := `<!el x '<send href="&text;">'>` +
		`<!el xx '<send href="&text;">' att='dir'>` +
		`<gauge hp maxhp caption='HP' color=red>` +
		`<!en hp 70>` +
		`AFTER`
	out := p.Filter(in)
	if out != "AFTER" {
		t.Fatalf("burst of definitions leaked: got %q, want %q", out, "AFTER")
	}
}

// TestFilter_SplitTagBufferedAcrossCalls verifies that a tag split at a
// network-chunk boundary is buffered and stripped on the second call rather
// than leaking '<' and the rest of the tag to the UI as separate fragments.
// Captured from a real t2tmud session where chunks ended with `<x>east\x1b[4z<`
// and the next chunk began with `/x>...`, producing visible `e</x>` artifacts.
func TestFilter_SplitTagBufferedAcrossCalls(t *testing.T) {
	p := New()
	out1 := p.Filter("\x1b[1;37m\x1b[4z<x>east\x1b[4z<")
	out2 := p.Filter("/x>\x1b[0m, ")
	combined := out1 + out2
	if strings.Contains(combined, "<") || strings.Contains(combined, ">") {
		t.Fatalf("split tag leaked angle brackets: out1=%q out2=%q", out1, out2)
	}
	if !strings.Contains(combined, "east") {
		t.Fatalf("exit name missing: out1=%q out2=%q", out1, out2)
	}
}

func TestFilter_SplitElementDefinitionBufferedAcrossCalls(t *testing.T) {
	p := New()
	out1 := p.Filter(`<!el x '<send href="&text;|l &text;" expire="room`)
	out2 := p.Filter(`_exits">'>HELLO`)
	combined := out1 + out2
	if combined != "HELLO" {
		t.Fatalf("split element definition leaked: combined=%q", combined)
	}
}

func TestFindTagEnd(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"<x>", 2},
		{"<i30 \"seagull 1\">", 16},
		{"<gauge hp maxhp caption='HP' color=red>", 38},
		{`<!el x '<send href="x">'>`, 24},
		{`<!el m '<send href="f|b" expire=ml>'>`, 36},
		{"<incomplete", -1},
		{`<unclosed-quote '`, -1},
	}
	for _, c := range cases {
		got := findTagEnd(c.in)
		if got != c.want {
			t.Errorf("findTagEnd(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}
