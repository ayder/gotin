# Mapper Resilience — Phase 2 Design

**Date:** 2026-04-26
**Branch:** `mapper-resilience` (continues from Phase 1)
**Status:** Spec / pre-implementation
**Phase 1 spec:** `docs/superpowers/specs/2026-04-26-mapper-resilience-phase1-design.md`
**MXP corpus reference:** `docs/t2tmud_mxp.md`

## Context

Phase 1 made the broken description hash inert by default, added the
`/map option vnum|hash|none` switch, wired MXP `<ROOMNAME>` into the
mapper, and produced a structured JSON-Lines log of every MXP / GMCP / raw
chunk. The captured logs from `t2tmud.org` (see `docs/t2tmud_mxp.md`)
confirm:

- t2tmud emits **no** `<ROOMNAME>` and **no** GMCP `Room.Info`.
- t2tmud emits a rich set of structural MXP tags: `<expire>` (room block
  start), `<x>` / `<w>` / etc. (typed exit hyperlinks), `<i30>` / `<i9>`
  (NPC and item presence wrappers).
- The hash-collapse bug in the wilderness ("Gently rolling plains") is
  real: five distinct tiles share an identical static paragraph and
  differ only by a nearby-sight line and a water-exits line.
- The legacy text-only `ProcessRoomData` path also has a chunk-splitting
  bug independent of the description hash, since it processes one network
  chunk at a time instead of accumulating a complete room block.

## Decision

Build a **structural mapper path** keyed off MXP tags and a hard-coded
t2tmud profile (with a thin extension seam for future MUDs). The
structural path is the **only** auto-mapping path Phase 2 implements;
when MXP is not active on the current connection, **no** auto-mapping
runs and the user must operate the mapper manually via `/map dig`,
`/map name`, `/map link`, `/map goto`, `/map delete`, `/map undo`, etc.

The Phase 1 text-only `ProcessRoomData` path is **retired**. Its tests
remain green (Phase 1 acceptance criterion 5 stays in force) but the
function is no longer invoked from `app.onData`. We do not maintain a
heuristic fallback. Phase 2 is "best on MXP-emitting MUDs, manual on
others" by explicit choice — the user prefers no automation over
unreliable automation.

## Goals

1. With MXP active and t2tmud emitting `<expire>` plus typed exit /
   presence tags, the auto-mapper produces correct topology on the
   wilderness corpus that Phase 1 collapsed (5 plains tiles → 5 rooms,
   not 1).
2. Room-block boundaries are buffered correctly across split network
   chunks. No half-room hashing.
3. Exit extraction is structural (collect `<x>` / `<w>` / etc. tag
   bodies), not English-pattern based.
4. Presence lines (`<i30>`, `<i9>`) are dropped from the description
   used for hashing; nearby-sight lines are KEPT.
5. The t2tmud profile is hard-coded as the in-code default; future MUDs
   can override via `gotin.json` `mud_profiles.<host>`.
6. When MXP is not active, auto-mapping is silently disabled.
   Manual mapper commands continue to work.
7. No new user-facing flag is introduced; `/map option vnum|hash|none`
   from Phase 1 keeps its current semantics.

## Non-Goals

- Heuristic / English-pattern fallback for non-MXP MUDs.
- Multi-MUD plugin system. The `mud_profiles` map is a small JSON
  override, not a plugin loader.
- Persisting per-room MXP target keywords (`"seagull 1"`, `"trash can 1"`)
  for future trigger workflows. Captured in passing but not stored.
- A status-bar UI consuming `<gauge>` / `<!en>` entities.
- Renaming Phase 1 commands.
- Backwards-compatible map JSON migration. Maps written by Phase 1 with
  `Hash=on` keep working under their old hash; new auto-mapped rooms use
  the new hash. No automatic re-hash on load. Users with corrupted
  Phase 1 maps should `rm` the file and re-walk.

## Strict gate

The auto-mapper runs **only** when ALL of the following are true at the
moment a movement command is issued:

1. `e.IsAutoMapping()` is true (the user has run `/map start`).
2. The current `network.Client` has the MXP protocol installed AND
   `c.ActiveProtocols()` includes `"MXP"` (i.e. negotiation succeeded).
3. The MUD has emitted at least one `<expire>` tag since connect (the
   "we have seen the room-block start signal" sentinel; without it we
   have no boundary to buffer to).

