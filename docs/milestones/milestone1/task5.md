# Task 5: ECHO Negotiation (Password Masking)

## Goal
Support standard Telnet ECHO negotiation to hide characters during password entry.

## Background
-   By default, local terminals often echo characters.
-   MUD servers send `IAC WILL ECHO` when they want to handle echoing (or hide input).
-   Client must respond `IAC DO ECHO` and **disable** local echo.
-   When password entry is done, server sends `IAC WONT ECHO`.
-   Client responds `IAC DONT ECHO` and **re-enables** local echo to show what user types.

## Steps
1.  **Update State Machine**
    -   File: `internal/network/telnet.go` (or `negotiator.go`)
    -   Handle `WILL ECHO` (Option 1).
    -   Maintain a state boolean: `ServerEcho` (default false).

2.  **Implementation**
    -   When `IAC WILL ECHO` received:
        -   Send `IAC DO ECHO`.
        -   Set `ServerEcho = true`.
        -   Emit event/callback to UI: `SetLocalEcho(false)`.
    -   When `IAC WONT ECHO` received:
        -   Send `IAC DONT ECHO`.
        -   Set `ServerEcho = false`.
        -   Emit event/callback to UI: `SetLocalEcho(true)`.



## Verification
-   Connect to MUD t2tmud.org 9999 .
-   At login screen ("Password:"), characters should appear as `*` or be invisible.
-   After login, characters should appear normal.
