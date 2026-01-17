# Task 2: The Line Buffer

## Goal
Stitch fragmented packets into coherent lines.

## Steps
1.  **Implement Buffer State**
    -   File: `internal/logic/buffer.go`
    -   Struct `LineBuffer` with a `bytes.Buffer` or string field.

2.  **Buffer Logic**
    -   Method `Feed(data []byte) []string`
    -   Append data to buffer.
    -   Scan for `\n` or `\r\n`.
    -   Return complete lines.
    -   Keep incomplete suffix in buffer.

3.  **Update Processor**
    -   Use `LineBuffer` in `ProcessIncoming`.
    -   Only emit events/update UI when a *full line* is ready.

## Verification
-   Simulate fragmented packets if possible (hard to do with real server without lag).
-   Generally, ensure connection still works and lines aren't broken weirdly.