If any of those is false, `ProcessMovement` returns
`(processed=false, "", nil)` — same as today's "not auto-mapping" early
return. The legacy `ProcessRoomData` and `HandleGMCPRoomInfo` paths are
also short-circuited under Phase 2 (kept available behind the feature
gate but never invoked from `app.onData` / `app.onGMCPRoomInfo` in the
new wiring).

Manual commands (`/map dig`, `/map name`, `/map link`, `/map goto`,
`/map delete`, `/map undo`, `/map show`, `/map info`, `/map option`,
`/map create`, `/map paths`, `/map exit`, `/map search`) are unaffected.

## Architecture

```
network rx
   │
   ▼
MXP Filter ──► strips tags from text stream (existing)
   │       └─► emits per-tag callbacks (NEW: SetTagCallback)
   ▼
text stream ──► UI viewport
              ──► RoomBlockBuffer  (NEW)
                  ├── starts on <expire>
                  ├── accumulates raw text + observed tags
                  └── completes on prompt detection
                       │
                       ▼
                  BlockParser  (NEW)
                       ├── derives exits from <x>/<w>/etc. tag bodies
                       ├── strips presence lines (<i30>/<i9>)
                       ├── strips weather + door-state lines (regex)
                       └── emits RoomBlock {description, exits, presence}
                       │
                       ▼
                  mapper.Engine.HandleRoomBlock(...)
                       └── existing pending-movement / loop-detection
                           logic, but called once per BLOCK instead of
                           per chunk
```

### 1. `mapper.MUDProfile` — t2tmud baked in, JSON override

New file: `internal/mapper/profile.go`.

```go
package mapper

// MUDProfile drives the structural block parser. Defaults are tuned for
// t2tmud.org and may be overridden per-host via gotin.json
// mud_profiles.<host>.
type MUDProfile struct {
    Host             string   `json:"host"`              // Match against connection host (case-insensitive).
    BlockStartTag    string   `json:"block_start_tag"`   // MXP tag name that marks a new room render.
    BlockEndPattern  string   `json:"block_end_pattern"` // Regex matched on text lines; first match closes the block.
    ExitTags         []string `json:"exit_tags"`         // MXP tag names whose body is a normalised direction.
    PresenceTags     []string `json:"presence_tags"`     // MXP tag names that mark lines to drop from description.
    WeatherPatterns  []string `json:"weather_patterns"`  // Regexes; matching lines are dropped from description.
    DoorStatePattern string   `json:"door_state_pattern"`// Regex; matching lines are dropped from description.
}

// DefaultT2TMUDProfile is the in-code default applied when no per-host
// override is present in gotin.json.
func DefaultT2TMUDProfile() MUDProfile {
    return MUDProfile{
        Host:            "t2tmud.org",
        BlockStartTag:   "expire",
        BlockEndPattern: `^HP:\d+ EP:\d+ \[\w+\] > $`,
        ExitTags:        []string{"x", "xx", "t", "w", "ww", "l", "ll", "y", "yy", "z"},
        PresenceTags:    []string{"i30", "i9"},
        WeatherPatterns: []string{
            `^The sky is .+\.$`,
            `^A .+ sky .+\.$`,
        },
        DoorStatePattern: `^The .+ (door|gate|portcullis) is (open|closed|locked|broken|sealed|barred)\.$`,
    }
}
```

The profile is **purely declarative**: no callbacks, no code, no
versioning. Compile-time defaults serve as a working profile and as
documentation of t2tmud's behaviour.

Override discovery (in app.go on connect):

1. Read `gotin.json` top-level `mud_profiles` map.
2. Look up the current host (case-insensitive) in that map.
3. If found, replace the corresponding fields on `DefaultT2TMUDProfile()`
   (per-field merge so a partial JSON override only changes what it sets).
4. Pass the resolved profile to the mapper engine via
   `e.SetMUDProfile(p)`.

If no override is present and the host is not `t2tmud.org`, the code
default still applies — but auto-mapping will only fire if that host
happens to also emit `<expire>` and matching exit tags. In practice this
means "if you're on a non-t2tmud MUD, you probably need a profile entry
in `gotin.json`."

### 2. MXP tag callback

`internal/mudproto/mxp/mxp.go`:

```go
// SetTagCallback registers a sink for every parsed MXP tag (open or
// close, with body and attributes intact). Invoked from Filter() before
// the tag is stripped from the text stream.
func (p *Protocol) SetTagCallback(cb func(name, body string)) {
    p.onTag = cb
}
```

