# Task 2: The Echo

## Goal
Verify input handling and display updates, including password masking.

## Steps
1.  **Handle Enter Key**
    -   File: `internal/ui/model.go`
    -   In `Update`, check for `tea.KeyEnter`.
    -   Get value from `m.textinput.Value()`.
    -   Clear text input: `m.textinput.Reset()`.

2.  **Update Viewport**
    -   Pass the typed text to the viewport.
    -   `m.viewport.SetContent(oldContent + "\n" + newContent)` (or use a helper).
    -   Ensure viewport scrolls to bottom.

3.  **Handle Password Mode (Echo Negotiation)**
    -   Listen for `SetLocalEcho(bool)` events (triggered by Network layer).
    -   If `true` (normal): Set `m.textinput.EchoMode = textinput.EchoNormal`.
    -   If `false` (password): Set `m.textinput.EchoMode = textinput.EchoPassword`.

## Verification
-   Type "Hello World" and hit Enter.
-   "Hello World" should appear in the main window.
-   Trigger password mode (mock event): Input characters should be masked (`*` or invisible).
