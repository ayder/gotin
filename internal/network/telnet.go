package network

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"time"
)

// EchoCallback is called when the server's echo state changes.
// localEcho indicates whether the client should echo locally (true = show input, false = hide input).
type EchoCallback func(localEcho bool)

// Client wraps a TCP connection to a MUD server.
type Client struct {
	conn               net.Conn
	reader             *bufio.Reader
	decoder            *Decoder
	debug              bool
	serverEcho         bool              // true when server is handling echo (client should hide input)
	echoCallback       EchoCallback      // called when echo state changes
	dataCallback       func(data string) // called when new data arrives
	disconnectCallback func(reason error) // called when ReadLoop exits; reason is nil for clean close
	gmcpCallback       GMCPCallback      // called when a GMCP subnegotiation arrives
	windowWidth        int               // terminal width for NAWS
	windowHeight       int               // terminal height for NAWS
	readDeadline       time.Duration     // per-read timeout; defaults to 5 minutes
	pending            []byte            // pending bytes from incomplete IAC sequences
	options            *optionTable      // per-option Q Method state (him/us)
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

// SetDisconnectCallback sets the callback invoked when ReadLoop exits.
// reason is nil on clean EOF, or the concrete net/os error for timeouts
// and transport failures.
func (c *Client) SetDisconnectCallback(callback func(reason error)) {
	c.disconnectCallback = callback
}

// SetGMCPCallback registers a handler for inbound GMCP subnegotiations.
func (c *Client) SetGMCPCallback(cb GMCPCallback) {
	c.gmcpCallback = cb
}

// SetReadTimeout overrides the per-read deadline used by ReadLoop.
// Defaults to 5 minutes; pass 0 to disable.
func (c *Client) SetReadTimeout(d time.Duration) {
	c.readDeadline = d
}

// Send writes raw bytes to the connection.
func (c *Client) Send(data []byte) error {
	_, err := c.conn.Write(data)
	return err
}

// ProcessIAC processes Telnet IAC sequences from the input.
// It returns:
// - cleanData: the input with IAC sequences removed
// - responses: negotiation responses to send back
func (c *Client) ProcessIAC(data []byte) (cleanData []byte, responses []byte) {
	// Prepend any pending data from previous reads
	if len(c.pending) > 0 {
		data = append(c.pending, data...)
		c.pending = nil
	}

	if len(data) == 0 {
		return data, nil
	}

	result := make([]byte, 0, len(data))
	var respBuf []byte
	i := 0

	for i < len(data) {
		b := data[i]
		if b == IAC {
			// Found IAC, need at least one more byte
			if i+1 >= len(data) {
				// IAC at end of buffer, save for next read
				c.pending = data[i:]
				break
			}

			cmd := data[i+1]
			switch cmd {
			case IAC:
				// IAC IAC = escaped 0xFF, output single 0xFF
				result = append(result, IAC)
				i += 2
				continue
			case DO, WILL, WONT, DONT:
				if i+2 < len(data) {
					option := data[i+2]
					c.handleNegotiation(cmd, option, &respBuf)
					i += 3
				} else {
					c.pending = data[i:]
					return result, respBuf
				}
				continue
			case SB:
				// Subnegotiation: IAC SB <option> ... IAC SE
				if i+2 < len(data) {
					option := data[i+2]
					foundSE := false
					j := i + 3
					for j < len(data) {
						if data[j] == IAC && j+1 < len(data) && data[j+1] == SE {
							foundSE = true
							break
						}
						j++
					}
					if foundSE {
						subData := c.unescapeSubneg(data[i+3 : j])
						c.handleSubnegotiation(option, subData, &respBuf)
						i = j + 2
					} else {
						c.pending = data[i:]
						return result, respBuf
					}
				} else {
					c.pending = data[i:]
					return result, respBuf
				}
				continue
			default:
				i += 2
				continue
			}
		}

		// Handle CR NUL / CR LF normalization
		if b == '\r' {
			if i+1 < len(data) {
				next := data[i+1]
				if next == '\n' {
					// CR LF -> \n
					result = append(result, '\n')
					i += 2
				} else if next == 0 {
					// CR NUL -> CR
					result = append(result, '\r')
					i += 2
				} else {
					// Stray CR, keep it or normalize? RFC says CR should be followed by LF or NUL.
					// We'll keep it for robustness.
					result = append(result, '\r')
					i++
				}
			} else {
				// CR at end of buffer, wait for next byte to normalize
				c.pending = data[i:]
				break
			}
		} else if b == 0 {
			// Drop stray NUL bytes
			i++
		} else {
			// Regular byte
			result = append(result, b)
			i++
		}
	}

	return result, respBuf
}

// handleNegotiation handles DO/DONT/WILL/WONT commands using a simplified
// RFC 1143 Q Method state machine (receive-side only).
func (c *Client) handleNegotiation(cmd, option byte, respBuf *[]byte) {
	if c.options == nil {
		c.options = newOptionTable()
	}
	switch cmd {
	case WILL:
		if c.options.getHim(option) == optYes {
			return // already enabled; stay silent
		}
		if c.acceptHim(option) {
			c.options.setHim(option, optYes)
			*respBuf = append(*respBuf, IAC, DO, option)
			c.onHimEnabled(option)
		} else {
			// Refuse once. RFC 1143 says a DONT reply here is fine; we do not
			// memoize the refusal because him remains optNo.
			*respBuf = append(*respBuf, IAC, DONT, option)
		}
	case WONT:
		if c.options.getHim(option) == optNo {
			return // already disabled; stay silent
		}
		c.options.setHim(option, optNo)
		*respBuf = append(*respBuf, IAC, DONT, option)
		c.onHimDisabled(option)
	case DO:
		if c.options.getUs(option) == optYes {
			return
		}
		if c.acceptUs(option) {
			c.options.setUs(option, optYes)
			*respBuf = append(*respBuf, IAC, WILL, option)
		} else {
			*respBuf = append(*respBuf, IAC, WONT, option)
		}
	case DONT:
		if c.options.getUs(option) == optNo {
			return
		}
		c.options.setUs(option, optNo)
		*respBuf = append(*respBuf, IAC, WONT, option)
	}
}

// acceptHim reports whether we accept the server performing this option.
func (c *Client) acceptHim(option byte) bool {
	switch option {
	case ECHO, SGA, GMCP:
		return true
	}
	return false
}

// acceptUs reports whether we accept performing this option ourselves.
func (c *Client) acceptUs(option byte) bool {
	switch option {
	case NAWS, TTYPE, SGA:
		return true
	}
	return false
}

// onHimEnabled fires side-effects when server-side state flips to YES.
func (c *Client) onHimEnabled(option byte) {
	if option == ECHO {
		c.serverEcho = true
		if c.echoCallback != nil {
			c.echoCallback(false)
		}
	}
}

// onHimDisabled fires side-effects when server-side state flips to NO.
func (c *Client) onHimDisabled(option byte) {
	if option == ECHO && c.serverEcho {
		c.serverEcho = false
		if c.echoCallback != nil {
			c.echoCallback(true)
		}
	}
}

// handleSubnegotiation handles SB ... SE sequences.
func (c *Client) handleSubnegotiation(option byte, data []byte, respBuf *[]byte) {
	switch option {
	case TTYPE:
		if len(data) > 0 && data[0] == TTYPE_SEND {
			*respBuf = append(*respBuf, c.buildTTYPE()...)
		}
	case GMCP:
		if c.gmcpCallback != nil {
			pkg, payload := splitGMCP(data)
			c.gmcpCallback(pkg, payload)
		}
	}
}

// unescapeSubneg removes IAC-escaping from subnegotiation data.
func (c *Client) unescapeSubneg(data []byte) []byte {
	result := make([]byte, 0, len(data))
	for i := 0; i < len(data); i++ {
		if data[i] == IAC && i+1 < len(data) && data[i+1] == IAC {
			result = append(result, IAC)
			i++
		} else {
			result = append(result, data[i])
		}
	}
	return result
}

// escapeIAC doubles any IAC (0xFF) bytes in the data for Telnet transmission.
func (c *Client) escapeIAC(data []byte) []byte {
	result := make([]byte, 0, len(data))
	for _, b := range data {
		result = append(result, b)
		if b == IAC {
			result = append(result, IAC)
		}
	}
	return result
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
		reader:  bufio.NewReader(conn),
		decoder: NewDecoder(),
		options: newOptionTable(),
	}, nil
}

