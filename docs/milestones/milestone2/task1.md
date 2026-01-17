# Task 1: The TUI Skeleton

## Goal
Create the basic visual structure using BubbleTea.

## Steps
1.  **Initialize BubbleTea Model**
    -   File: `internal/ui/model.go`
    -   Define `Model` struct containing:
        -   `viewport` (for output)
        -   `textinput` (for input)
        -   `ready` (bool)

2.  **Implement Init, Update, View**
    -   File: `internal/ui/model.go`
    -   `Init`: Return `textinput.Blink`.
    -   `Update`: Handle `tea.KeyMsg` (Ctrl+C to quit, WindowSizeMsg to resize).
    -   `View`: Render components.

3.  **Layout Management**
    -   Implement `WindowSizeMsg` handling to adjust viewport height/width dynamically.
    -   Use `lipgloss` for styling borders if needed.

## Verification
-   Run the app.
-   Verify you see an input line and an empty viewport.
-   Resize the terminal; components should adjust.
