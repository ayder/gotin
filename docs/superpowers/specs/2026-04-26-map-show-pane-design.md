# /map show — Side-by-Side Map Pane (Design)

**Date:** 2026-04-26
**Status:** Draft for implementation
**Replaces:** Nothing (the previous `/map show` was renamed to `/map mermaid` and remains as a one-shot Mermaid dump)

## 1. Goal

Render the in-memory mapper graph as a live, terminal-native map pane drawn to the right of the MUD output. Inspired visually by the corebound zone viewer (yellow boxes, black lines, current room marker). The pane updates in place as the player walks; layers (Z levels and in/out portals) auto-switch with the current room.

## 2. Non-Goals

- Zoom levels.
- Mouse interaction.
- Persistent pane layout across sessions (always closed on launch).
- Editing the map from inside the pane (digs/links/teleports stay on the input line).
- Cross-layer visualisation (one layer at a time).

## 3. User-Facing Behaviour

### Toggle
- `/map show` toggles the pane visible/hidden. Status line confirms toggle.
- `Esc` while focused inside the pane closes it; focus stays on the MUD input line throughout — `Esc` only closes the pane when no other modal is open and the input line is empty.

### Layout
- Side-by-side split. MUD content viewport keeps the left side; map pane occupies a fixed-width strip on the right.
- Map pane width: `clamp(ceil(termWidth/3), 24, 48)` columns. The chat viewport must keep at least 40 columns; if `termWidth < 24 + 1 + 40 = 65`, `/map show` is rejected with a status message ("terminal too narrow for /map show") and the pane is not toggled on.
- A 1-column vertical separator (`│`) sits between the two panes.
- The input line spans the full terminal width (does not split).

### Pane Header
- Line 1: layer label — `<Layer Name> [Z=<z>]` (`Z=…` is omitted when z==0 and the layer has no other Z; included otherwise).
- Line 2: current room name, truncated to pane width.

### Pane Body
- Map drawing area. Cell pitch fixed at `(2 cols, 2 rows)`.
- Pan offset stored in `(col, row)` cells (not coordinate units). Default offset: pane is centred on the current room every time `/map show` is opened or the player moves to a different room.

### Pane Footer (1 line)
- A single status line listing layer-bridge exits from the *current room only*:
  `↑ stairs up | ↓ cellar | ▶ inn | ◀ courtyard | dig: u/d/in/out`
- Only directions that exist on the current room are listed; `dig:` enumerates whichever of u/d/in/out are NOT yet linked, hinting that `/map dig <dir>` would create a layer bridge there. If the current room has none of these, footer reads `(no layer bridges)`.

### Pane Keys (only active when pane is visible AND focus mode = pane)
- Pane focus is implicit when the input line is empty and the player presses a movement-pan key. We treat `h/j/k/l` and arrow keys as pane navigation only when the input is empty; if the user is mid-typing, those characters go to the textinput as normal.
- `h` / `←`: pan view 2 cols left (one cell pitch).
- `l` / `→`: pan view 2 cols right.
- `k` / `↑`: pan view 2 rows up.
- `j` / `↓`: pan view 2 rows down.
- `c`: recenter on current room.
- `[`: switch to the previous layer (see §4.4).
- `]`: switch to the next layer.
- `Esc`: close the pane.

When the pane is closed, none of these keys have special meaning; arrow keys behave exactly as they do today.

## 4. Architecture

### 4.1 Package Boundary
A new package `internal/ui/mappane` contains a *pure renderer*:

```go
package mappane

type View struct {
    PaneWidth   int          // total cols including header/footer
    PaneHeight  int          // total rows
    Map         *mapper.Map  // snapshot, may be nil
    CurrentID   string
    PanOffset   Point        // in cell units (col, row)
    LayerKey    string       // representative room ID of layer to render; "" = current room's layer
}

type Point struct{ Col, Row int }

// Render returns a styled string sized exactly PaneWidth x PaneHeight.
func Render(v View) string
```

`Render` is pure: same inputs → same output. No goroutines, no I/O.

### 4.2 Engine Snapshot
Add to `internal/mapper/mapper.go`:

```go
// Snapshot returns a deep-copied *Map plus the current room ID. Safe for
// rendering off-engine without holding e.mu. Returns nil, "" if no map.
func (e *Engine) Snapshot() (*Map, string)
```

Implementation: `e.mu.RLock()` → `e.data.deepCopy()` → return. The renderer never holds the engine lock.

### 4.3 Layer Resolution
Inside `mappane`:

```go
// LayerOf returns the set of room IDs reachable from start using only the
// eight cardinal/diagonal exits (N/S/E/W/NE/NW/SE/SW). Up/Down/In/Out are
// NOT traversed.
func LayerOf(m *mapper.Map, start string) map[string]struct{}

// AllLayers returns one representative ID per distinct connected component
// (under cardinal exits), sorted by representative ID for determinism. Used
// by '[' / ']' navigation.
func AllLayers(m *mapper.Map) []string
```

