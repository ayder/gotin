package network

import (
	"io"
	"testing"
)

// fakeProto is a minimal Protocol for testing dispatch.
type fakeProto struct {
	name       string
	opt        byte
	kind       Kind
	enabled    int
	disabled   int
	subnegBody []byte
	subnegErr  error
}

func (f *fakeProto) Name() string             { return f.name }
func (f *fakeProto) Option() byte             { return f.opt }
func (f *fakeProto) Kind() Kind               { return f.kind }
func (f *fakeProto) OnEnable(_ Context) error { f.enabled++; return nil }
func (f *fakeProto) OnDisable(_ Context)      { f.disabled++ }
func (f *fakeProto) OnSubnegotiation(_ Context, data []byte) error {
	f.subnegBody = append(f.subnegBody[:0], data...)
	return f.subnegErr
}

func TestRegistry_AcceptHimForInstalledProtocol(t *testing.T) {
	c := &Client{protocols: make(map[byte]Protocol)}
	// Before install, an exotic option is refused.
	_, resp := c.ProcessIAC([]byte{IAC, WILL, MCCP2})
	if string(resp) != string([]byte{IAC, DONT, MCCP2}) {
		t.Fatalf("want DONT MCCP2 before install, got %v", resp)
	}
	// After install (without a live conn), acceptHim must accept.
	if err := c.Install(&fakeProto{name: "MCCP2", opt: MCCP2, kind: KindHim}); err != nil {
		t.Fatalf("Install: %v", err)
	}
	_, resp = c.ProcessIAC([]byte{IAC, WILL, MCCP2})
	if string(resp) != string([]byte{IAC, DO, MCCP2}) {
		t.Fatalf("want DO MCCP2 after install, got %v", resp)
	}
}

// Spec: MXP is a client-performs option. Server sends IAC DO MXP; client
// must reply IAC WILL MXP. A KindHim-only protocol must NOT accept DO.
func TestRegistry_AcceptUsForClientPerformsProtocol(t *testing.T) {
	c := &Client{protocols: make(map[byte]Protocol)}
	// Before install, DO MXP is refused.
	_, resp := c.ProcessIAC([]byte{IAC, DO, MXP})
	if string(resp) != string([]byte{IAC, WONT, MXP}) {
		t.Fatalf("want WONT MXP before install, got %v", resp)
	}
	// After install with KindUs, reply must be WILL MXP.
	fp := &fakeProto{name: "MXP", opt: MXP, kind: KindUs}
	if err := c.Install(fp); err != nil {
		t.Fatalf("Install: %v", err)
	}
	_, resp = c.ProcessIAC([]byte{IAC, DO, MXP})
	if string(resp) != string([]byte{IAC, WILL, MXP}) {
		t.Fatalf("want WILL MXP after install, got %v", resp)
	}
	if fp.enabled != 1 {
		t.Fatalf("OnEnable should fire on Us-side transition; got %d", fp.enabled)
	}
}

// A bidirectional (KindHim|KindUs) protocol like MXP must accept both
// IAC WILL and IAC DO from the server. Regression for t2tmud.org negotiation,
// where the server sends IAC WILL MXP first.
func TestRegistry_BidirectionalKindAcceptsBothWillAndDo(t *testing.T) {
	c := &Client{protocols: make(map[byte]Protocol)}
	_ = c.Install(&fakeProto{name: "MXP", opt: MXP, kind: KindHim | KindUs})

	_, respWill := c.ProcessIAC([]byte{IAC, WILL, MXP})
	if string(respWill) != string([]byte{IAC, DO, MXP}) {
		t.Fatalf("server-initiated WILL MXP should yield DO reply, got %v", respWill)
	}

	_, respDo := c.ProcessIAC([]byte{IAC, DO, MXP})
	if string(respDo) != string([]byte{IAC, WILL, MXP}) {
		t.Fatalf("server-initiated DO MXP should yield WILL reply, got %v", respDo)
	}
}

// Spec: a KindHim protocol must NOT accept a server DO (that would mean the
// server is asking us to perform a server-performs option).
func TestRegistry_KindHimRejectsDO(t *testing.T) {
	c := &Client{protocols: make(map[byte]Protocol)}
	_ = c.Install(&fakeProto{name: "MCCP2", opt: MCCP2, kind: KindHim})
	_, resp := c.ProcessIAC([]byte{IAC, DO, MCCP2})
	if string(resp) != string([]byte{IAC, WONT, MCCP2}) {
		t.Fatalf("KindHim must reject DO; got %v", resp)
	}
}

func TestRegistry_DispatchesSubneg(t *testing.T) {
	c := &Client{protocols: make(map[byte]Protocol)}
	fp := &fakeProto{name: "MCCP2-sub", opt: MCCP2, kind: KindHim}
	if err := c.Install(fp); err != nil {
		t.Fatalf("Install: %v", err)
	}
	// Negotiate first so him == YES.
	_, _ = c.ProcessIAC([]byte{IAC, WILL, MCCP2})

	body := []byte("hello")
	buf := []byte{IAC, SB, MCCP2}
	buf = append(buf, body...)
	buf = append(buf, IAC, SE)
	_, _ = c.ProcessIAC(buf)

	if string(fp.subnegBody) != "hello" {
		t.Fatalf("subneg body: want %q got %q", "hello", fp.subnegBody)
	}
	if fp.enabled != 1 {
		t.Fatalf("OnEnable want 1 call, got %d", fp.enabled)
	}
}

