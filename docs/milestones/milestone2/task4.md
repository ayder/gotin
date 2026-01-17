# Task 4: Command History

## Goal
Persist and cycle through previous commands.

## Steps
1.  **Add History Slice**
    -   File: `internal/input/history.go` (or in UI model for simplicity initially).
    -   Store `[]string` of sent commands.
    -   Store `historyIndex int`.

2.  **Handle Up/Down Keys**
    -   In `Update`:
        -   `KeyUp`: Decrement index, set textinput value to `history[index]`.
        -   `KeyDown`: Increment index, set textinput value.

## Verification
-   Type "A", "B", "C".
-   Press Up. input should be "C".
-   Press Up. input should be "B".
