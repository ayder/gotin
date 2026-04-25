package network

import (
	"bufio"
	"bytes"
	"compress/zlib"
	"errors"
	"io"
	"net"
	"strings"
	"testing"
	"time"
)

func TestDisconnectCallback_EOF(t *testing.T) {
	server, clientConn := net.Pipe()
	c := &Client{
		conn:    clientConn,
		reader:  bufio.NewReader(clientConn),
		decoder: NewDecoder(),
	}

	var gotErr error
	called := make(chan struct{})
	c.SetDisconnectCallback(func(err error) {
		gotErr = err
		close(called)
	})

	go c.ReadLoop()
	_ = server.Close()

	select {
	case <-called:
	case <-time.After(2 * time.Second):
		t.Fatal("disconnect callback not called within 2s")
	}
	if gotErr != nil && !errors.Is(gotErr, io.EOF) {
		t.Fatalf("expected nil or io.EOF, got %v", gotErr)
	}
}

func TestDisconnectCallback_Timeout(t *testing.T) {
	_, clientConn := net.Pipe()
	c := &Client{
		conn:    clientConn,
		reader:  bufio.NewReader(clientConn),
		decoder: NewDecoder(),
	}
	c.SetReadTimeout(50 * time.Millisecond)

	gotErr := make(chan error, 1)
	c.SetDisconnectCallback(func(err error) { gotErr <- err })

	go c.ReadLoop()

	select {
	case err := <-gotErr:
		var ne net.Error
		if !errors.As(err, &ne) || !ne.Timeout() {
			t.Fatalf("expected net.Error with Timeout()==true, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("callback not invoked within 2s")
	}
}

func TestQMethod_NoDuplicateReplyOnRepeatedWILL(t *testing.T) {
	c := &Client{}
	_, r1 := c.ProcessIAC([]byte{IAC, WILL, ECHO})
	_, r2 := c.ProcessIAC([]byte{IAC, WILL, ECHO})
	if string(r1) != string([]byte{IAC, DO, ECHO}) {
		t.Fatalf("first reply: want DO ECHO, got %v", r1)
	}
	if len(r2) != 0 {
		t.Fatalf("second reply: want silence, got %v", r2)
	}
}

func TestQMethod_NoReplyOnUnchangedDONT(t *testing.T) {
	c := &Client{}
	// We never enabled NAWS-from-server; a DONT NAWS from server is already our state.
	_, r := c.ProcessIAC([]byte{IAC, DONT, NAWS})
	if len(r) != 0 {
		t.Fatalf("want silence on DONT for already-disabled option, got %v", r)
	}
}

func TestQMethod_RefusalReplyOnUnknownOption(t *testing.T) {
	c := &Client{}
	_, r := c.ProcessIAC([]byte{IAC, WILL, MSDP})
	if string(r) != string([]byte{IAC, DONT, MSDP}) {
		t.Fatalf("want DONT MSDP for unknown option, got %v", r)
	}
}

func TestQMethod_DoSGA_ReplyWILL(t *testing.T) {
	c := &Client{}
	_, r := c.ProcessIAC([]byte{IAC, DO, SGA})
	if string(r) != string([]byte{IAC, WILL, SGA}) {
		t.Fatalf("want WILL SGA, got %v", r)
	}
}

func TestNAWS_DoNAWS_TriggersWillPlusSubnegotiation(t *testing.T) {
	c := &Client{}
	c.SetWindowSize(120, 40)

	_, resp := c.ProcessIAC([]byte{IAC, DO, NAWS})

	// Expected: IAC WILL NAWS  +  IAC SB NAWS <width-high> <width-low> <height-high> <height-low> IAC SE
	want := []byte{IAC, WILL, NAWS}
	want = append(want, c.buildNAWS()...)

	if string(resp) != string(want) {
		t.Fatalf("want %v, got %v", want, resp)
	}
}

func TestNAWS_SendNAWS_SendsEvenWhenNotNegotiated(t *testing.T) {
	server, clientConn := net.Pipe()
	defer server.Close()
	defer clientConn.Close()

	c := &Client{
		conn:    clientConn,
		reader:  bufio.NewReader(clientConn),
		decoder: NewDecoder(),
		options: newOptionTable(),
	}
	c.SetWindowSize(80, 24)

	// SendNAWS now sends regardless of negotiation state so that terminal
	// resize events are not lost when they race with the server's DO NAWS.
	want := c.buildNAWS()
	got := make([]byte, len(want))

	go c.SendNAWS()

	server.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
	n, err := io.ReadFull(server, got)
	if err != nil {
		t.Fatalf("expected NAWS subnegotiation, got err=%v n=%d", err, n)
	}
	if string(got) != string(want) {
		t.Fatalf("want %v, got %v", want, got)
	}
}

func TestNAWS_SendNAWS_SendsWhenEnabled(t *testing.T) {
	server, clientConn := net.Pipe()
	defer server.Close()
	defer clientConn.Close()

	c := &Client{
		conn:    clientConn,
		reader:  bufio.NewReader(clientConn),
		decoder: NewDecoder(),
		options: newOptionTable(),
	}
	c.SetWindowSize(100, 30)

	// Enable NAWS by processing DO NAWS
	_, resp := c.ProcessIAC([]byte{IAC, DO, NAWS})
	if len(resp) == 0 {
		t.Fatal("expected negotiation response")
	}

	// Now SendNAWS should produce a subnegotiation on the wire.
	// Write in a goroutine so the unbuffered pipe does not deadlock.
	want := c.buildNAWS()
	got := make([]byte, len(want))

	go c.SendNAWS()

	server.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
	n, err := io.ReadFull(server, got)
	if err != nil {
		t.Fatalf("expected NAWS subnegotiation, got err=%v n=%d", err, n)
	}
	if string(got) != string(want) {
		t.Fatalf("want %v, got %v", want, got)
	}
}

// fakeConn is a net.Conn backed by a byte slice; reads are non-blocking and
// returns whatever remains in the slice. Useful for deterministic protocol
// tests without goroutine scheduling races.
type fakeConn struct {
	data   []byte
	offset int
	closed bool
}

func (f *fakeConn) Read(p []byte) (int, error) {
	if f.offset >= len(f.data) {
		if f.closed {
			return 0, io.EOF
		}
		return 0, io.EOF
	}
	n := copy(p, f.data[f.offset:])
	f.offset += n
	return n, nil
}
func (f *fakeConn) Write(p []byte) (int, error)  { return len(p), nil }
func (f *fakeConn) Close() error                 { f.closed = true; return nil }
func (f *fakeConn) LocalAddr() net.Addr          { return nil }
func (f *fakeConn) RemoteAddr() net.Addr         { return nil }
func (f *fakeConn) SetDeadline(time.Time) error  { return nil }
func (f *fakeConn) SetReadDeadline(time.Time) error  { return nil }
func (f *fakeConn) SetWriteDeadline(time.Time) error { return nil }

// TestMCCP2_FullFlow verifies that when a server sends the MCCP2 start
// subnegotiation, ReadLoop correctly swaps to a zlib decompressor and
// delivers plaintext to the data callback.
func TestMCCP2_FullFlow(t *testing.T) {
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

	// Assemble everything the "server" sends into one byte slice.
	var payload []byte
	payload = append(payload, []byte("Welcome!\r\n")...)
	payload = append(payload, IAC, WILL, MCCP2)
	payload = append(payload, IAC, SB, MCCP2, IAC, SE)
	payload = append(payload, compressed.Bytes()...)

	conn := &fakeConn{data: payload, closed: true}
	c := &Client{
		conn:      conn,
		reader:    bufio.NewReader(conn),
		decoder:   NewDecoder(),
		protocols: make(map[byte]Protocol),
	}

	// Register a fake MCCP2-like protocol that swaps to a zlib reader.
	c.protocols[MCCP2] = &mccp2LikeProtocol{}

	var received []string
	c.SetDataCallback(func(data string) {
		received = append(received, data)
	})

	done := make(chan struct{})
	c.SetDisconnectCallback(func(error) { close(done) })

	go c.ReadLoop()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for ReadLoop")
	}

	// We should have received the banner AND the decompressed room text.
	all := strings.Join(received, "")
	if !strings.Contains(all, "Welcome!") {
		t.Fatalf("expected 'Welcome!' in output, got: %q", all)
	}
	if !strings.Contains(all, "small, dimly lit room") {
		t.Fatalf("expected decompressed plaintext after MCCP2 swap, got: %q", all)
	}
}

// mccp2LikeProtocol is a stand-in for the real MCCP2 protocol used in the
// integration test above.
type mccp2LikeProtocol struct{}

func (p *mccp2LikeProtocol) Name() string   { return "MCCP2-LIKE" }
func (p *mccp2LikeProtocol) Option() byte   { return MCCP2 }
func (p *mccp2LikeProtocol) Kind() Kind     { return KindHim }
func (p *mccp2LikeProtocol) OnEnable(ctx Context) error { return nil }
func (p *mccp2LikeProtocol) OnDisable(ctx Context)    {}
func (p *mccp2LikeProtocol) OnSubnegotiation(ctx Context, _ []byte) error {
	ctx.SwapReader(func(prev io.Reader) io.Reader {
		zr, err := zlib.NewReader(prev)
		if err != nil {
			panic(err)
		}
		return zr
	})
	return nil
}

// TestMCCP2_SwapMechanism verifies the core reader-swap logic without
// involving the full ReadLoop so we can isolate failures.
func TestMCCP2_SwapMechanism(t *testing.T) {
	var compressed bytes.Buffer
	w := zlib.NewWriter(&compressed)
	w.Write([]byte("hello world"))
	w.Close()

	tail := compressed.Bytes()
	base := io.MultiReader(bytes.NewReader(tail), strings.NewReader(""))

	zr, err := zlib.NewReader(base)
	if err != nil {
		t.Fatalf("zlib.NewReader: %v", err)
	}

	buf := make([]byte, 4096)
	br := bufio.NewReader(zr)
	n, err := br.Read(buf)
	t.Logf("first Read: n=%d err=%v data=%q", n, err, string(buf[:n]))

	if n == 0 {
		t.Fatal("expected decompressed bytes, got none")
	}
	if !strings.Contains(string(buf[:n]), "hello world") {
		t.Fatalf("expected 'hello world', got %q", string(buf[:n]))
	}
}
