# Task 2: The Alias Engine

## Goal
Shorten complex commands.

## Steps
1.  **Alias Storage**
    -   File: `internal/input/alias.go`
    -   Map `Aliases map[string]string`.

2.  **Expansion Logic**
    -   In `HandleInput` (before routing):
    -   Split text into words.
    -   Check if first word is in `Aliases`.
    -   If yes, replace first word with value.

3.  **Add Command**
    -   `/alias <key> <value>`: Update map.

## Verification
-   `/alias k kill`
-   Type `k rat`.
-   Verify server receives `kill rat`.
