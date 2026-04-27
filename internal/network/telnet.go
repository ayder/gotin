package network

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"strconv"
	"time"

	"github.com/ayder/gotin/internal/mudproto/protolog"
)

// EchoCallback is called when the server's echo state changes.
// localEcho indicates whether the client should echo locally (true = show input, false = hide input).
type EchoCallback func(localEcho bool)

// Client wraps a TCP connection to a MUD server.
type Client struct {
	conn               net.Conn
	reader             io.Reader // swapped by protocols (e.g. MCCP2 zlib.Reader)
	decoder            *Decoder
	debug              bool
	serverEcho         bool                      // true when server is handling echo (client should hide input)
	echoCallback       EchoCallback              // called when echo state changes
	dataCallback       func(data string)         // called when new data arrives
	disconnectCallback func(reason error)        // called when ReadLoop exits; reason is nil for clean close
	windowWidth        int                       // terminal width for NAWS
	windowHeight       int                       // terminal height for NAWS
	readDeadline       time.Duration             // per-read timeout; defaults to 5 minutes
	pending            []byte                    // pending bytes from incomplete IAC sequences
	options            *optionTable              // per-option Q Method state (him/us)
	protocols          map[byte]Protocol         // installed subprotocols keyed by option byte
	protocolStatusCb   func(active []string)     // fires when the negotiated-protocol set changes
	pendingSwap        func(io.Reader) io.Reader // set by ctx.SwapReader, consumed by ReadLoop
	streamTail         []byte                    // bytes after the triggering IAC SE to replay into the swapped reader
	debugLog           *log.Logger
	debugFile          *os.File
	protoLog           protolog.Logger
}

// SetDebug enables or disables debug logging.
func (c *Client) SetDebug(enabled bool) {
	if enabled && c.debugLog == nil {
		f, err := os.OpenFile("gotin.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err == nil {
			c.debugFile = f
			c.debugLog = log.New(f, "", log.LstdFlags)
		}
	} else if !enabled && c.debugFile != nil {
		c.debugFile.Close()
		c.debugFile = nil
		c.debugLog = nil
	}
	c.debug = enabled
}

func (c *Client) debugf(format string, args ...any) {
	if c.debug && c.debugLog != nil {
		c.debugLog.Printf(format, args...)
	}
}

// DebugLogf writes a timestamped line to the debug log file if debug is enabled.
func (c *Client) DebugLogf(format string, args ...any) {
	c.debugf(format, args...)
}

// SetProtocolLogger installs a structured logger for raw rx chunks.
func (c *Client) SetProtocolLogger(l protolog.Logger) { c.protoLog = l }

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
						// A subneg handler may have requested a stream swap
						// (MCCP2). Everything after this IAC SE is for the
						// swapped reader, so hand off and stop processing.
						if c.pendingSwap != nil {
							if i < len(data) {
								c.streamTail = append([]byte(nil), data[i:]...)
							}
							return result, respBuf
						}
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
	if c.debug {
		c.debugf("Recv: %s %d", cmdName(cmd), option)
	}
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
			if option == NAWS {
				*respBuf = append(*respBuf, c.buildNAWS()...)
			}
			c.onUsEnabled(option)
		} else {
			*respBuf = append(*respBuf, IAC, WONT, option)
		}
	case DONT:
		if c.options.getUs(option) == optNo {
			return
		}
		c.options.setUs(option, optNo)
		*respBuf = append(*respBuf, IAC, WONT, option)
		c.onUsDisabled(option)
	}
}

// acceptHim reports whether we accept the server performing this option.
// Built-in options (ECHO, SGA) are always accepted; an installed Protocol
// with KindHim is also accepted.
func (c *Client) acceptHim(option byte) bool {
	switch option {
	case ECHO, SGA:
		return true
	}
	if p, ok := c.protocols[option]; ok && p.Kind()&KindHim != 0 {
		return true
	}
	return false
}