// ReadLoop continuously reads data from the connection.
// It sends decoded text to the dataCallback if set, or prints to stdout.
func (c *Client) ReadLoop() {
	buffer := make([]byte, 4096)
	var exitErr error
	defer func() {
		if c.disconnectCallback != nil {
			c.disconnectCallback(exitErr)
		}
	}()

	for {
		// Set read deadline to handle stale connections
		deadline := c.readDeadline
		if deadline <= 0 {
			deadline = 5 * time.Minute
		}
		c.conn.SetReadDeadline(time.Now().Add(deadline))

		n, err := c.reader.Read(buffer)
		if err != nil {
			if errors.Is(err, io.EOF) {
				// Clean disconnect
				exitErr = nil
				return
			}
			// Other network errors
			exitErr = err
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
				if werr := c.Send(responses); werr != nil {
					// Failed to send negotiation response, likely connection lost
					exitErr = werr
					return
				}
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
	for i := 0; i+2 <= len(responses); {
		if responses[i] == IAC {
			if i+2 >= len(responses) {
				break
			}
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
			i += 3
		} else {
			i++
		}
	}
}

// SetWindowSize sets the terminal dimensions for NAWS reporting.
func (c *Client) SetWindowSize(width, height int) {
	c.windowWidth = width
	c.windowHeight = height
}

// buildTTYPE returns a TTYPE subnegotiation response with xterm-256color.
func (c *Client) buildTTYPE() []byte {
	termType := []byte("xterm-256color")
	result := make([]byte, 0, 6+len(termType))
	result = append(result, IAC, SB, TTYPE, TTYPE_IS)
	result = append(result, c.escapeIAC(termType)...)
	result = append(result, IAC, SE)
	return result
}

// buildNAWS returns a NAWS subnegotiation with the current window size.
func (c *Client) buildNAWS() []byte {
	w := c.windowWidth
	if w <= 0 {
		w = 80
	}
	h := c.windowHeight
	if h <= 0 {
		h = 24
	}
	widthHigh := byte(w >> 8)
	widthLow := byte(w & 0xFF)
	heightHigh := byte(h >> 8)
	heightLow := byte(h & 0xFF)

	payload := []byte{widthHigh, widthLow, heightHigh, heightLow}
	result := make([]byte, 0, 3+len(payload)*2+2)
	result = append(result, IAC, SB, NAWS)
	result = append(result, c.escapeIAC(payload)...)
	result = append(result, IAC, SE)
	return result
}

// SendNAWS sends the current window size to the server via NAWS subnegotiation.
func (c *Client) SendNAWS() {
	if c.conn != nil {
		c.Send(c.buildNAWS())
	}
}

// Close closes the underlying connection.
func (c *Client) Close() error {
	if c.conn == nil {
		return nil
	}
	return c.conn.Close()
}
