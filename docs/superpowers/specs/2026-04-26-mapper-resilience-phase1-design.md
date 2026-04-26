# Mapper Resilience — Phase 1 Design

**Date:** 2026-04-26
**Branch:** `mapper-resilience`
**Status:** Spec / pre-implementation
**Phase 2 follow-up:** `docs/superpowers/notes/2026-04-26-mapper-resilience-phase2-reminder.md`

## Context

The auto-mapper currently uses a description hash (`desc[0:50] + sorted(exits)`) as the
primary identity signal for loop detection. On MUDs like `t2tmud.org`, many distinct
rooms share identical short descriptions and exit sets, so the hash collapses different
rooms into one and corrupts the mapped topology.

Two failures observed on `t2tmud.org`:

1. **Hash collisions on non-unique rooms.** Hash-based loop detection produces false
   "loop detected" results, linking unrelated rooms.
2. **MXP `<ROOMNAME>` not reaching the mapper.** The MXP `<ROOMNAME>` tag is parsed and
   sent to the UI status line (`internal/app/app.go:389`) but never fed into
   `mapper.Engine`, so auto-created rooms are stuck named `"New Room"`.

We also do not currently log enough MXP / GMCP detail in `gotin.log` to determine what
the MUD actually emits — so we cannot yet design a correct hash replacement.

## Decision

Split the work into two phases:

- **Phase 1 (this spec):** Add a per-strategy switch (`vnum`, `hash`, `none`), default
  the broken hash to off, wire MXP `<ROOMNAME>` into the mapper, and emit structured
  JSON debug logs of every MXP / GMCP / raw chunk that contains markup or escape
  sequences. This unblocks the user immediately and produces the log corpus Phase 2
  needs.
- **Phase 2 (separate spec, gated on log analysis):** Redesign the description
  extractor and hash function based on what the captured logs reveal about
  `t2tmud.org`'s real output. **No Phase 2 spec is written before Phase 1 logs are
  captured and inspected.**

Phase 1 explicitly does **not** fix the hash content or attempt description cleaning.
With `Hash=off` as the default, the broken hash becomes inert; users can opt back in
on MUDs where it works.

## Goals

1. User can toggle each loop-detection strategy independently and combine them.
2. Defaults render the current bug inert without removing any code path.
3. MXP-supplied room names appear on auto-created rooms.
4. `gotin.log` captures enough MXP / GMCP / raw detail to reverse-engineer
   `t2tmud.org`'s output offline.
5. No regression for MUDs that already work (e.g. those that supply a clean GMCP
   `vnum`).

## Non-Goals

- Fixing the hash content or formula.
- Cleaning room descriptions of dynamic content (weather, NPCs, items, players).
- Per-MUD configuration profiles for description cleaning.
- A new structural identity model (room fingerprints, multi-signal matching).
- Changes to GMCP / MXP negotiation.

All of the above are Phase 2 concerns.

## Architecture

### 1. `mapper.MappingOptions` on `Engine`

New struct on `internal/mapper/mapper.go`:

```go
type MappingOptions struct {
    Vnum bool // Use stable IDs from GMCP/MXP (e.g. Room.Info.num) for loop detection.
    Hash bool // Use description+exits hash for loop detection.
}

func DefaultMappingOptions() MappingOptions {
    return MappingOptions{Vnum: true, Hash: false}
}
```

Stored on `Engine` and guarded by existing `e.mu`:

```go
type Engine struct {
    // ...existing fields...
    options MappingOptions
}

func (e *Engine) SetMappingOptions(opts MappingOptions)
func (e *Engine) GetMappingOptions() MappingOptions
```

`NewEngine(path)` initializes `options = DefaultMappingOptions()`.

### 2. Loop-detection paths consult the options

**`HandleGMCPRoomInfo` (mapper.go:854)**

- If `options.Vnum && r.Vnum != ""`: existing vnum-keyed lookup.
- Else if `options.Hash && r.Description != ""`: existing hash-keyed lookup using
  `ComputeRoomHash(r.Description, exits)`.
- Else: skip the loop-detection lookup. Always create a new room.
- Room ID for new rooms: `r.Vnum` if `options.Vnum && r.Vnum != ""`, else
  `uuid.New().String()`. **Do not** key new rooms on the hash unless
  `options.Hash` is on.

**`ProcessRoomData` (mapper.go:791)**

- If `options.Hash`: existing hash-keyed lookup.
- Else: skip the lookup; always create a new room with `uuid.New().String()`.
- The "no pending movement" branch still updates `Description` / `DescriptionHash`
  on the current room, but only writes into `hashIndex` when `options.Hash` is on
  (avoids polluting the index for later when the user toggles hash on).

**`createRoom` (mapper.go:732)**

- Adds to `hashIndex` only when `options.Hash` is true. (Currently unconditional —
  this avoids the index being pre-populated with bad keys.)

### 3. `/map option` command

