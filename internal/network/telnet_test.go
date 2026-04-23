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
