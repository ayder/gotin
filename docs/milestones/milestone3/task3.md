# Task 3: The Trigger Command System

## Goal
Implement in-client commands to creating and managing triggers dynamically.

## Steps
1.  **Update Input Handler**
    -   File: `internal/input/handler.go`
    -   Add commands:
        -   `/trigger {pattern} {response}`: Add a new trigger using brace delimiters for safe parsing.
        -   `/untrigger {pattern}`: Remove a trigger.
        -   `/triggers`: List all active triggers.

3.  **Implement Regex Group Support & Logic**
    -   Update `CheckLine` to use `pattern.FindStringSubmatch`.
    -   If matches found, perform substitution on the `Response` string (e.g., replace `$1` with first group).
    -   Use `regexp.Expand` or manual string replacement.

5.  **Loop Prevention** (New)
    -   **Problem**: Triggers triggering themselves (e.g., `look bee` -> "bee" -> trigger fires `look bee`).
    -   **Solution**:
        -   Implement a cooldown or recursion depth check.
        -   Simple approach: Track the last fired trigger time. If same trigger fires within X ms (e.g., 100ms) or N times in 1s, block it.
        -   Advanced: Pass a "recursion depth" context if internal.
        -   Chosen Strategy: Minimum Fire Interval 200ms per trigger rule.

4.  **Handle ANSI Colors** 
    -   **Goal**: Ensure triggers work on colored text.
    -   **Implement Strip Function**:
        -   File: `internal/logic/util.go`
        -   Function `StripANSI(text string) string`
        -   Use regex: `\x1b\[[0-9;]*m` to replace with empty string.
    -   **Update Logic**:
        -   Pass `StripANSI(line)` to the trigger matcher.
        -   Pass `original line` (with color) to the UI.

## Verification
-   Run client.
-   Type `/trigger ^Greetings (.*) say Hello $1`
-   Restart client (verify persistence).
-   Type `/triggers` (should see it).
-   Connect to server (or mock).
-   Receive "Greetings Traveler" (in Color!).
-   Verify client sends "say Hello Traveler".
-   Type `/untrigger ^Greetings (.*)`, verify it's gone.
