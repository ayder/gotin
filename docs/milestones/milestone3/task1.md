# Task 1: The Interceptor Function

## Goal
Re-route data flow from Socket -> Display to Socket -> Processor -> Display.

## Steps
1.  **Define Processor Interface**
    -   File: `internal/logic/processor.go` (new file)
    -   Function: `ProcessIncoming(data []byte) []byte` (initially pass-through).

2.  **Integrate Logic Layer**
    -   File: `cmd/termud/main.go`
    -   Connect `network` output to `logic.ProcessIncoming`, then to `ui`. (This might require refactoring the main loop to be event-driven via channels).

3.  **Refactor to Channels (Recommended for Go)**
    -   Network writes to `chan []byte`.
    -   Logic reads from `chan`, processes, writes to `ui_chan`.
    -   UI listens on `ui_chan`.

## Verification
-   Run the app.
-   Connect. Text should still appear.
-   Add a `fmt.Println("Interceptor!")` in `ProcessIncoming`. verify it prints to debug log (or stderr) when data arrives.
