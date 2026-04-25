package gmcp

import (
	"io"
	"testing"
)

// ctxStub satisfies network.Context for unit testing without a live Client.
type ctxStub struct{}

func (ctxStub) Send(_ []byte) error                  { return nil }
func (ctxStub) SendSubneg(_ byte, _ []byte) error    { return nil }
func (ctxStub) SwapReader(_ func(io.Reader) io.Reader) {}
func (ctxStub) Debug(_ string, _ ...any)              {}
func (ctxStub) OptionActive(_ byte) bool              { return true }
func (ctxStub) NotifyProtocolStatus()                 {}

func TestOnSubnegotiation_SplitsPackageAndPayload(t *testing.T) {
	var gotPkg string
	var gotPayload []byte
	p := New(func(pkg string, payload []byte) {
		gotPkg = pkg
		gotPayload = payload
	})

	body := []byte(`Room.Info {"name":"Foyer"}`)
	if err := p.OnSubnegotiation(ctxStub{}, body); err != nil {
		t.Fatalf("OnSubnegotiation: %v", err)
	}
	if gotPkg != "Room.Info" {
		t.Fatalf("pkg: want Room.Info got %q", gotPkg)
	}
	if string(gotPayload) != `{"name":"Foyer"}` {
		t.Fatalf("payload: got %q", gotPayload)
	}
}

func TestOnSubnegotiation_NilCallbackIsSafe(t *testing.T) {
	p := New(nil)
	if err := p.OnSubnegotiation(ctxStub{}, []byte("Core.Hello {}")); err != nil {
		t.Fatalf("OnSubnegotiation: %v", err)
	}
}

func TestOnSubnegotiation_NoPayloadWhenNoSpace(t *testing.T) {
	var gotPkg string
	var gotPayload []byte
	p := New(func(pkg string, payload []byte) {
		gotPkg = pkg
		gotPayload = payload
	})
	_ = p.OnSubnegotiation(ctxStub{}, []byte("External.Discord.Status"))
	if gotPkg != "External.Discord.Status" {
		t.Fatalf("pkg: got %q", gotPkg)
	}
	if gotPayload != nil {
		t.Fatalf("payload: want nil got %v", gotPayload)
	}
}

// TestIsDataActive_StartsFalse verifies that GMCP does not report itself as
// data-active before receiving any subnegotiations.
func TestIsDataActive_StartsFalse(t *testing.T) {
	p := New(nil)
	if p.IsDataActive() {
		t.Fatal("IsDataActive should be false before any subnegotiation")
	}
}

// TestIsDataActive_TrueAfterSubneg verifies that GMCP becomes data-active
// after OnSubnegotiation is called.
func TestIsDataActive_TrueAfterSubneg(t *testing.T) {
	p := New(nil)
	_ = p.OnSubnegotiation(ctxStub{}, []byte("Core.Hello {}"))
	if !p.IsDataActive() {
		t.Fatal("IsDataActive should be true after receiving a subnegotiation")
	}
}
