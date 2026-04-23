# TermMud

**TermMud** is a modern, terminal-based MUD (Multi-User Dungeon) client written in Go. It leverages the [Bubble Tea](https://github.com/charmbracelet/bubbletea) framework to provide a responsive and rich Text User Interface (TUI).

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
git clone https://github.com/yourusername/dmud.git
cd dmud
go build -o termud ./cmd/termud
```

## Usage

### Quick Start
Run the client and enter the "Wizard" mode to connect dynamically:
```bash
./termud
```
Then, inside the client:
```text
/connect t2tmud.org 9999
```

### CLI Flags
You can also connect directly via command-line flags:
```bash
./termud -host t2tmud.org -port 9999
```

-   `-host`: The MUD server hostname.
-   `-port`: The MUD server port.
-   `-debug`: Enable debug logging for Telnet negotiations (useful for developers).

## Commands

TermMud uses local commands prefixed with `/` to manage the client. All other input is sent directly to the MUD server.

| Command | Description | Example |
| :--- | :--- | :--- |
| `/connect <host> <port>` | Connect to a MUD server. | `/connect t2tmud.org 9999` |
| `/quit` (or `/q`) | Exit the application. | `/quit` |
| `/alias <key> <value>` | Create a text alias. | `/alias k kill` |
| `/unalias <key>` | Remove an alias. | `/unalias k` |
| `/aliases` | List all defined aliases. | `/aliases` |
| `/trigger <p> <r>` | Add a trigger (Regex). | `/trigger ^Hi say Hello` |
| `/triggers` | List all triggers. | `/triggers` |
| `/map <subcmd>` | Mapper commands (see below). | `/map show` |
| `/help` | Show available commands. | `/help` |

## Configuration
Configuration is stored in `~/.dmud_config.json`. It is automatically created on first run.

To manage triggers in-game:
-   `/trigger <pattern> <response>`: Add a new trigger.
-   `/untrigger <pattern>`: Remove a trigger.
-   `/triggers`: List all active triggers.

Example:
```text
/trigger ^Welcome (.*) say Hello $1
```

### The Mapper
TermMud features a built-in mapping system to track your exploration.

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
| `/map goto <room>` | Teleport to a room by ID or name |
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
| `/map show` | Display ASCII map of the immediate area |
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
/map show                    # View ASCII map
/map stop                    # Disable auto-mapping
/map exit                    # Save and exit
```

**Persistence:**
Maps are saved to the file specified in `/map create`. Triggers and Aliases are saved to `~/.termud/config.json`.
*Note: Patterns are Go Regular Expressions.*

## License
MIT