In `internal/command/` (alongside existing `/map` subcommands):

- `/map option` → prints `mapping options: vnum=on, hash=off` (current values).
- `/map option vnum on|off`
- `/map option hash on|off`
- `/map option none` → sets `Vnum=false, Hash=false`.
- Invalid input → usage message.

Each setter call:
1. Updates `mapEngine.SetMappingOptions(...)`.
2. Marks the active connection's options dirty and calls `scheduleSave()`.
3. Echoes new state back via `ui.StatusMsg`.

### 4. Persistence in `gotin.json`

Schema additions (backward compatible — missing fields fall back to defaults):

```json
{
  "aliases": {...},
  "connections": {
    "t2t": {
      "host": "t2tmud.org",
      "port": 9999,
      "auto": true,
      "mapping_options": { "vnum": true, "hash": false }
    }
  },
  "mapping_options": { "vnum": true, "hash": false }
}
```

- Per-connection `mapping_options` wins for saved-alias connects.
- Top-level `mapping_options` is the fallback for ad-hoc `/connect` and for newly
  created connection aliases.
- On connect, `Session` resolves the active options and calls
  `mapEngine.SetMappingOptions`.
- `/map option …` writes to whichever scope is active for the current session
  (per-connection if connected via alias, else top-level).

### 5. MXP `<ROOMNAME>` → mapper

New mapper method:

```go
// SetIncomingRoomName attaches a name supplied out-of-band (e.g. MXP <ROOMNAME>)
// to the room that is about to be created or has just been created. Behavior:
//   - If a movement is pending, store the name so the next ProcessRoomData /
//     HandleGMCPRoomInfo uses it instead of "New Room".
//   - Else: update the current room's Name in place.
func (e *Engine) SetIncomingRoomName(name string)
```

Implementation: a new `pendingName string` field on `Engine`, consumed and cleared
inside `createRoom` (replacing the hard-coded `"New Room"` literal in
`mapper.go:836`). When no movement is pending, write directly to
`e.data.Rooms[e.data.CurrentRoom].Name`.

Wire in `internal/app/app.go:389`:

```go
m.SetRoomNameCallback(func(name string) {
    if name == "" {
        return
    }
    s.mapEngine.SetIncomingRoomName(name)
    s.trySendUI(ui.RoomNameMsg{Name: name})
})
```

### 6. Structured JSON logging — `internal/mudproto/protolog`

New package providing one entry point used by MXP, GMCP, and the network read path.

```go
package protolog

type Entry struct {
    TS     string         `json:"ts"`     // RFC3339Nano
    Source string         `json:"source"` // "mxp" | "gmcp" | "raw"
    Dir    string         `json:"dir"`    // "rx" | "tx"
    Event  string         `json:"event"`  // "chunk" | "tag" | "mode" | "probe" | "room_info" | "room_name"
    UTF8   string         `json:"utf8,omitempty"` // strconv.Quote-style escaped
    Hex    string         `json:"hex,omitempty"`  // lowercase hex
    Parsed map[string]any `json:"parsed,omitempty"`
}

type Logger interface {
    Log(e Entry)
    Enabled() bool
}
```

- One JSON object per line, written to the existing `gotin.log` writer.
- Gated by the existing `-debug` flag. When debug is off, `Enabled()` returns false
  and call sites must skip payload formatting (avoid hex-encoding overhead).
- Sources:
  - `mxp.Filter` (`internal/mudproto/mxp/mxp.go:128`):
    - On entry, when chunk contains `<` or `\x1b`: emit a `chunk` event with
      `utf8`+`hex` of the raw chunk.
    - Per parsed tag: emit a `tag` event with `parsed.name`, `parsed.body`.
    - On `<ROOMNAME>` extraction: emit a `room_name` event with `parsed.name`.
    - Per mode change: emit a `mode` event with `parsed.n`, `parsed.mode`.
    - On probe response sent: emit a `probe` event (`dir: "tx"`) with
      `parsed.kind = "version"|"supports"`.
  - `gmcp.go`:
    - Per subneg payload arrival: `chunk` event with `utf8`+`hex` of the raw
      payload.
    - On `Room.Info` parse: `room_info` event with `parsed` populated from the
      parsed `RoomInfo` (vnum, name, area, exits, description length).
  - Network read path (`internal/network/`):
    - When `-debug` on, emit a `raw` `chunk` event for each rx chunk before any
      protocol filtering. Truncate `utf8`/`hex` to 4 KiB; flag `truncated=true`
      in `parsed` when truncated. (Bandwidth guard for floods.)

### 7. Wiring

- `app.New(...)` constructs a `protolog.Logger` from the same writer as
  `gotin.log` and passes it into:
  - `mxp.Protocol` (new `SetLogger` setter; `nil` ⇒ no-op).
  - `gmcp` callbacks (logger captured in the `g.SetCallback` closure in
    `app.go:382`).
  - `network.Client` (new optional `SetProtocolLogger` for raw rx).