Layer label derivation (computed once at render time, not stored):
1. Take the multiset of room names in the layer.
2. The layer's *primary* name = mode of names, ignoring empty / "New Room" / placeholder names; fall back to the current room's name; final fallback `"Unnamed Layer"`.
3. Z annotation: if every room in the layer has the same Z, suffix `[Z=<z>]` only when z != 0. If the layer spans Z values (rare — only when a non-cardinal traversal somehow placed siblings on different Z), suffix `[Z=mixed]`.

### 4.4 Layer Cursor
- Default `LayerCursor = -1`: render the current room's layer (auto-follow).
- `[` / `]` switch to the previous / next layer. Each keystroke recomputes `AllLayers(m)`, finds the index of the currently rendered layer's representative ID inside the fresh list, then steps -1 / +1 with wraparound. The model stores the *representative ID*, not a numeric index, so insertions or deletions between keystrokes do not break navigation.
- While a fixed layer is being viewed, the layer label is suffixed with `(viewing remote)` and auto-follow is suspended.
- `c` clears the fixed layer (cursor returns to -1, auto-follow on) and recenters.
- A new room created on a different layer than the one currently viewed clears the cursor automatically — the pane snaps to the player.

### 4.5 UI Integration
In `internal/ui/model.go`:
- New fields:
  - `mapPaneVisible bool`
  - `mapPanOffset mappane.Point`
  - `mapPaneLayerKey string`
  - `mapEngineSnapshot func() (*mapper.Map, string)` (closure injected at construction)
- `WindowSizeMsg` handler: when `mapPaneVisible`, recompute `paneWidth` and shrink the chat viewport to `width - paneWidth - 1`. When invisible, full width.
- View(): when visible, build the pane string via `mappane.Render` and join with `lipgloss.JoinHorizontal(lipgloss.Top, leftView, separator, paneView)`.
- Toggle hook: when `/map show` fires, the command handler emits a new `ui.MapPaneToggleMsg`; the model flips `mapPaneVisible`, recomputes sizes, and recenters.

### 4.6 Auto-Recenter Trigger
- The mapper already updates `CurrentRoom` inside `HandleRoomBlock`, `Goto`, `Dig`, `Undo`, etc. After any successful map mutation, the Session is informed via existing status messages. We piggyback on those: each StatusMsg starting with `[Map]` triggers a `mapPaneRecenterMsg` send to the model. The model only recenters if the *current room ID* differs from the last rendered ID.

## 5. Render Geometry

### 5.1 Coordinate → Screen Mapping
Within the pane body (after subtracting header rows and footer row):
```
bodyCols = paneWidth - 2     // 1-col left/right padding
bodyRows = paneHeight - 3    // 2 header rows + 1 footer row
center   = (bodyCols/2, bodyRows/2) // in cell units, snapped to even
```
For a room at `(rx, ry)` and the rendered current-room coord `(cx, cy)`:
```
screenCol = center.Col + 2*(rx - cx) - panOffset.Col
screenRow = center.Row + 2*(cy - ry) - panOffset.Row   // Y inverted: north up
```
Y is inverted because the mapper uses `+y == north` and terminals number rows downward.

### 5.2 Glyphs
- Room normal: `■` (U+25A0).
- Room current: `▣` (U+25A3) plus inverse style.
- Room placeholder (a known target with no name yet): `□` (U+25A1).
- Cardinal links: horizontal `─`, vertical `│`.
- Diagonals: NE/SW use `╱`, NW/SE use `╲`.
- Bridge badges overlaid on a room cell glyph occupy the four corners of an imagined 3x3 around the cell, but since each room is a single char, badges are drawn as a *suffix label* in the immediately-adjacent gutter:
  - `↑` at `(screenCol, screenRow-1)` only if no `│` is already there from a north exit.
  - `↓` at `(screenCol, screenRow+1)` only if no `│` is already there from a south exit.
  - `▶` at `(screenCol+1, screenRow)` only if no `─` is already there from an east exit.
  - `◀` at `(screenCol-1, screenRow)` only if no `─` is already there from a west exit.
- If both a cardinal link and a bridge would occupy the same gutter, the cardinal link wins (data adjacency is higher signal); the bridge surfaces in the footer status line instead.

### 5.3 Connectivity Rule
For every room `R` in the layer:
- For each direction `D` in {N, S, E, W, NE, NW, SE, SW}:
  - Look up `R.Exits[D]`; if it points to room `R'` AND `R'` is also in the layer AND `R'.X / R'.Y` matches the expected `+1/-1` offset from `R.X/R.Y`, draw the link char at the gutter cell.
  - If `R.Exits[D]` exists but `R'` does not satisfy both conditions, skip the link entirely (the data is inconsistent and we do not invent geometry).
- A link char is drawn exactly once per edge: the renderer iterates rooms in sorted order and draws each edge from the lower ID side only.