Wire from `app.connect`: when `findMXP(c) != nil`, install a callback
that forwards every tag to `s.roomBuffer.OnTag(name, body)`.

The existing `SetRoomNameCallback` stays. The new `SetTagCallback` is a
superset — it sees `ROOMNAME` too — but specialised hooks for `ROOMNAME`
remain because they also drive the UI (`ui.RoomNameMsg`).

### 3. `mapper.RoomBlockBuffer`

New file: `internal/mapper/blockbuf.go`.

```go
// RoomBlock holds the parsed room-render output for one mapper update.
type RoomBlock struct {
    Description string   // Cleaned static description (post-strip, post-filter).
    Exits       []string // Normalised exit short-forms in order of appearance.
    Presence    []string // Raw "X is here" lines (kept for completeness; not hashed).
    Vnum        string   // Always empty on t2tmud; non-empty if a future profile sets it.
    Name        string   // From <ROOMNAME> if observed; else "".
}

// RoomBlockBuffer accumulates MXP tags and stripped text between
// <expire> markers. Single-goroutine: only the network-rx goroutine
// calls OnTag / OnText.
type RoomBlockBuffer struct {
    profile  MUDProfile
    open     bool
    text     strings.Builder
    tags     []taggedFragment // ordered (name, body) pairs in this block
    endRe    *regexp.Regexp
    weatherR []*regexp.Regexp
    doorR    *regexp.Regexp
    onClose  func(b RoomBlock)
}

func NewRoomBlockBuffer(p MUDProfile, onClose func(RoomBlock)) *RoomBlockBuffer
func (b *RoomBlockBuffer) OnTag(name, body string)   // called by MXP tag callback
func (b *RoomBlockBuffer) OnText(s string)           // called by app.onData with stripped text
func (b *RoomBlockBuffer) Reset()                    // called on disconnect
```

Behaviour:

- `OnTag(name, body)`:
  - If `name == profile.BlockStartTag`: close any in-flight block
    (orphan; should not happen normally), then open a fresh one.
  - Else if `b.open`: append `(name, body)` to `b.tags`.
  - Else: ignore.
- `OnText(s)`:
  - If not open: ignore.
  - Else: append to `b.text`. Scan the appended text line-by-line; when
    a line matches `b.endRe` (the prompt pattern), call
    `b.parseAndEmit()` and close the block.
- `parseAndEmit()` constructs a `RoomBlock` (see Block parser) and
  invokes `b.onClose`. Then resets `b.text`, `b.tags`, `b.open`.
- `Reset()` is called by app on disconnect / reconnect to clear stale
  state.

### 4. Block parser

Inside `RoomBlockBuffer.parseAndEmit()`:

1. **Exits.** Walk `b.tags` in order. For each entry where `name` is in
   `profile.ExitTags`, parse the body's first whitespace-trimmed token
   (the body of `<x>east</x>` is just `east`), normalise via
   `ParseDirection`, and append the short-form to `Exits` if not
   already present. (Land and water exits are merged; duplicates
   suppressed in case the server emits both `<x>n</x>` and `<w>n</w>`.)

2. **Presence.** For each `<i30>` / `<i9>` open tag, capture the line
   in `b.text` whose byte range overlaps the tag's position (tracked
   via fragment offsets). Add the trimmed line text to `Presence`.
   These lines are **also dropped** from the description body.

3. **Description.**
   - Strip ANSI escape sequences from `b.text` to get plain text.
   - Split into lines.
   - Drop any line that contains a presence tag (already covered by
     step 2 — track which line indices to drop).
   - Drop lines matching `b.weatherR`.
   - Drop lines matching `b.doorR`.
   - Drop lines matching `b.endRe` (the prompt — already excluded since
     it closes the block, but defensive).
   - Drop lines that are pure exits-line markers
     (`^    The only obvious exit (is|are) .+\.$`,
     `^    There is water to the .+\.$`) — these are presentation, not
     identity.
   - Drop empty / whitespace-only lines.
   - Trim each remaining line, then join with `\n`. The result is the
     hashable static description, including nearby-sight lines.

4. **Vnum / Name.** Default empty. If a future profile adds a "vnum tag"
   field, populate from there. For t2tmud both stay empty.

### 5. New structural hash

