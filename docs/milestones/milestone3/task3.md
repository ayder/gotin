# Task 3: The Regex Trigger Engine

## Goal
Make the client react to text.

## Steps
1.  **Define Trigger Structure**
    -   File: `internal/logic/trigger.go`
    -   Struct `Trigger { Pattern *regexp.Regexp, Response string }`

2.  **Implement Matcher**
    -   List of `Triggers`.
    -   In `ProcessIncoming` (or specifically `HandleLine`), iterate triggers.
    -   If `Pattern.MatchString(line)`, send `Response` to network.

3.  **Hardcode Test Trigger**
    -   Add trigger: `Pattern: "Welcome", Response: "Thank you"` (Be careful with loops!).

## Verification
-   Connect to MUD.
-   When trigger word is received, verify client sends response (check logs or server feedback).