### 5.4 Coordinate Overlap
If two distinct rooms in the same layer share `(X, Y)`:
- Sort by ID for determinism.
- Place the first room at the computed `(screenCol, screenRow)`.
- Nudge the second by `+1` col only (no link char between them — they have no edge by definition or they would share an ID via loop detection).
- Overlay `!` at `(screenCol+2, screenRow)` to flag the overlap.
- More than two collisions: nudge each subsequent room by an additional `+1` col; `!` shown once.

This is rare in practice (Phase 2 hashing should prevent most cases) and exists only to keep render deterministic when data is dirty.

### 5.5 Off-Pane Rooms
- Rooms whose mapped screen position falls outside the body rectangle are silently clipped.
- If the current room is clipped (because the user has panned away), draw a `*` arrow indicator at the pane edge in the direction of the current room.

## 6. Data Flow

```
keypress / map mutation
        │
        ▼
  command.Map* ──► engine.* ──► engine.Snapshot()
                                       │
                                       ▼
                               mappane.Render(View{...})
                                       │
                                       ▼
                          model.View() composes left+right
```

The engine never calls into the renderer. The renderer never mutates the engine. The model owns the snapshot lifecycle and refreshes via Bubble Tea messages.

## 7. Error Handling

- `Snapshot()` returns `(nil, "")` when no map → renderer draws an empty pane with body line `"(no map — try /map create)"`.
- Snapshot non-nil but `CurrentID` empty / not in `Rooms` → body line `"(current room missing from map)"`.
- Layer empty (current room exists but has no layer-mates and itself is excluded somehow) → render just the single room glyph centered.
- Pane width less than the floor (24) at toggle time → reject toggle with status message; do not partial-render.
- Pane height less than 5 (header + body + footer minimum) at any time → renderer outputs a single line `"pane too short"` padded to width.

## 8. Testing

### 8.1 Unit — `internal/ui/mappane/render_test.go`
Each test builds a small `*mapper.Map` fixture and asserts the exact rendered string (golden compare via raw multi-line literals).

Coverage cases (one fixture per case unless noted):
1. Single room layer — current room glyph centered.
2. Two cardinal-linked rooms (E) — `▣─■` substring.
3. Two diagonally linked rooms (NE) — gutter cell carries `╱` at the right offset.
4. Mixed grid: 3x3 with all eight neighbours connected — verifies all four diagonal characters and all cardinal links coexist without collision.
5. Edge omitted when data does not match: build a map where `A.exits[E]=B` but `B.X != A.X+1` — assert no `─` appears between them.
6. Two layers — start room on layer A connected by an `up` exit to a room on layer B; render with `LayerKey=""` shows only layer A; `LayerKey=<repIDofB>` shows only layer B.
7. Bridge overlay — current room with one `up` exit and one `north` cardinal exit: assert `│` north (cardinal wins) and `↑` is absent there but the footer lists `↑`.
8. Coord overlap — two distinct rooms share `(0,0)`: second is nudged `+1` col, `!` overlay present.
9. Pan offset — same map as case 4 but `PanOffset = {Col:2, Row:0}`: rooms shift accordingly, current-room arrow indicator appears at the left edge if the current room is clipped.
10. Invalid input — `Map=nil` and `CurrentID=""` both produce the documented fallback strings.

### 8.2 Unit — `internal/ui/mappane/layer_test.go`
- `LayerOf` does not traverse u/d/in/out.
- `AllLayers` returns deterministic order across calls.
- Layer-label heuristic: most-common-name wins; ties broken by lexical order.

### 8.3 Integration — `internal/ui/model_test.go`
Drive the `tea.Model` with `WindowSizeMsg` then a synthetic `MapPaneToggleMsg`:
- Pane invisible → full-width chat viewport.
- Pane visible at 80x24 → chat viewport width = 80 - paneWidth - 1, pane width per formula.
- Toggle off again restores width.
- `Esc` keypress while pane visible and input empty closes the pane.
- `h/j/k/l` while pane visible and input empty mutate `mapPanOffset` only.
- `h/j/k/l` while textinput non-empty are forwarded to the input as normal.

### 8.4 Existing Tests
All existing tests must keep passing. The renamed `/map mermaid` path keeps its current tests untouched.

## 9. Implementation Plan Notes

The implementation plan should sequence work like this (final structure decided in writing-plans):
1. Engine `Snapshot()` + tests.
2. `mappane` package skeleton: types, `LayerOf`, `AllLayers`, `Render` for the empty / single-room cases + tests.
3. Cardinal links + connectivity rule + tests.
4. Diagonal links + tests.
5. Bridge badges + footer composition + tests.
6. Pan offset + `c` recenter + tests.
7. Layer cursor + `[` / `]` + tests.
8. Model integration: split width, toggle command, key routing, ESC close + integration tests.
9. Auto-recenter on map mutation.
10. Manual smoke against t2tmud.

## 10. Open Items Deferred

- Zoom levels (deferred to v2 if requested).
- Persistence of last-used pane width / offset across sessions.
- Cross-layer "world overview" mode.
- Bridge badge ASCII fallback for terminals without Unicode.
