package network

import (
	"bufio"
	"errors"
	"io"
	"net"
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

func TestGMCP_Dispatch(t *testing.T) {
	c := &Client{}
	var gotPkg string
	var gotPayload []byte
	c.SetGMCPCallback(func(pkg string, payload []byte) {
		gotPkg = pkg
		gotPayload = payload
	})

	// IAC SB GMCP "Room.Info {\"name\":\"Foyer\"}" IAC SE
	msg := []byte("Room.Info {\"name\":\"Foyer\"}")
	buf := []byte{IAC, SB, GMCP}
	buf = append(buf, msg...)
	buf = append(buf, IAC, SE)

	_, _ = c.ProcessIAC(buf)

	if gotPkg != "Room.Info" {
		t.Fatalf("expected pkg Room.Info, got %q", gotPkg)
	}
	if string(gotPayload) != `{"name":"Foyer"}` {
		t.Fatalf("expected payload JSON, got %q", string(gotPayload))
	}
}

func TestGMCP_AcceptWillOffer(t *testing.T) {
	c := &Client{}
	_, resp := c.ProcessIAC([]byte{IAC, WILL, GMCP})
	want := []byte{IAC, DO, GMCP}
	if string(resp) != string(want) {
		t.Fatalf("expected DO GMCP reply, got %v", resp)
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