- `Session` resolves `MappingOptions` for the current connection on connect and
  calls `mapEngine.SetMappingOptions(...)`.

## Data Flow (summary)

```
network rx bytes
    │
    ├──► protolog (raw chunk, if -debug)
    │
    ▼
MXP Filter ──► protolog (chunk + tag + mode + room_name + probe events)
    │
    ▼
text stream ──► UI viewport
              ──► logic.LineBuffer ──► triggers/aliases
              ──► (if no GMCP) mapper.ProcessRoomData

GMCP subneg ──► protolog (chunk + room_info)
            ──► mapper.HandleGMCPRoomInfo

MXP <ROOMNAME> ──► mapper.SetIncomingRoomName ──► used inside createRoom
                ──► UI status line
```

## Testing

### Unit tests

- `mapper.SetMappingOptions` round-trip; concurrent get/set under `e.mu`.
- `HandleGMCPRoomInfo` with `Vnum=false`: same vnum twice → two distinct rooms.
- `HandleGMCPRoomInfo` with `Vnum=false, Hash=false`: same description twice →
  two distinct rooms.
- `ProcessRoomData` with `Hash=false`: identical chunk twice → two distinct rooms.
- `ProcessRoomData` with `Hash=false`: `hashIndex` does **not** grow.
- `SetIncomingRoomName` populates next created room's Name.
- `SetIncomingRoomName` with no pending movement updates current room's Name.
- `protolog.Entry` JSON marshalling deterministic; `Hex` lower-case; `UTF8`
  reversible via `strconv.Unquote`.
- `mxp.Filter` with logger attached: a chunk containing
  `<VERSION><ROOMNAME>X</ROOMNAME>\x1b[1zfoo\n` produces the expected sequence
  of log entries (`chunk`, `tag` × N, `room_name`, `mode`, `probe` for response).
- `gmcp` Room.Info: log entry `parsed` matches `RoomInfo` fields.

### Integration / manual

- Save `t2t` connection; `/map option vnum off`; reconnect; verify
  `gotin.json.connections.t2t.mapping_options.vnum == false`.
- Walk five rooms on `t2tmud.org` with `-debug`; confirm `gotin.log` contains
  `mxp` and `gmcp` JSON entries with `room_name` and/or `room_info`.
- Confirm rooms are no longer collapsed under default options.
- Confirm auto-mapped room names are populated when `<ROOMNAME>` is sent.

### Test fixtures

- Add `internal/mudproto/mxp/testdata/t2tmud_chunk.txt` with a captured raw chunk
  (sanitized) once Phase 1 ships, so Phase 2 can regression-test against it. Not
  required to land Phase 1.

## Acceptance Criteria

1. `go build ./...` and `go test ./...` pass on `mapper-resilience`.
2. `/map option` prints, sets, persists, and reloads correctly across restarts.
3. Default new-config has `vnum=true, hash=false` at both global and
   per-connection scope.
4. With defaults, walking a 10-room tour on `t2tmud.org` produces exactly 10
   distinct rooms (no false collapses).
5. With `Hash=on`, behavior matches the current broken behavior — i.e. nothing
   silently changes for users who opt in. (Verified by existing mapper tests
   continuing to pass with `Hash=true` set in test setup.)
6. After a `t2tmud.org` session with `-debug`, `gotin.log` contains valid JSON
   Lines including at least one `mxp/room_name` or `gmcp/room_info` entry.

## Phase 1 Exit Gate

User runs the Phase 1 build against `t2tmud.org` with `-debug`, walks at least 10
rooms (including some revisits and at least one weather/time tick), and `/quit`s
cleanly. The resulting `gotin.log` is shared and inspected. **Phase 2 spec writing
begins only after this log exists and has been read.**

## Risks / Open Questions

- **Logger overhead.** Hex-encoding every rx chunk is O(n) per chunk. Mitigation:
  gate strictly behind `-debug`; truncate at 4 KiB. Acceptable given the goal is
  diagnostic capture, not always-on telemetry.
- **MXP carryover for split tags.** Current `mxp.Filter` already handles split
  tags by passing through `<` when no `>` is found. Logger should emit the
  pre-parse `chunk` event regardless, so that split-tag debugging is possible.
  No change to filter logic; just ensure the logger sees both halves.
- **MXP `<ROOMNAME>` arrival ordering vs. movement.** The name might arrive
  before, between, or after the description block. `SetIncomingRoomName` uses
  `pendingName` as a one-slot latch consumed by `createRoom`; if multiple
  `<ROOMNAME>` events arrive before consumption, the last one wins. This matches
  the user's mental model ("the most recent name is the right one").
- **Vnum-only collisions across MUDs.** If a user merges two map files from
  different MUDs that both use small integer vnums, ids collide. Out of scope
  for Phase 1 (no per-MUD namespacing yet); flagged for Phase 2.
