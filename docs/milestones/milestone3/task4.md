# Task 5: The Mapper

## Goal
Implement a mapping system to track rooms, exits, and navigation paths using the `/map` command.

## Requirements

### Data Structure
-   **Room**: 
    -   ID (int/uuid)
    -   Name (string)
    -   Description (string): The full room description text.
    -   DescriptionHash (string): Hash of the description for fast lookup.
    -   Exits (map[direction]RoomID)
    -   Coordinates (x, y, z)
-   **Map**: List of Rooms, CurrentRoomID.
-   **Persistence**: Save/Load from JSON.

### Directions
Standard bidirectional pairs:
-   North (n) <-> South (s)
-   East (e) <-> West (w)
-   NorthEast (ne) <-> SouthWest (sw)
-   NorthWest (nw) <-> SouthEast (se)
-   Up (u) <-> Down (d)
-   In <-> Out

### Commands
All commands are prefixed with `/map`. When the map is created, defined paths command will be used to create and link rooms.

1.  **Creation & Loading**
    -   `/map create`: Create specific empty graph buffer.
    -   `/map create <filename>`: Load map from a JSON file.

2.  **Configuration**
    -   `/map paths`: Configure available directions (defaults to standard set above).

3.  **Mapping**
    -   `/map dig <direction> <action>`: Create a new room in the specified direction based on the action (e.g., "dig cellar {pull lever}").
    -   `/map undo`: Undo the last mapping action (delete last created room/link/dig).
    -   `/map name <room_name>`: Set the name of the current room.
    -   `/map delete <room_id> <room_name>`: Delete a specific room by ID or name.
    -   `/map goto <room_name>`: Teleport mapper state to the specified room (by name or ID).
    -   `/map link <direction> <room_id> <room_name>`: Create a one-way exit in the current room to the specified target room.

4.  **Auto-Mapping (Smart)**
    -   `/map start`: Begin tracking.
    -   **Loop Detection**: When entering a room, calculate hash. If match found in map, link to existing room instead of creating new one.
    -   **Hashing Strategy**:
        -   Description: Line starting with `\t' and first 50 chars (avoids sky/time/NPCs).
        -   Exits: Line starting with `\t` (Tab) and standard exit line format (The only obvious exits are east, north, west and south.).
        -   Hash = SHA256(Desc[0:50] + Exits).

5.  **Search & Info**
    -   `/map search <query>`: Search for rooms containing the query string (in Name or Description).
    -   `/map show`: Display the current map area in ASCII-art style.
    -   `/map info`: Display the current room info, exits, and coordinates.

6.  **Persistence**
    -   `/map exit`: Save the current map to disk and exit mapping mode.

## Implementation Steps
1.  **Define Map Logic `internal/logic/mapper`**
    -   Structs for `Room`, `Map`.
    -   Logic for adding rooms, linking, and undo stack.
2.  **ASCII Renderer**
    -   Simple grid-based renderer for `show`.
3.  **Update Handler `internal/input/handler.go`**
    -   Implement `/map` subcommand parser.
4.  **Wire to Main**
    -   Integrate into the main loop.

## Verification
-   `/map create` -> Start fresh.
-   `/map name Start` -> Room named "Start".
-   `/map dig n "dig cellar {pull lever}"` -> New room created North.
-   `/map show` -> Verify visual.
-   `/map undo` -> Verify back to start.
-   `/map exit` -> Verify `map.json` saved.
