# Task 4: The Negotiator (The Handshake)

## Goal
Politoy reply to Server requests to keep connection stable.

## Steps
1.  **Enhance IAC Processor**
    -   File: `internal/network/telnet.go`
    -   Instead of just stripping, **inspect** the command.

2.  **Implement Naive Refusal**
    -   When `IAC DO <OPTION>` is seen:
        -   Send back `IAC WONT <OPTION>`.
    -   When `IAC WILL <OPTION>` is seen:
        -   Send back `IAC DONT <OPTION>`.
    -   *Exception*: If we decide to support a specific option later (like TTYPE), we handle it here. For now, refuse everything.

3.  **Send Logic**
    -   Ensure `Client` has a `Send(bytes []byte)` method to write back to the socket.

## Verification
-   Connect to a MUD known for negotiation (e.g., t2tmud.org 9999).
-   Verify the client doesn't hang or get disconnected instantly.
-   Check debug logs (if added) to see `Sent: WONT 24` (Terminal Type) etc.
