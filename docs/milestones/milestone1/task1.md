# Task 1: The Raw Socket (The Pipe)

## Goal
Establish a TCP connection and prove data is flowing.

## Steps
1.  **Initialize Go Module** (If not already done)
    -   `go mod init dmud` (in root)

2.  **Create Connection Wrapper**
    -   File: `internal/network/telnet.go`
    -   Function: `Connect(host string, port int) (*Client, error)`
    -   Use `net.DialTimeout("tcp", ...)`

3.  **Implement Read Loop**
    -   File: `internal/network/telnet.go`
    -   Function: `(c *Client) ReadLoop()`
    -   Use a buffer (e.g., `make([]byte, 1024)`) to read from the connection.
    -   Print raw bytes to `stdout` for verification: `fmt.Printf("%q\n", buffer[:n])`

4.  **Create Main Entry Point**
    -   File: `cmd/termud/main.go`
    -   Call `network.Connect("towel.blinkenlights.nl", 23)` (or similar MUD)
    -   Start `ReadLoop()`

## Verification
-   Run `go run cmd/termud/main.go`
-   Verify output contains raw byte arrays (e.g., `b'...'` or similar Go representation).