// acceptUs reports whether we accept performing this option ourselves.
// Built-in options (NAWS, TTYPE, SGA) are always accepted; an installed
// Protocol with KindUs is also accepted.
func (c *Client) acceptUs(option byte) bool {
	switch option {
	case NAWS, TTYPE, SGA:
		return true
	}
	if p, ok := c.protocols[option]; ok && p.Kind()&KindUs != 0 {
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
	if p, ok := c.protocols[option]; ok {
		if err := p.OnEnable(c.ctx()); err != nil && c.debug {
			c.debugf("%s OnEnable: %v", p.Name(), err)
		}
		c.notifyProtocolStatus()
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
	if p, ok := c.protocols[option]; ok {
		p.OnDisable(c.ctx())
		c.notifyProtocolStatus()
	}
}

// onUsEnabled fires OnEnable on an installed Protocol when the client-side
// Q Method state flips to YES (i.e. we replied WILL to the server's DO).
func (c *Client) onUsEnabled(option byte) {
	if p, ok := c.protocols[option]; ok {
		if err := p.OnEnable(c.ctx()); err != nil && c.debug {
			c.debugf("%s OnEnable: %v", p.Name(), err)
		}
		c.notifyProtocolStatus()
	}
}

// onUsDisabled fires OnDisable when the client-side Q Method state flips
// to NO.
func (c *Client) onUsDisabled(option byte) {
	if p, ok := c.protocols[option]; ok {
		p.OnDisable(c.ctx())
		c.notifyProtocolStatus()
	}
}

// handleSubnegotiation handles SB ... SE sequences.
// Built-in options (TTYPE) are handled inline; any option with an installed
// Protocol dispatches to its OnSubnegotiation.
func (c *Client) handleSubnegotiation(option byte, data []byte, respBuf *[]byte) {
	switch option {
	case TTYPE:
		if len(data) > 0 && data[0] == TTYPE_SEND {
			*respBuf = append(*respBuf, c.buildTTYPE()...)
		}
		return
	}
	if p, ok := c.protocols[option]; ok {
		if c.debug {
			c.debugf("Recv: SB %d len=%d", option, len(data))
		}
		if err := p.OnSubnegotiation(c.ctx(), data); err != nil && c.debug {
			c.debugf("%s OnSubnegotiation: %v", p.Name(), err)
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
		conn:      conn,
		reader:    bufio.NewReader(conn),
		decoder:   NewDecoder(),
		options:   newOptionTable(),
		protocols: make(map[byte]Protocol),
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
		if n > 0 {
			// Log the raw rx chunk pre-IAC so Phase 2 can analyse telnet-level
			// sequences as well as MUD payload. Truncated to 1 KiB so each JSON
			// line stays well under PIPE_BUF (4 KiB on Linux/macOS) and append
			// writes remain atomic when multiple log handles share gotin.log.
			if c.protoLog != nil && c.protoLog.Enabled() {
				raw := buffer[:n]
				truncated := false
				const maxLen = 1024
				if len(raw) > maxLen {
					raw = raw[:maxLen]
					truncated = true
				}
				parsed := map[string]any{"len": n}
				if truncated {
					parsed["truncated"] = true
				}
				c.protoLog.Log(protolog.Entry{
					Source: "raw",
					Dir:    "rx",
					Event:  "chunk",
					UTF8:   protolog.EncodeUTF8(raw),
					Hex:    protolog.EncodeHex(raw),
					Parsed: parsed,
				})
			}

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

			// Apply any pending reader swap requested by a subneg handler
			// (e.g. MCCP2 wrapping the stream in a zlib reader).
			if c.pendingSwap != nil {
				swap := c.pendingSwap
				tail := c.streamTail
				c.pendingSwap = nil
				c.streamTail = nil

				// CRITICAL: the old c.reader is a *bufio.Reader and may have
				// buffered compressed bytes that were read from the socket but
				// not yet returned by Read(). If we replace it without draining
				// those bytes, the zlib decompressor will miss them and produce
				// garbage.
				buffered := 0
				if br, ok := c.reader.(*bufio.Reader); ok {
					if buffered = br.Buffered(); buffered > 0 {
						peeked, _ := br.Peek(buffered)
						// Copy because Peek references bufio's internal buffer.
						copied := append([]byte(nil), peeked...)
						tail = append(tail, copied...)
					}
				}

				if c.debug {
					c.debugf("ReadLoop: reader swap — tail=%d buffered=%d total=%d", len(c.streamTail), buffered, len(tail))
				}

				var base io.Reader = c.conn
				if len(tail) > 0 {
					base = io.MultiReader(bytes.NewReader(tail), c.conn)
				}
				c.reader = bufio.NewReader(swap(base))
			}
		}

		if err != nil {
			if errors.Is(err, io.EOF) {
				exitErr = nil
				return
			}
			exitErr = err
			return
		}
	}
}

// cmdName returns the human-readable name for a Telnet command byte.
func cmdName(cmd byte) string {
	switch cmd {
	case WILL:
		return "WILL"
	case WONT:
		return "WONT"
	case DO:
		return "DO"
	case DONT:
		return "DONT"
	case SB:
		return "SB"
	case SE:
		return "SE"
	case IAC:
		return "IAC"
	default:
		return fmt.Sprintf("%d", cmd)
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
			c.debugf("Sent: %s %d", cmdName(cmd), option)

			// For subnegotiations, log the payload and skip to IAC SE.
			if cmd == SB {
				if option == NAWS && i+7 < len(responses) {
					// NAWS payload: widthHigh widthLow heightHigh heightLow [IAC SE]
					w := int(responses[i+3])<<8 | int(responses[i+4])
					h := int(responses[i+5])<<8 | int(responses[i+6])
					c.debugf("Sent: NAWS size %dx%d", w, h)
				}
				j := i + 3
				for j+1 < len(responses) {
					if responses[j] == IAC && responses[j+1] == SE {
						j += 2
						break
					}
					j++
				}
				i = j
				continue
			}
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
	rawW, rawH := c.windowWidth, c.windowHeight
	w, h := rawW, rawH
	if w <= 0 {
		w = 80
	}
	if h <= 0 {
		h = 24
	}
	widthHigh := byte(w >> 8)
	widthLow := byte(w & 0xFF)
	heightHigh := byte(h >> 8)
	heightLow := byte(h & 0xFF)

	if c.debug {
		c.debugf("buildNAWS: raw=%dx%d effective=%dx%d", rawW, rawH, w, h)
	}

	payload := []byte{widthHigh, widthLow, heightHigh, heightLow}
	result := make([]byte, 0, 3+len(payload)*2+2)
	result = append(result, IAC, SB, NAWS)
	result = append(result, c.escapeIAC(payload)...)
	result = append(result, IAC, SE)
	return result
}

// SendNAWS sends the current window size to the server via NAWS subnegotiation.
// It sends regardless of negotiation state so that terminal resizes are not lost
// when they race with the server's DO NAWS.
func (c *Client) SendNAWS() {
	if c.conn == nil || c.options == nil {
		return
	}
	negotiated := c.options.getUs(NAWS) == optYes
	c.debugf("SendNAWS: window=%dx%d negotiated=%v", c.windowWidth, c.windowHeight, negotiated)
	c.Send(c.buildNAWS())
}

// Close closes the underlying connection.
func (c *Client) Close() error {
	if c.conn == nil {
		return nil
	}
	return c.conn.Close()
}
