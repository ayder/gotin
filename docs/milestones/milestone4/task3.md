# Task 3: Configuration Persistence

## Goal
Save settings to disk.

## Steps
1.  **Define Config Struct**
    -   `Config` struct with `Aliases`, `Triggers`, `LastHost`.

2.  **Save/Load Logic**
    -   File: `internal/config/manager.go`
    -   `Save(cfg Config)`: Marshal to JSON, write to `~/.termud/config.json`.
    -   `Load() Config`: Read file, Unmarshal.

3.  **Integration**
    -   Load on startup.
    -   Save on exit (or on change).

## Verification
-   Set alias. Quit. Restart.
-   Alias should still exist.