In `internal/mapper/mapper.go`:

```go
// ComputeStructuralHash builds a Phase 2 room-identity hash from a
// cleaned description and a sorted, de-duplicated exit set. The format
// is deliberately distinct from the Phase 1 ComputeRoomHash output so
// that mixed Phase 1 / Phase 2 maps cannot accidentally collide.
func ComputeStructuralHash(desc string, exits []string) string {
    norm := normaliseDescription(desc) // trim, collapse internal whitespace
    sortedExits := append([]string(nil), exits...)
    sort.Strings(sortedExits)
    payload := "v2|" + norm + "|" + strings.Join(sortedExits, ",")
    h := sha256.Sum256([]byte(payload))
    return hex.EncodeToString(h[:])
}
```

The Phase 1 `ComputeRoomHash` is kept for backwards compatibility with
saved maps. New rooms created under Phase 2 use the v2 hash. Existing
Phase 1 hash entries in `hashIndex` are not touched — Phase 2 looks up
new rooms in a parallel `structuralIndex` map. This avoids any
accidental cross-version collisions.

```go
type Engine struct {
    // ...existing fields...
    structuralIndex map[string]string // v2 hash -> RoomID
    profile         MUDProfile
    sawBlockStart   bool              // sentinel for the strict gate
    blockBuf        *RoomBlockBuffer
}
```

### 6. New entry point: `Engine.HandleRoomBlock`

```go
// HandleRoomBlock processes a fully-buffered room render. Replaces the
// per-chunk ProcessRoomData / HandleGMCPRoomInfo flow when MXP is the
// data source. Behavior:
//   - If !IsAutoMapping(): no-op.
//   - If !sawBlockStart (a <expire> hasn't been seen yet this session):
//     no-op. The block buffer should never call us in that state, but
//     defensive.
//   - If a movement is pending (set by ProcessMovement), use the block
//     to either link to an existing room (loop detection) or create a
//     new one. Honour MappingOptions.{Vnum, Hash} for the lookup
//     strategies, same priority chain as Phase 1's HandleGMCPRoomInfo.
//   - If no movement is pending, just refresh the current room's
//     description and (when Hash is on) re-key its structural hash.
func (e *Engine) HandleRoomBlock(b RoomBlock) (processed bool, roomName string, loopDetected bool, err error)
```

This becomes the **single** structural ingestion point. `ProcessRoomData`
and `HandleGMCPRoomInfo` from Phase 1 are no longer wired in `app.onData`
/ `app.onGMCPRoomInfo` (they remain in the package for tests and for
hypothetical re-enablement, but the production code path skips them).

`ProcessMovement` stays unchanged in its public shape but the gate is
tightened to only set the pending state when MXP is active and
`sawBlockStart` is true. Otherwise `processed=false` and the caller (in
`app.go`) treats the input as a server-bound command (current behaviour).

### 7. Wiring changes in `internal/app/app.go`

- `Session` gains a `roomBuf *mapper.RoomBlockBuffer` field, initialised
  on connect once we know the MUD profile.
- On connect, after `installProtocols(c, …)` and the existing GMCP/MXP
  callback wiring:
  1. Resolve the active MUD profile (per-host override or default).
  2. Call `s.mapEngine.SetMUDProfile(profile)`.
  3. Construct `s.roomBuf = mapper.NewRoomBlockBuffer(profile, s.onRoomBlock)`.
  4. If MXP is installed: install the tag callback, the room-name
     callback (existing), and the protolog logger (existing). The tag
     callback forwards to `s.roomBuf.OnTag(name, body)` and ALSO sets
     `s.mapEngine.MarkBlockStartSeen()` when `name == profile.BlockStartTag`.
  5. If MXP is NOT installed: leave `roomBuf` nil. The mapper's gate
     prevents any automation from running.
- `app.onData(c, lb, proc, data)`:
  - The current `ProcessRoomData` / `HandleGMCPRoomInfo` calls under
    `HasPendingMovement && !gmcpActive` are removed.
  - Instead, after MXP filtering, push the cleaned `data` to
    `s.roomBuf.OnText(data)` (when `roomBuf != nil`).
