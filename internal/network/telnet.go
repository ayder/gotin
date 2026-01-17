package network

import (
	"fmt"
	"net"
	"strconv"
	"time"
)

// EchoCallback is called when the server's echo state changes.
// localEcho indicates whether the client should echo locally (true = show input, false = hide input).
type EchoCallback func(localEcho bool)

// Client wraps a TCP connection to a MUD server.
// Client wraps a TCP connection to a MUD server.
type Client struct {
	conn         net.Conn
	decoder      *Decoder
	debug        bool
	serverEcho   bool              // true when server is handling echo (client should hide input)
	echoCallback EchoCallback      // called when echo state changes
	dataCallback func(data string) // called when new data arrives
}

// SetDebug enables or disables debug logging.
func (c *Client) SetDebug(enabled bool) {
	c.debug = enabled
}

// SetEchoCallback sets the callback function for echo state changes.
func (c *Client) SetEchoCallback(callback EchoCallback) {
	c.echoCallback = callback
}

// SetDataCallback sets the callback function for incoming data.
func (c *Client) SetDataCallback(callback func(data string)) {
	c.dataCallback = callback
}

// Send writes raw bytes to the connection.
func (c *Client) Send(data []byte) error {
	_, err := c.conn.Write(data)
	return err
}

// ProcessIAC processes Telnet IAC sequences from the input.
// It returns:
// - cleanData: the input with IAC sequences removed
// - responses: negotiation responses to send back (WONT for DO, DONT for WILL)
//
// It handles:
// - IAC followed by DO/DONT/WILL/WONT + option byte (3 bytes total)
// - IAC followed by SB ... SE (subnegotiation, variable length)
// - IAC IAC (escaped 0xFF, outputs single 0xFF)
// - IAC followed by other commands (2 bytes total)
//
// Special handling for ECHO option:
// - WILL ECHO: Server will handle echo, respond with DO ECHO, disable local echo
// - WONT ECHO: Server stops echoing, respond with DONT ECHO, enable local echo
func (c *Client) ProcessIAC(data []byte) (cleanData []byte, responses []byte) {
	if len(data) == 0 {
		return data, nil
	}

	result := make([]byte, 0, len(data))
	var respBuf []byte
	i := 0

	for i < len(data) {
		if data[i] != IAC {
			// Regular byte, keep it
			result = append(result, data[i])
			i++
			continue
		}

		// Found IAC, need at least one more byte
		if i+1 >= len(data) {
			// IAC at end of buffer, discard it (incomplete sequence)
			break
		}

		cmd := data[i+1]

		switch cmd {
		case IAC:
			// IAC IAC = escaped 0xFF, output single 0xFF
			result = append(result, IAC)
			i += 2

		case DO:
			// Server asks us to DO something, we refuse with WONT
			if i+2 < len(data) {
				option := data[i+2]
				respBuf = append(respBuf, IAC, WONT, option)
				i += 3
			} else {
				i = len(data)
			}

		case WILL:
			// Server says it WILL do something
			if i+2 < len(data) {
				option := data[i+2]
				if option == ECHO {
					// Server will handle echo - accept and disable local echo
					respBuf = append(respBuf, IAC, DO, option)
					if !c.serverEcho {
						c.serverEcho = true
						if c.echoCallback != nil {
							c.echoCallback(false) // disable local echo
						}
					}
				} else {
					// Refuse other options with DONT
					respBuf = append(respBuf, IAC, DONT, option)
				}
				i += 3
			} else {
				i = len(data)
			}

		case WONT:
			// Server says it WONT do something
			if i+2 < len(data) {
				option := data[i+2]
				if option == ECHO {
					// Server stops echoing - respond and enable local echo
					respBuf = append(respBuf, IAC, DONT, option)
					if c.serverEcho {
						c.serverEcho = false
						if c.echoCallback != nil {
							c.echoCallback(true) // enable local echo
						}
					}
				}
				// For other options, just acknowledge by not responding
				i += 3
			} else {
				i = len(data)
			}

		case DONT:
			// Server refuses, we just acknowledge by ignoring
			if i+2 < len(data) {
				i += 3
			} else {
				i = len(data)
			}

		case SB:
			// Subnegotiation: IAC SB ... IAC SE
			// Skip until we find IAC SE
			i += 2 // Skip IAC SB
			for i < len(data) {
				if data[i] == IAC && i+1 < len(data) && data[i+1] == SE {
					i += 2 // Skip IAC SE
					break
				}
				i++
			}

		default:
			// Other 2-byte commands (like IAC NOP, IAC GA, etc.)
			i += 2
		}
	}

	return result, respBuf
}

// Connect establishes a TCP connection to the specified host and port.
func Connect(host string, port int) (*Client, error) {
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	conn, err := net.DialTimeout("tcp", addr, 10*time.Second)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to %s: %w", addr, err)
	}
	return &Client{
		conn:    conn,
		decoder: NewDecoder(),
	}, nil
}

// ReadLoop continuously reads data from the connection.
// It sends decoded text to the dataCallback if set, or prints to stdout.
func (c *Client) ReadLoop() {
	buffer := make([]byte, 1024)
	for {
		n, err := c.conn.Read(buffer)
		if err != nil {
			// Signal connection closed? For now just handle error/close
			return
		}
		if n > 0 {
			// Process IAC sequences and get negotiation responses
			clean, responses := c.ProcessIAC(buffer[:n])

			// Send negotiation responses back to server
			if len(responses) > 0 {
				if c.debug {
					c.logNegotiations(responses)
				}
				c.Send(responses)
			}

			if len(clean) > 0 {
				text := c.decoder.Decode(clean)
				if c.dataCallback != nil {
					c.dataCallback(text)
				} else {
					fmt.Print(text)
				}
			}
		}
	}
}

// logNegotiations logs the negotiation responses being sent.
func (c *Client) logNegotiations(responses []byte) {
	for i := 0; i+2 < len(responses); i += 3 {
		if responses[i] == IAC {
			cmd := responses[i+1]
			option := responses[i+2]
			cmdName := ""
			switch cmd {
			case WILL:
				cmdName = "WILL"
			case WONT:
				cmdName = "WONT"
			case DO:
				cmdName = "DO"
			case DONT:
				cmdName = "DONT"
			}
			fmt.Printf("[DEBUG] Sent: %s %d\n", cmdName, option)
		}
	}
}

// Close closes the underlying connection.
func (c *Client) Close() error {
	return c.conn.Close()
}
