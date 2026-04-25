package mccp2

import (
	"bytes"
	"compress/zlib"
	"io"
	"testing"

	"github.com/ayder/gotin/internal/network"
)

// ctxStub captures SwapReader so the test can drive the swap by hand.
type ctxStub struct {
	swap func(io.Reader) io.Reader
	dbg  []string
}

func (c *ctxStub) Send(_ []byte) error                  { return nil }
func (c *ctxStub) SendSubneg(_ byte, _ []byte) error    { return nil }
func (c *ctxStub) SwapReader(f func(io.Reader) io.Reader) { c.swap = f }
func (c *ctxStub) Debug(format string, args ...any)      {}
func (c *ctxStub) OptionActive(_ byte) bool              { return true }
func (c *ctxStub) NotifyProtocolStatus()                 {}

// TestOnSubnegotiation_InstallsZlibReader verifies that MCCP2's subneg handler
// requests a reader swap that wraps the upstream reader with zlib.NewReader
// and produces the original plaintext.
func TestOnSubnegotiation_InstallsZlibReader(t *testing.T) {
	// Build a zlib-compressed payload.
	var compressed bytes.Buffer
	w := zlib.NewWriter(&compressed)
	plaintext := "You are in a small, dimly lit room.\r\n"
	if _, err := w.Write([]byte(plaintext)); err != nil {
		t.Fatalf("zlib write: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("zlib close: %v", err)
	}

	// Run the protocol's subneg handler with our capturing stub context.
	p := New()
	ctx := &ctxStub{}
	if err := p.OnSubnegotiation(ctx, nil); err != nil {
		t.Fatalf("OnSubnegotiation: %v", err)
	}
	if ctx.swap == nil {
		t.Fatal("expected SwapReader to be called")
	}

	// Apply the swap against the compressed bytes (mirrors ReadLoop's role).
	wrapped := ctx.swap(&compressed)
	out, err := io.ReadAll(wrapped)
	if err != nil {
		t.Fatalf("read from swapped reader: %v", err)
	}
	if string(out) != plaintext {
		t.Fatalf("decoded output mismatch: want %q got %q", plaintext, out)
	}
}

// TestIdentity ensures the protocol reports the correct telnet option.
func TestIdentity(t *testing.T) {
	p := New()
	if p.Name() != "MCCP2" {
		t.Fatalf("Name: want MCCP2 got %s", p.Name())
	}
	if p.Option() != network.MCCP2 {
		t.Fatalf("Option: want %d got %d", network.MCCP2, p.Option())
	}
}