- `app.onGMCPRoomInfo` keeps logging the protolog `room_info` event,
  keeps sending `ui.RoomNameMsg`, but **no longer calls
  `mapEngine.HandleGMCPRoomInfo`**. (Phase 2 expects all room data
  through the MXP-block path; GMCP `Room.Info` is informational on
  every MUD we've inspected and was unused on t2tmud anyway.)
- `s.onRoomBlock(b mapper.RoomBlock)` is the closure passed to
  `NewRoomBlockBuffer`. It:
  1. Sends `ui.RoomNameMsg{Name: b.Name}` if non-empty (the
     SetIncomingRoomName latch from Phase 1 is kept for the
     `<ROOMNAME>`-only case but is now redundant in the MXP-block
     pathway since `RoomBlock.Name` is included).
  2. Calls `e.mapEngine.HandleRoomBlock(b)`.
  3. Handles `processed / loopDetected / err` for the user-visible
     status messages, mirroring the existing GMCP path.

### 8. `gotin.json` schema additions

Top-level new field:

```json
{
  "mud_profiles": {
    "t2tmud.org": {
      "block_start_tag": "expire",
      "exit_tags": ["x","xx","t","w","ww","l","ll","y","yy","z"],
      "presence_tags": ["i30","i9"]
    }
  }
}
```

Empty `mud_profiles` is valid (defaults apply). A partial entry merges
with the in-code default (per-field replace; arrays are replaced
wholesale, not appended).

The existing per-connection `mapping_options` from Phase 1 stays
unchanged.

## User-visible behaviour changes

- `/map start` while MXP is **not** active: prints
  `Auto-mapping requires MXP to be enabled on this connection. Manual /map dig still works.`
  Auto-mapping is enabled internally (so subsequent reconnects with MXP
  can pick up), but no rooms are auto-created until MXP comes online and
  `<expire>` is seen.
- `/map start` with MXP active but no `<expire>` seen yet: succeeds; the
  first dig will not auto-create until the first `<expire>` arrives. No
  user-visible warning needed; a status line on the first auto-create
  ("Block stream picked up, mapping armed.") is acceptable but optional.
- Manual mapper commands work in all states.
- `/map info` continues to display the current room's data; under
  Phase 2 the description shown is the cleaned static text (post-strip),
  not the raw colored block.
- `/map option vnum on|off` and `/map option hash on|off` keep their
  Phase 1 semantics. With MXP off they're effectively dormant (no
  automation is running). With MXP on they gate the v2 hash and the
  vnum lookup respectively.

## Testing

### Unit tests

- `mapper.DefaultT2TMUDProfile` snapshot: every field has a non-empty
  default that matches `docs/t2tmud_mxp.md`.
- `mapper.MergeProfile(default, override)` per-field merge semantics.
- `RoomBlockBuffer.OnTag/OnText` lifecycle:
  - `<expire>` opens a block.
  - Subsequent `<x>east</x>`, weather text, prompt → emits a
    `RoomBlock` with `Exits=["e"]`, `Description=<cleaned>`, no
    `Presence`.
  - Mid-block split network chunks (split inside a tag body, split
    inside a line) reassemble correctly.
  - Spurious `<expire>` mid-block discards prior partial block and
    starts a fresh one.
  - `Reset()` discards in-flight state.
- `BlockParser`:
  - The 5 plains tiles fixture (captured corpus) produces 5 distinct
    structural hashes.
  - Weather lines dropped per `WeatherPatterns`.
  - Door-state lines dropped per `DoorStatePattern`.
  - Nearby-sight lines KEPT (regression: `The Lune River lies …`
    survives into `Description`).
  - Presence lines dropped (regression: `An ugly orc` line removed).
- `Engine.HandleRoomBlock` with the strict gate:
  - MXP not seen → no-op even with auto-mapping on.
  - Block start sentinel false → no-op.
  - All conditions satisfied → drives the existing pending-movement /
    loop-detection logic.
- `Engine.ProcessMovement` returns `processed=false` when MXP is not
  active.

### Integration / fixture tests

- New `internal/mapper/testdata/t2tmud_plains_*.txt` fixtures: 5 raw
  chunks captured from `gotin.log`. Replayed through a synthetic
  `Filter` + `RoomBlockBuffer` pipeline; assert exactly 5 distinct
  rooms with the expected exit sets.
- `internal/mapper/testdata/t2tmud_paved_street.txt`: 1 chunk, NPC
  presence → assert NPC line dropped, description hashes correctly.
- `internal/mapper/testdata/t2tmud_white_towers.txt`: nearby-sight
  variants → assert hashes differ when sights differ.

