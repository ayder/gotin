# Gotin

**Gotin** is a modern, terminal-based MUD (Multi-User Dungeon) client written in Go. It leverages the [Bubble Tea](https://github.com/charmbracelet/bubbletea) framework to provide a responsive and rich Text User Interface (TUI).

## Features

-   **Modern TUI**: Scrollable viewport, command history, and responsive layout.
-   **Telnet Support**: Handles standard Telnet negotiation, including ECHO (password masking) and ANSI color stripping for triggers.
-   **Scripting**: 
    -   **Aliases**: Create shortcuts for complex commands.
    -   **Triggers**: Automate responses to game text (via config).
-   **Cross-Platform**: Runs on macOS, Linux, and Windows.

## Installation

### Prerequisites
-   Go 1.21 or higher.

### Build from Source
```bash
git clone https://github.com/yourusername/gotin.git
cd gotin
go build -o gotin ./cmd/gotin
```

## Usage

### Quick Start
Run the client and enter the "Wizard" mode to connect dynamically:
```bash
./gotin
```
Then, inside the client:
```text
/connect t2tmud.org 9999
```

### CLI Flags
You can also connect directly via command-line flags:
```bash
./gotin -host t2tmud.org -port 9999
```

-   `-host`: The MUD server hostname.
-   `-port`: The MUD server port.
-   `-debug`: Enable debug logging for Telnet negotiations (useful for developers).

## Commands

Gotin uses local commands prefixed with `/` to manage the client. All other input is sent directly to the MUD server.

| Command | Description | Example |
| :--- | :--- | :--- |
| `/connect <host> <port>` | Connect to a MUD server. | `/connect t2tmud.org 9999` |
| `/quit` (or `/q`) | Exit the application. | `/quit` |
| `/alias add <key> <value>` | Create a text alias. | `/alias add k kill` |
| `/alias remove <key>` | Remove an alias. | `/alias remove k` |
| `/alias list` | List all defined aliases. | `/alias list` |
| `/connection add <name> <host> <port> [auto]` | Save a connection alias. | `/connection add t2t t2tmud.org 9999` |
| `/connection remove <name>` | Remove a connection alias. | `/connection remove t2t` |
| `/connection list` | List saved connection aliases. | `/connection list` |
| `/trigger add <p> <r>` | Add a trigger (Regex). | `/trigger add ^Hi say Hello` |
| `/trigger remove <p>` | Remove a trigger. | `/trigger remove ^Hi` |
| `/trigger list` | List all triggers. | `/trigger list` |
| `/map <subcmd>` | Mapper commands (see below). | `/map mermaid` |
| `/help` | Show available commands. | `/help` |

## Configuration
Configuration is stored in `~/.gotin_config.json`. It is automatically created on first run.

To manage triggers in-game:
-   `/trigger add <pattern> <response>`: Add a new trigger.
-   `/trigger remove <pattern>`: Remove a trigger.
-   `/trigger list`: List all active triggers.

Example:
```text
/trigger add ^Welcome (.*) say Hello $1
```

### The Mapper
Gotin features a built-in mapping system to track your exploration.

#### Creation & Loading
| Command | Description |
| :--- | :--- |
| `/map create [filename]` | Initialize a new map (optionally loading from JSON) |
| `/map paths [directions]` | Show or set available directions (defaults: n,s,e,w,ne,sw,nw,se,u,d,in,out) |

#### Manual Mapping
| Command | Description |
| :--- | :--- |
| `/map dig <dir> <action>` | Create a new room in the given direction |
| `/map name <room_name>` | Set the current room's name |
| `/map link <dir> <room>` | Create a one-way exit to another room (by ID or name) |
| `/map teleport <room>` | Teleport to a room by ID or name |
| `/map delete <room>` | Delete a room by ID or name |
| `/map undo` | Undo last mapping action |

#### Auto-Mapping (Smart)
| Command | Description |
| :--- | :--- |
| `/map start [room]` | Begin smart auto-mapping (optionally starting at a specific room) |
| `/map stop` | Stop auto-mapping |

When auto-mapping is enabled, typing a direction (e.g., `n`, `east`) will automatically create and link rooms as you explore.

**Smart Loop Detection:**
The mapper uses a hashing strategy to detect when you've returned to a previously visited room:
-   Takes the first 50 characters of the room description (avoids weather/time/NPC variations)
-   Combines with the list of exits
-   Creates a SHA256 hash for fast comparison

When a matching hash is found, the mapper links to the existing room instead of creating a duplicate, keeping your map clean and connected.

#### Search & Info
| Command | Description |
| :--- | :--- |
| `/map mermaid [radius\|all]` | Render Mermaid graph of nearby rooms |
| `/map show` | Toggle a side-by-side terminal map pane on the right of the MUD output |
| `/map info` | Show current room details (ID, name, exits, coordinates) |
| `/map search <query>` | Find rooms by name or description |

#### Persistence
| Command | Description |
| :--- | :--- |
| `/map exit` | Save map to disk and exit mapper |

**Example Usage:**
```text
/map create world.json       # Create or load a map
/map name "Town Square"      # Name the starting room
/map start                   # Enable auto-mapping
n                            # Move north (auto-creates room)
/map name "Market Street"    # Name the new room
/map mermaid                 # Render Mermaid graph of nearby rooms
/map stop                    # Disable auto-mapping
/map exit                    # Save and exit
```

**Persistence:**
Maps are saved to the file specified in `/map create`. Triggers and Aliases are saved to `~/.gotin/config.json`.
*Note: Patterns are Go Regular Expressions.*

## License
MIT
