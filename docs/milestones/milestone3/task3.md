# Task 3: The Trigger Command System

## Goal
Implement in-client commands to creating and managing triggers dynamically.

## Steps
1.  **Update Input Handler**
    -   File: `internal/input/handler.go`
    -   Add commands:
        -   `/trigger <pattern> <response>`: Add a new trigger.
        -   `/untrigger <pattern>`: Remove a trigger.
        -   `/triggers`: List all active triggers.

3.  **Implement Regex Group Support**
    -   Update `CheckLine` to use `pattern.FindStringSubmatch`.
    -   If matches found, perform substitution on the `Response` string (e.g., replace `$1` with first group).
    -   Use `regexp.Expand` or manual string replacement.

## Verification
-   Run client.
-   Type `/trigger ^Greetings (.*) say Hello $1`
-   Restart client (verify persistence).
-   Type `/triggers` (should see it).
-   Connect to server (or mock).
-   Receive "Greetings Traveler".
-   Verify client sends "say Hello Traveler".
-   Type `/untrigger ^Greetings (.*)`, verify it's gone.
