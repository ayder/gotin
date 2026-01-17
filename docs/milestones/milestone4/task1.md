# Task 1: The Command Router

## Goal
Process local commands (`/quit`) vs server commands (`say hi`).

## Steps
1.  **Input Interception**
    -   File: `internal/input/handler.go`
    -   Function `HandleInput(text string)`
    -   Check `strings.HasPrefix(text, "/")`.

2.  **Implementation**
    -   If prefix found: Call `ProcessLocalCommand(text)`.
    -   Else: Send to Network channel.

3.  **Local Commands**
    -   `ProcessLocalCommand`: Switch on command verb.
        -   `/quit`: Exit app (`tea.Quit`).
        -   `/connect <host> <port>`: Trigger network connection.

## Verification
-   Type `/quit`. App should close.
-   Type `hi`. Should go to server (verify in server logs or echo).
