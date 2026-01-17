# Task 4: The ANSI Stripper

## Goal
Ensure triggers work on colored text.

## Steps
1.  **Implement Strip Function**
    -   File: `internal/logic/util.go`
    -   Function `StripANSI(text string) string`
    -   Use regex: `\x1b\[[0-9;]*m` to replace with empty string.

2.  **Update Trigger Logic**
    -   Pass `StripANSI(line)` to the trigger matcher.
    -   Pass `original line` (with color) to the UI.

## Verification
-   Test triggering on a colored word (e.g. `[Health]`).
-   Verify trigger fires.
-   Verify UI still shows red text.