func TestRegistry_UninstallFiresOnDisable(t *testing.T) {
	c := &Client{protocols: make(map[byte]Protocol)}
	fp := &fakeProto{name: "MXP", opt: MXP, kind: KindUs}
	_ = c.Install(fp)
	_, _ = c.ProcessIAC([]byte{IAC, DO, MXP}) // server asks client to do MXP; flips us to YES
	if err := c.Uninstall("MXP"); err != nil {
		t.Fatalf("Uninstall: %v", err)
	}
	if fp.disabled != 1 {
		t.Fatalf("OnDisable want 1 call, got %d", fp.disabled)
	}
	if _, ok := c.protocols[MXP]; ok {
		t.Fatal("protocol still in registry after Uninstall")
	}
}

// swapProto is a Protocol that calls ctx.SwapReader on subneg.
type swapProto struct {
	opt     byte
	wrapper func(io.Reader) io.Reader
	called  int
}

func (s *swapProto) Name() string             { return "SWAP" }
func (s *swapProto) Option() byte             { return s.opt }
func (s *swapProto) Kind() Kind               { return KindHim }
func (s *swapProto) OnEnable(_ Context) error { return nil }
func (s *swapProto) OnDisable(_ Context)      {}
func (s *swapProto) OnSubnegotiation(ctx Context, _ []byte) error {
	s.called++
	ctx.SwapReader(s.wrapper)
	return nil
}

// ActiveProtocols should reflect both server-performs (Him) and client-performs
// (Us) transitions, and the SetProtocolStatusCallback must fire exactly when
// the active set changes.
func TestActiveProtocols_CallbackOnNegotiation(t *testing.T) {
	c := &Client{protocols: make(map[byte]Protocol), options: newOptionTable()}
	var calls [][]string
	c.SetProtocolStatusCallback(func(active []string) {
		calls = append(calls, append([]string(nil), active...))
	})
	_ = c.Install(&fakeProto{name: "MCCP2", opt: MCCP2, kind: KindHim})
	_ = c.Install(&fakeProto{name: "MXP", opt: MXP, kind: KindUs})

	if got := c.ActiveProtocols(); len(got) != 0 {
		t.Fatalf("active before negotiation: want empty, got %v", got)
	}

	// Server: WILL MCCP2 → client agrees, MCCP2 becomes active.
	_, _ = c.ProcessIAC([]byte{IAC, WILL, MCCP2})
	if got := c.ActiveProtocols(); len(got) != 1 || got[0] != "MCCP2" {
		t.Fatalf("after WILL MCCP2: want [MCCP2], got %v", got)
	}

	// Server: DO MXP → client agrees, MXP becomes active.
	_, _ = c.ProcessIAC([]byte{IAC, DO, MXP})
	got := c.ActiveProtocols()
	if len(got) != 2 || got[0] != "MCCP2" || got[1] != "MXP" {
		t.Fatalf("after DO MXP: want [MCCP2 MXP], got %v", got)
	}

	// Server: WONT MCCP2 → MCCP2 drops out.
	_, _ = c.ProcessIAC([]byte{IAC, WONT, MCCP2})
	if got := c.ActiveProtocols(); len(got) != 1 || got[0] != "MXP" {
		t.Fatalf("after WONT MCCP2: want [MXP], got %v", got)
	}

	// The callback fires during Install (once per protocol) and then once
	// per Q Method transition. With two protocols installed, that's 2 + 3 = 5.
	if len(calls) != 5 {
		t.Fatalf("callback calls: want 5, got %d (%v)", len(calls), calls)
	}
}

func TestProcessIAC_CapturesStreamTailAfterSwapRequest(t *testing.T) {
	c := &Client{protocols: make(map[byte]Protocol)}
	sp := &swapProto{
		opt: MCCP2,
		wrapper: func(r io.Reader) io.Reader { return r }, // identity for test
	}
	_ = c.Install(sp)
	_, _ = c.ProcessIAC([]byte{IAC, WILL, MCCP2})

	// IAC SB MCCP2 IAC SE <tail bytes...>
	input := []byte{IAC, SB, MCCP2, IAC, SE, 'a', 'b', 'c'}
	clean, _ := c.ProcessIAC(input)

	if sp.called != 1 {
		t.Fatalf("OnSubnegotiation calls: want 1 got %d", sp.called)
	}
	if c.pendingSwap == nil {
		t.Fatal("pendingSwap should be set after ctx.SwapReader")
	}
	if string(c.streamTail) != "abc" {
		t.Fatalf("streamTail: want %q got %q", "abc", c.streamTail)
	}
	// The tail must NOT be in cleanData — it belongs to the swapped reader.
	if len(clean) != 0 {
		t.Fatalf("cleanData should be empty, got %q", clean)
	}
}