### Existing tests

- All Phase 1 tests must continue to pass.
- `TestProcessRoomData_*` tests stay green (function still works
  internally) but a new test confirms `app.onData` does NOT call
  `ProcessRoomData` when MXP is wired.

## Acceptance criteria

1. `go build ./...` and `go test ./... -race` pass.
2. With MXP active on t2tmud and `Hash=on`, walking the 5-tile plains
   loop produces 5 distinct rooms in the saved map. Walking back across
   the same path links correctly (no false collapses, no duplicates).
3. With MXP active and `Hash=off`, every dig creates a new room (Phase 1
   parity).
4. With MXP **not** active on any connection: `/map start` prints the
   warning; no auto-rooms are created; `/map dig` continues to work and
   creates rooms manually.
5. The `mud_profiles.t2tmud.org` entry in `gotin.json` overrides only the
   fields it specifies; absent fields fall back to the in-code default.
6. The Phase 1 hash (`ComputeRoomHash`) and the Phase 2 hash
   (`ComputeStructuralHash`) coexist in the same map without collision.
   Maps from Phase 1 with `Hash=on` survive load.

## Migration / compatibility

- Existing Phase 1 maps load. Their `description_hash` fields remain
  populated with the old short-hash strings; they sit in the legacy
  `hashIndex` and are still consulted by the Phase 1 path (which is now
  unreachable from production code but retained behind the dead-code
  gate for tests).
- New rooms get a Phase 2 v2 hash. Stored in `Room.DescriptionHash` (we
  reuse the field to avoid schema churn) and in a new in-memory
  `structuralIndex map[string]string`.
- A user with a corrupted Phase 1 map can `rm <map>.json` and re-walk to
  rebuild under Phase 2. We do **not** attempt automatic rebuild on
  load; the migration is opt-in by deletion.

## Risks / open questions

1. **Block-end pattern brittleness.** The default `^HP:\d+ EP:\d+ \[\w+\] > $`
   assumes a specific prompt format. Players with custom prompts will
   break the buffer. Two mitigations: (a) make the pattern overridable
   per-MUD (already in `MUDProfile.BlockEndPattern`); (b) add a fallback
   timeout — if no end pattern matches within N seconds of a block
   start, parse-and-emit anyway. Pick (a) for now; revisit (b) if logs
   show stuck blocks.

2. **Tag-callback replay of element definitions.** The `<!el>` and `<!en>`
   bursts on MXP-enable will fire `OnTag` calls. The buffer is not yet
   open (no `<expire>` seen), so they're silently ignored. Confirmed by
   the `OnTag` "ignore when not open" branch.

3. **Multiple `<x>` tags for the same direction.** Some MUDs may emit
   both a land and a water exit for the same compass direction. We
   de-duplicate at the structural-hash level (sorted unique). The
   mapper still records both as a single edge to the destination room.
   If two distinct destinations reachable by `n` exist, the topology
   model loses one — but no MUD we've seen does this and `t2tmud.org`'s
   exit element catalogue makes it impossible.

4. **Other MXP MUDs.** Phase 2 ships only the t2tmud profile. Adding
   another MUD requires a `gotin.json` entry naming the right tags. The
   test suite documents the t2tmud profile; future MUDs would add their
   own fixtures.

5. **GMCP retirement on the room-info path.** Removing
   `mapEngine.HandleGMCPRoomInfo` from `app.onGMCPRoomInfo` means a
   future MUD that DOES emit `Room.Info` (Aardwolf, Achaea, etc.)
   wouldn't be auto-mapped via GMCP. This is acceptable — those MUDs
   would set `Vnum=on` but Phase 2 only fires on MXP-block close. If a
   user reports such a MUD, we'd add a second ingestion path then.
   Documented limitation, not a bug.

## Out-of-scope (Phase 3 candidates, not committed)

- Heuristic text path for non-MXP MUDs.
- Per-room storage of MXP target keywords (`<i30>` `id` attribute).
- A `<gauge>` / `<!en>`-driven status bar.
- GMCP-driven mapping for Aardwolf-style MUDs.
- Auto-rebuild of Phase 1 hashes to v2 on map load.
- Cross-host map merging with vnum namespacing.

## Implementation plan

A separate `docs/superpowers/plans/2026-04-26-mapper-resilience-phase2.md`
will break this spec into TDD tasks once approved.
