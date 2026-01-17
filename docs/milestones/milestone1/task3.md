# Task 3: The IAC Stripper (The Filter)

## Goal
Remove Telnet negotiation codes ("garbage characters") from the visible text.

## Steps
1.  **Define Telnet Constants**
    -   File: `internal/network/protocol.go`
    -   Define `IAC = 255`, `DONT = 254`, `DO = 253`, `WONT = 252`, `WILL = 251`, `SB = 250`, `SE = 240`.

2.  **Implement Byte Processor**
    -   File: `internal/network/telnet.go`
    -   Create a state machine or simple scanner:
        -   Iterate over incoming byte slice.
        -   If byte is `IAC` (0xFF):
            -   Look ahead to determine length of command (2 bytes for DO/DONT/WILL/WONT, var length for SB..SE).
            -   **Remove** these bytes from the stream effectively (copy non-IAC bytes to a new buffer or use a strict state machine filter).

3.  **Integrate into Read Path**
    -   Call the IAC stripper *before* the Decoder.

## Verification
-   Run `go run cmd/termud/main.go`
-   Verify weird symbols (`ÿ`) are gone. Text should still look like text, but cleaner.
