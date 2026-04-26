# Mapper Resilience — Phase 1 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a per-MUD strategy switch (`/map option vnum|hash|none`) that defaults the broken description hash to off, wire MXP `<ROOMNAME>` into the mapper, and emit structured JSON debug logs of every MXP / GMCP / raw-rx chunk so we can later reverse-engineer `t2tmud.org`'s real output for Phase 2.

**Architecture:** Add `MappingOptions{Vnum, Hash bool}` to `mapper.Engine` and gate the existing vnum / hash lookups in `HandleGMCPRoomInfo` and `ProcessRoomData` behind those flags. New `/map option` subcommand (parsed in `internal/input/handler.go`, dispatched in `internal/app/app.go`) updates options and persists them in `gotin.json`, per-connection or top-level. A new `pendingName` latch on `Engine` lets the MXP `<ROOMNAME>` callback (already invoked from `app.go`) populate auto-created room names. New `internal/mudproto/protolog` package writes JSON-line entries to `gotin.log`; hooks live in `mxp.Filter`, the GMCP path, and `network.Client`'s read loop, all gated by `-debug`.

**Tech Stack:** Go 1.21+, standard library, existing `bubbletea` UI, existing `crypto/sha256`, `encoding/json`, `encoding/hex`. No new external deps.

**Spec:** `docs/superpowers/specs/2026-04-26-mapper-resilience-phase1-design.md`
**Phase 2 reminder (do NOT implement now):** `docs/superpowers/notes/2026-04-26-mapper-resilience-phase2-reminder.md`
**Branch:** `mapper-resilience` (already created, currently checked out).

---

## File Structure

**Create**
- `internal/mudproto/protolog/protolog.go` — `Entry`, `Logger` interface, `JSONLinesLogger` impl.
- `internal/mudproto/protolog/protolog_test.go` — unit tests.

**Modify**
- `internal/mapper/mapper.go` — add `MappingOptions`, options-aware loop detection, `pendingName` latch, `SetIncomingRoomName`.
- `internal/mapper/mapper_test.go` — new tests for options gating, name latch; existing tests adjusted to set `Hash=true` where they rely on hashing.
- `internal/command/command.go` — add `MapOption` command struct.
- `internal/input/handler.go` — `option` subcommand parsing inside `cmdMap`.
- `internal/input/handler_test.go` — parser test cases.
- `internal/input/handler.go` — extend `ConnectionAlias` with `MappingOptions *mapper.MappingOptions`.
- `internal/app/app.go` — `gotinData.MappingOptions` field, dispatch for `MapOption`, resolve options on connect, persist on change, wire MXP `<ROOMNAME>` to mapper, install protolog logger into MXP / GMCP / network.
- `internal/mudproto/mxp/mxp.go` — `SetLogger` setter; emit chunk/tag/room_name/mode/probe events from `Filter` and probe-response sites.
- `internal/mudproto/gmcp/gmcp.go` — `SetLogger` setter; emit chunk event in `OnSubnegotiation`.
- `internal/network/telnet.go` — `SetProtocolLogger` setter; emit raw-rx chunk event in `ReadLoop` (truncated at 4 KiB).

**Checkpoints**
- **A.** Options struct + gating in mapper (Tasks 1–6)
- **B.** `/map option` command (Tasks 7–9)
- **C.** MXP `<ROOMNAME>` → mapper (Tasks 10–12)
- **D.** Persistence in `gotin.json` (Tasks 13–15)
- **E.** `protolog` package + hooks (Tasks 16–22)
- **F.** Build, full test suite, manual t2tmud verification (Task 23)

Commit after each task that ends with a passing test. Engineer should pause for review at the end of each checkpoint.

---

## Checkpoint A — Options struct + gating in mapper

### Task 1: Add `MappingOptions` type and defaults

**Files:**
- Modify: `internal/mapper/mapper.go` (insert after `Direction` constants block, before `Room`)

- [ ] **Step 1: Write the failing test**

Add to `internal/mapper/mapper_test.go` (top of file or in a new test block — keep with existing tests):

```go
func TestDefaultMappingOptions(t *testing.T) {
    opts := DefaultMappingOptions()
    if !opts.Vnum {
        t.Errorf("DefaultMappingOptions().Vnum = false, want true")
    }
    if opts.Hash {
        t.Errorf("DefaultMappingOptions().Hash = true, want false")
    }
}

func TestMappingOptionsJSON(t *testing.T) {
    opts := MappingOptions{Vnum: true, Hash: false}
    b, err := json.Marshal(opts)
    if err != nil {
        t.Fatalf("Marshal: %v", err)
    }
    if string(b) != `{"vnum":true,"hash":false}` {
        t.Errorf("Marshal = %q, want %q", string(b), `{"vnum":true,"hash":false}`)
    }
    var got MappingOptions
    if err := json.Unmarshal(b, &got); err != nil {
        t.Fatalf("Unmarshal: %v", err)
    }
    if got != opts {
        t.Errorf("round trip = %+v, want %+v", got, opts)
    }
}
```

If `encoding/json` is not yet imported in the test file, add it.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/mapper/ -run TestDefaultMappingOptions -v`
Expected: FAIL with `undefined: DefaultMappingOptions` and `undefined: MappingOptions`.

- [ ] **Step 3: Add the type and constructor in `mapper.go`**

Insert immediately after the `Out Direction = "out"` constant block (around line 33, before `ReverseDirection`):

```go
// MappingOptions configures which loop-detection strategies the mapper uses
// during auto-mapping. Strategies are independent and may be combined.
//
//	Vnum: when true, prefer stable IDs supplied by the MUD (e.g. GMCP
//	      Room.Info.num) for both new-room IDs and loop detection.
//	Hash: when true, use the description+exits hash for loop detection,
//	      and key new rooms into the hash index. Set to false on MUDs that
//	      have non-unique room descriptions, where hashing causes false
//	      collapses.
type MappingOptions struct {
    Vnum bool `json:"vnum"`
    Hash bool `json:"hash"`
}

// DefaultMappingOptions returns the safe default: vnum on, hash off. Hash is
// off by default because some MUDs (e.g. t2tmud.org) reuse identical short
// descriptions across distinct rooms, which collapses the map under hashing.
func DefaultMappingOptions() MappingOptions {
    return MappingOptions{Vnum: true, Hash: false}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/mapper/ -run "TestDefaultMappingOptions|TestMappingOptionsJSON" -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/mapper/mapper.go internal/mapper/mapper_test.go
git commit -m "feat(mapper): add MappingOptions struct with vnum/hash flags"
```

---

### Task 2: Store and expose `MappingOptions` on `Engine`

**Files:**
- Modify: `internal/mapper/mapper.go` (Engine struct, NewEngine, new methods)
- Modify: `internal/mapper/mapper_test.go`

- [ ] **Step 1: Write the failing test**

Add to `internal/mapper/mapper_test.go`:

```go
func TestEngineMappingOptionsRoundTrip(t *testing.T) {
    e := NewEngine("")
    if got := e.GetMappingOptions(); got != DefaultMappingOptions() {
        t.Errorf("GetMappingOptions() = %+v, want %+v", got, DefaultMappingOptions())
    }
    e.SetMappingOptions(MappingOptions{Vnum: false, Hash: true})
    if got := e.GetMappingOptions(); got != (MappingOptions{Vnum: false, Hash: true}) {
        t.Errorf("after SetMappingOptions: got %+v", got)
    }
}

func TestEngineMappingOptionsConcurrent(t *testing.T) {
    e := NewEngine("")
    var wg sync.WaitGroup
    for i := 0; i < 50; i++ {
        wg.Add(2)
        go func() { defer wg.Done(); e.SetMappingOptions(MappingOptions{Vnum: true, Hash: false}) }()
        go func() { defer wg.Done(); _ = e.GetMappingOptions() }()
    }
    wg.Wait()
}
```

Ensure `sync` is imported in the test file (it likely already is).

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/mapper/ -run "TestEngineMappingOptions" -v`
Expected: FAIL with `e.GetMappingOptions undefined` and `e.SetMappingOptions undefined`.

- [ ] **Step 3: Add field, init, and methods**

In `internal/mapper/mapper.go`:

Add field to `Engine` struct (around line 122, end of the struct):

```go
type Engine struct {
    // ...existing fields...
    options MappingOptions
}
```

Initialize in `NewEngine` (around line 144, replace existing return):

```go
func NewEngine(path string) *Engine {
    return &Engine{
        data: &Map{
            Rooms: make(map[string]*Room),
        },
        path:      path,
        undo:      make([]mapCommand, 0),
        paths:     DefaultPaths(),
        hashIndex: make(map[string]string),
        options:   DefaultMappingOptions(),
    }
}
```

Add new methods (place near `SetPaths`/`GetPaths`, around line 156):

```go
// SetMappingOptions replaces the loop-detection options. Safe for concurrent use.
func (e *Engine) SetMappingOptions(opts MappingOptions) {
    e.mu.Lock()
    defer e.mu.Unlock()
    e.options = opts
}

// GetMappingOptions returns the current loop-detection options.
func (e *Engine) GetMappingOptions() MappingOptions {
    e.mu.RLock()
    defer e.mu.RUnlock()
    return e.options
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/mapper/ -run "TestEngineMappingOptions" -v -race`
Expected: PASS for both tests, with no race detected.

- [ ] **Step 5: Commit**

```bash
git add internal/mapper/mapper.go internal/mapper/mapper_test.go
git commit -m "feat(mapper): expose MappingOptions getter/setter on Engine"
```

---

### Task 3: Gate `HandleGMCPRoomInfo` on options

**Files:**
- Modify: `internal/mapper/mapper.go:854` (`HandleGMCPRoomInfo`)
- Modify: `internal/mapper/mapper_test.go`

Goal: when `Vnum=true`, prefer `r.Vnum` for both ID and lookup; when `Vnum=false`, ignore the vnum entirely (use UUID). When `Hash=true`, fall back to hash lookup; when `Hash=false`, do not look up by hash and do not write to `hashIndex`. Defaults (`Vnum=true, Hash=false`): vnum is honored, hash is bypassed.

- [ ] **Step 1: Write failing tests**

Add to `internal/mapper/mapper_test.go`:

```go
func TestHandleGMCPRoomInfo_VnumOff_DoesNotCollapse(t *testing.T) {
    e := NewEngine("")
    if err := e.Create(""); err != nil {
        t.Fatalf("Create: %v", err)
    }
    e.SetMappingOptions(MappingOptions{Vnum: false, Hash: false})
    e.StartAutoMapping()

    // First move north, GMCP delivers vnum 100.
    if _, _, err := e.ProcessMovement("n"); err != nil {
        t.Fatalf("ProcessMovement: %v", err)
    }
    _, _, _, err := e.HandleGMCPRoomInfo(GMCPRoom{Vnum: "100", Name: "Plaza", Description: "A plaza.", Exits: []string{"s"}})
    if err != nil {
        t.Fatalf("HandleGMCPRoomInfo: %v", err)
    }

    // Walk back, then north again — same vnum delivered.
    if _, _, err := e.ProcessMovement("s"); err != nil {
        t.Fatalf("ProcessMovement: %v", err)
    }
    if _, _, err := e.ProcessMovement("n"); err != nil {
        t.Fatalf("ProcessMovement: %v", err)
    }
    // The exit already exists from the first walk; this is not a duplicate
    // creation case. Use a NEW direction with same vnum to test gating.
    if _, _, err := e.ProcessMovement("e"); err != nil {
        t.Fatalf("ProcessMovement: %v", err)
    }
    _, _, loop, err := e.HandleGMCPRoomInfo(GMCPRoom{Vnum: "100", Name: "Plaza", Description: "A plaza.", Exits: []string{"w"}})
    if err != nil {
        t.Fatalf("HandleGMCPRoomInfo (2nd): %v", err)
    }
    if loop {
        t.Errorf("loopDetected = true with Vnum=false, want false (room must be duplicated)")
    }
}

func TestHandleGMCPRoomInfo_VnumOn_CollapsesByVnum(t *testing.T) {
    e := NewEngine("")
    if err := e.Create(""); err != nil {
        t.Fatalf("Create: %v", err)
    }
    e.SetMappingOptions(MappingOptions{Vnum: true, Hash: false})
    e.StartAutoMapping()

    if _, _, err := e.ProcessMovement("n"); err != nil {
        t.Fatalf("ProcessMovement: %v", err)
    }
    _, _, _, _ = e.HandleGMCPRoomInfo(GMCPRoom{Vnum: "100", Name: "Plaza", Description: "A plaza.", Exits: []string{"s"}})

    // Walk back south, then go east, GMCP returns Vnum 100 again.
    if _, _, err := e.ProcessMovement("s"); err != nil {
        t.Fatalf("ProcessMovement: %v", err)
    }
    if _, _, err := e.ProcessMovement("e"); err != nil {
        t.Fatalf("ProcessMovement: %v", err)
    }
    _, _, loop, _ := e.HandleGMCPRoomInfo(GMCPRoom{Vnum: "100", Name: "Plaza", Description: "A plaza.", Exits: []string{"w"}})
    if !loop {
        t.Errorf("loopDetected = false with Vnum=true, want true")
    }
}

func TestHandleGMCPRoomInfo_HashOff_DoesNotPopulateIndex(t *testing.T) {
    e := NewEngine("")
    if err := e.Create(""); err != nil {
        t.Fatalf("Create: %v", err)
    }
    e.SetMappingOptions(MappingOptions{Vnum: false, Hash: false})
    e.StartAutoMapping()

    if _, _, err := e.ProcessMovement("n"); err != nil {
        t.Fatalf("ProcessMovement: %v", err)
    }
    _, _, _, _ = e.HandleGMCPRoomInfo(GMCPRoom{Name: "X", Description: "desc", Exits: []string{"s"}})

    e.mu.RLock()
    n := len(e.hashIndex)
    e.mu.RUnlock()
    if n != 0 {
        t.Errorf("hashIndex size = %d after Hash=false, want 0", n)
    }
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/mapper/ -run "TestHandleGMCPRoomInfo_" -v`
Expected: FAIL — current code always uses vnum when present and always populates hashIndex.

- [ ] **Step 3: Update `HandleGMCPRoomInfo`**

Replace the body of `HandleGMCPRoomInfo` from "Determine room identifier" through the new-room branch (currently `mapper.go:894-915`). Replace with:

```go
    // No pending movement — just update current room info.
    if e.pendingDir == "" {
        if curr, ok := e.data.Rooms[e.data.CurrentRoom]; ok {
            if r.Description != "" {
                curr.Description = r.Description
                if e.options.Hash {
                    curr.DescriptionHash = ComputeRoomHash(r.Description, exits)
                    e.hashIndex[curr.DescriptionHash] = curr.ID
                }
            }
            if r.Name != "" {
                curr.Name = r.Name
            }
        }
        return false, "", false, nil
    }

    fromRoom, ok := e.data.Rooms[e.pendingFromRoom]
    if !ok {
        e.pendingDir = ""
        e.pendingFromRoom = ""
        return false, "", false, fmt.Errorf("source room not found")
    }

    // Loop detection — try strategies in priority order, gated by options.
    if e.options.Vnum && r.Vnum != "" {
        if existingRoom, found := e.data.Rooms[r.Vnum]; found {
            e.saveState()
            e.completePendingMovement(fromRoom, e.pendingDir, r.Vnum, existingRoom)
            return true, existingRoom.Name, true, nil
        }
    }
    if e.options.Hash && r.Description != "" {
        hash := ComputeRoomHash(r.Description, exits)
        if existingID, found := e.hashIndex[hash]; found {
            if existingRoom, ok := e.data.Rooms[existingID]; ok {
                e.saveState()
                e.completePendingMovement(fromRoom, e.pendingDir, existingID, existingRoom)
                return true, existingRoom.Name, true, nil
            }
        }
    }

    // New room. Choose ID: vnum if Vnum on and present, else UUID.
    var newID string
    if e.options.Vnum && r.Vnum != "" {
        newID = r.Vnum
    } else {
        newID = uuid.New().String()
    }

    e.saveState()
    var descHash string
    if e.options.Hash {
        descHash = ComputeRoomHash(r.Description, exits)
    }
    newRoom := e.createRoom(fromRoom, e.pendingDir, newID, r.Name, r.Description, descHash)
    return true, newRoom.Name, false, nil
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/mapper/ -run "TestHandleGMCPRoomInfo_" -v`
Expected: PASS for the three new tests.

- [ ] **Step 5: Commit**

```bash
git add internal/mapper/mapper.go internal/mapper/mapper_test.go
git commit -m "feat(mapper): gate GMCP room handling on MappingOptions"
```

---

### Task 4: Gate `ProcessRoomData` on `Hash`

**Files:**
- Modify: `internal/mapper/mapper.go:791` (`ProcessRoomData`)
- Modify: `internal/mapper/mapper_test.go`

- [ ] **Step 1: Write failing tests**

Add to `internal/mapper/mapper_test.go`:

```go
func TestProcessRoomData_HashOff_AlwaysCreates(t *testing.T) {
    e := NewEngine("")
    if err := e.Create(""); err != nil {
        t.Fatalf("Create: %v", err)
    }
    e.SetMappingOptions(MappingOptions{Vnum: false, Hash: false})
    e.StartAutoMapping()

    sample := "\tA grey room.\nObvious exits: south.\n"

    // Move north → process data; create new room.
    if _, _, err := e.ProcessMovement("n"); err != nil {
        t.Fatalf("ProcessMovement n: %v", err)
    }
    if _, _, _, err := e.ProcessRoomData(sample); err != nil {
        t.Fatalf("ProcessRoomData (1): %v", err)
    }

    // Move back south, then east, deliver identical text.
    if _, _, err := e.ProcessMovement("s"); err != nil {
        t.Fatalf("ProcessMovement s: %v", err)
    }
    if _, _, err := e.ProcessMovement("e"); err != nil {
        t.Fatalf("ProcessMovement e: %v", err)
    }
    _, _, loop, err := e.ProcessRoomData(sample)
    if err != nil {
        t.Fatalf("ProcessRoomData (2): %v", err)
    }
    if loop {
        t.Errorf("loopDetected = true with Hash=false; want false")
    }

    e.mu.RLock()
    n := len(e.hashIndex)
    e.mu.RUnlock()
    if n != 0 {
        t.Errorf("hashIndex size = %d, want 0 with Hash=false", n)
    }
}

func TestProcessRoomData_HashOn_PreservesLegacyBehavior(t *testing.T) {
    e := NewEngine("")
    if err := e.Create(""); err != nil {
        t.Fatalf("Create: %v", err)
    }
    e.SetMappingOptions(MappingOptions{Vnum: false, Hash: true})
    e.StartAutoMapping()

    sample := "\tA grey room.\nObvious exits: south.\n"
    if _, _, err := e.ProcessMovement("n"); err != nil {
        t.Fatalf("ProcessMovement n: %v", err)
    }
    if _, _, _, err := e.ProcessRoomData(sample); err != nil {
        t.Fatalf("ProcessRoomData (1): %v", err)
    }

    if _, _, err := e.ProcessMovement("s"); err != nil {
        t.Fatalf("ProcessMovement s: %v", err)
    }
    if _, _, err := e.ProcessMovement("e"); err != nil {
        t.Fatalf("ProcessMovement e: %v", err)
    }
    _, _, loop, _ := e.ProcessRoomData(sample)
    if !loop {
        t.Errorf("loopDetected = false with Hash=true; want true (legacy behavior)")
    }
}
```

- [ ] **Step 2: Run tests**

Run: `go test ./internal/mapper/ -run "TestProcessRoomData_Hash" -v`
Expected: FAIL — current implementation ignores options.

- [ ] **Step 3: Update `ProcessRoomData`**

Replace the body of `ProcessRoomData` (currently `mapper.go:791-838`):

```go
func (e *Engine) ProcessRoomData(text string) (processed bool, roomName string, loopDetected bool, err error) {
    if !e.IsAutoMapping() {
        return false, "", false, nil
    }

    e.mu.Lock()
    defer e.mu.Unlock()

    // No pending movement — update current room info if present.
    if e.pendingDir == "" {
        if curr, ok := e.data.Rooms[e.data.CurrentRoom]; ok {
            roomData := ParseRoomData(text)
            if roomData.Description != "" {
                curr.Description = roomData.RawText
                if e.options.Hash {
                    hash := ComputeRoomHash(roomData.Description, roomData.Exits)
                    curr.DescriptionHash = hash
                    e.hashIndex[hash] = curr.ID
                }
            }
        }
        return false, "", false, nil
    }

    roomData := ParseRoomData(text)

    fromRoom, ok := e.data.Rooms[e.pendingFromRoom]
    if !ok {
        e.pendingDir = ""
        e.pendingFromRoom = ""
        return false, "", false, fmt.Errorf("source room not found")
    }

    // Loop detection by hash — only when Hash is enabled.
    if e.options.Hash {
        hash := ComputeRoomHash(roomData.Description, roomData.Exits)
        if existingID, found := e.hashIndex[hash]; found {
            if existingRoom, ok := e.data.Rooms[existingID]; ok {
                e.saveState()
                e.completePendingMovement(fromRoom, e.pendingDir, existingID, existingRoom)
                return true, existingRoom.Name, true, nil
            }
        }
    }

    // New room.
    e.saveState()
    var descHash string
    if e.options.Hash {
        descHash = ComputeRoomHash(roomData.Description, roomData.Exits)
    }
    newRoom := e.createRoom(fromRoom, e.pendingDir, uuid.New().String(), "New Room", roomData.RawText, descHash)
    return true, newRoom.Name, false, nil
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/mapper/ -run "TestProcessRoomData_Hash" -v`
Expected: PASS for both tests.

- [ ] **Step 5: Commit**

```bash
git add internal/mapper/mapper.go internal/mapper/mapper_test.go
git commit -m "feat(mapper): gate ProcessRoomData hash lookup on options.Hash"
```

---

### Task 5: Gate `createRoom` hash-index write

**Files:**
- Modify: `internal/mapper/mapper.go:732` (`createRoom`)

`createRoom` currently writes `e.hashIndex[descHash] = id` unconditionally. With Tasks 3 and 4 already passing `descHash = ""` when `Hash=false`, the index would still get a `""` key and pollute lookups. Gate the write explicitly to be safe.

- [ ] **Step 1: Write failing test**

Add to `internal/mapper/mapper_test.go`:

```go
func TestCreateRoom_HashOff_NoIndexEntry(t *testing.T) {
    e := NewEngine("")
    if err := e.Create(""); err != nil {
        t.Fatalf("Create: %v", err)
    }
    e.SetMappingOptions(MappingOptions{Vnum: false, Hash: false})
    e.StartAutoMapping()

    if _, _, err := e.ProcessMovement("n"); err != nil {
        t.Fatalf("ProcessMovement: %v", err)
    }
    if _, _, _, err := e.ProcessRoomData("\tA room.\nObvious exits: south.\n"); err != nil {
        t.Fatalf("ProcessRoomData: %v", err)
    }

    e.mu.RLock()
    _, hasEmpty := e.hashIndex[""]
    n := len(e.hashIndex)
    e.mu.RUnlock()
    if hasEmpty {
        t.Errorf("hashIndex contains empty-string key; should not pollute index when Hash=false")
    }
    if n != 0 {
        t.Errorf("hashIndex size = %d; want 0 when Hash=false", n)
    }
}
```

- [ ] **Step 2: Run test**

Run: `go test ./internal/mapper/ -run TestCreateRoom_HashOff -v`
Expected: FAIL — `createRoom` currently writes `e.hashIndex[""] = id`.

- [ ] **Step 3: Gate the write in `createRoom`**

In `internal/mapper/mapper.go` around line 778, replace:

```go
    e.data.Rooms[id] = room
    e.hashIndex[descHash] = id
    e.data.CurrentRoom = id
```

with:

```go
    e.data.Rooms[id] = room
    if e.options.Hash && descHash != "" {
        e.hashIndex[descHash] = id
    }
    e.data.CurrentRoom = id
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/mapper/ -run TestCreateRoom_HashOff -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/mapper/mapper.go internal/mapper/mapper_test.go
git commit -m "fix(mapper): skip hashIndex write when options.Hash is off"
```

---

### Task 6: Make existing mapper tests pass under new defaults

The existing tests in `internal/mapper/mapper_test.go` exercise hash-based loop detection assuming it is on. With the new default `Hash=false`, those tests will fail. Fix by adding `e.SetMappingOptions(MappingOptions{Vnum: true, Hash: true})` immediately after each `NewEngine(...)` (and after `e.Create(...)` if `Create` is called) in any test that relies on loop-detection behavior.

- [ ] **Step 1: Run the full mapper test suite to find regressions**

Run: `go test ./internal/mapper/ -v`
Note any failing tests beyond those introduced in Tasks 1–5. Expected typical failures: existing loop-detection tests that walk twice and expect re-linking.

- [ ] **Step 2: Patch each failing test**

For each failing test that creates an engine and expects hashing to detect a loop, add this immediately after the `NewEngine`/`Create` lines:

```go
e.SetMappingOptions(MappingOptions{Vnum: true, Hash: true})
```

Tests in `internal/mapper/mapper_test.go` that are about pure structural operations (`Dig`, `Goto`, `Link`, `Delete`, `Undo`, `SetName`, `SetDescription` without hashing assertions, `Show`) do **not** need the change.

- [ ] **Step 3: Run the full suite**

Run: `go test ./internal/mapper/ -v -race`
Expected: PASS for all tests.

- [ ] **Step 4: Commit**

```bash
git add internal/mapper/mapper_test.go
git commit -m "test(mapper): enable legacy Hash=true in tests that rely on it"
```

**CHECKPOINT A complete.** Pause for review: `go test ./internal/mapper/ -race -v` should be green.

---

## Checkpoint B — `/map option` command

### Task 7: Add `MapOption` command type

**Files:**
- Modify: `internal/command/command.go`

- [ ] **Step 1: Add type and marker**

Append to the Mapper section of `internal/command/command.go` (after the existing `MapExit` declarations around line 81):

```go
// MapOption sets a single loop-detection strategy flag, or applies the "none"
// shortcut. When Print is true, the action is "show current settings" and
// other fields are ignored. Strategy must be one of: "vnum", "hash", "none".
type MapOption struct {
    Print    bool   // True for bare /map option (no args)
    Strategy string // "vnum" | "hash" | "none"
    Enable   bool   // Ignored when Strategy == "none" (always disables)
}

func (*MapOption) isCommand() {}
```

- [ ] **Step 2: Verify it compiles**

Run: `go build ./internal/command/`
Expected: builds clean.

- [ ] **Step 3: Commit**

```bash
git add internal/command/command.go
git commit -m "feat(command): add MapOption command type"
```

---

### Task 8: Parse `/map option` in `cmdMap`

**Files:**
- Modify: `internal/input/handler.go:617` (`cmdMap`)
- Modify: `internal/input/handler_test.go`

- [ ] **Step 1: Write failing tests**

Add to `internal/input/handler_test.go`:

```go
func TestCmdMap_OptionPrint(t *testing.T) {
    h := NewHandler()
    res := h.Process("/map option")
    cmd, ok := res.Command.(*command.MapOption)
    if !ok {
        t.Fatalf("Command type = %T, want *command.MapOption", res.Command)
    }
    if !cmd.Print {
        t.Errorf("Print = false, want true")
    }
}

func TestCmdMap_OptionVnumOn(t *testing.T) {
    h := NewHandler()
    res := h.Process("/map option vnum on")
    cmd, ok := res.Command.(*command.MapOption)
    if !ok {
        t.Fatalf("Command type = %T, want *command.MapOption", res.Command)
    }
    if cmd.Print || cmd.Strategy != "vnum" || !cmd.Enable {
        t.Errorf("got %+v, want {Strategy:vnum Enable:true}", cmd)
    }
}

func TestCmdMap_OptionHashOff(t *testing.T) {
    h := NewHandler()
    res := h.Process("/map option hash off")
    cmd := res.Command.(*command.MapOption)
    if cmd.Strategy != "hash" || cmd.Enable {
        t.Errorf("got %+v, want {Strategy:hash Enable:false}", cmd)
    }
}

func TestCmdMap_OptionNone(t *testing.T) {
    h := NewHandler()
    res := h.Process("/map option none")
    cmd := res.Command.(*command.MapOption)
    if cmd.Strategy != "none" {
        t.Errorf("Strategy = %q, want \"none\"", cmd.Strategy)
    }
}

func TestCmdMap_OptionInvalid(t *testing.T) {
    h := NewHandler()
    cases := []string{
        "/map option vnum",
        "/map option vnum maybe",
        "/map option foo on",
        "/map option none on",
    }
    for _, in := range cases {
        res := h.Process(in)
        if res.Command != nil {
            t.Errorf("%q produced Command %+v, want nil", in, res.Command)
        }
        if res.Response == "" {
            t.Errorf("%q produced empty Response, want usage message", in)
        }
    }
}
```

(The test file already imports `command` — confirm; if not, add `"github.com/ayder/gotin/internal/command"` to the imports.)

If the existing test helper for parsing single inputs is not `h.Process(line)`, look at one of the existing tests in `handler_test.go` and use the same entry point. (Existing tests around `handler_test.go:164` show the pattern.)

- [ ] **Step 2: Run tests**

Run: `go test ./internal/input/ -run "TestCmdMap_Option" -v`
Expected: FAIL — `option` not recognized; falls through to "Unknown map subcommand".

- [ ] **Step 3: Add `option` case to `cmdMap`**

In `internal/input/handler.go:617`, inside the `switch subcmd` block, before `default:`:

```go
    case "option":
        if len(subargs) == 0 {
            return ParseResult{Command: &command.MapOption{Print: true}}
        }
        strat := strings.ToLower(subargs[0])
        switch strat {
        case "vnum", "hash":
            if len(subargs) != 2 {
                return ParseResult{Response: "Usage: /map option " + strat + " on|off"}
            }
            switch strings.ToLower(subargs[1]) {
            case "on":
                return ParseResult{Command: &command.MapOption{Strategy: strat, Enable: true}}
            case "off":
                return ParseResult{Command: &command.MapOption{Strategy: strat, Enable: false}}
            default:
                return ParseResult{Response: "Usage: /map option " + strat + " on|off"}
            }
        case "none":
            if len(subargs) != 1 {
                return ParseResult{Response: "Usage: /map option none"}
            }
            return ParseResult{Command: &command.MapOption{Strategy: "none"}}
        default:
            return ParseResult{Response: "Usage: /map option [vnum|hash] [on|off] | /map option none | /map option"}
        }
```

Also extend the existing top-level usage string in this function (around line 620) to include `option`:

```go
            Response: "Usage: /map <subcommand> [args...]\nSubcommands: create, paths, dig, undo, delete, goto, link, name, search, show, info, option, start, stop, exit",
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/input/ -run "TestCmdMap_Option" -v`
Expected: PASS for all five tests.

Run: `go test ./internal/input/ -v`
Expected: full input package test suite passes (no regressions).

- [ ] **Step 5: Commit**

```bash
git add internal/input/handler.go internal/input/handler_test.go
git commit -m "feat(input): parse /map option vnum|hash|none"
```

---

### Task 9: Dispatch `*command.MapOption` in `app.go`

**Files:**
- Modify: `internal/app/app.go` (mapper command dispatch block, around lines 705–860)

The dispatch handler updates the engine's options, prints the new state, and (in Task 14) persists. For now, only the engine and the user-visible echo. Persistence is wired in Task 14 — leave a `s.scheduleMapOptionsSave()` call site here that we'll define then.

- [ ] **Step 1: Add the case**

After `case *command.MapExit:` block (around line 854), insert before the closing brace of the surrounding switch:

```go
    case *command.MapOption:
        opts := s.mapEngine.GetMappingOptions()
        if c.Print {
            s.program.Send(ui.StatusMsg{Message: fmt.Sprintf("mapping options: vnum=%s, hash=%s\n", onOff(opts.Vnum), onOff(opts.Hash))})
            return false, false
        }
        switch c.Strategy {
        case "vnum":
            opts.Vnum = c.Enable
        case "hash":
            opts.Hash = c.Enable
        case "none":
            opts.Vnum = false
            opts.Hash = false
        default:
            s.program.Send(ui.StatusMsg{Message: fmt.Sprintf("Unknown mapping strategy: %s\n", c.Strategy)})
            return false, false
        }
        s.mapEngine.SetMappingOptions(opts)
        s.scheduleMapOptionsSave(opts)
        s.program.Send(ui.StatusMsg{Message: fmt.Sprintf("mapping options: vnum=%s, hash=%s\n", onOff(opts.Vnum), onOff(opts.Hash))})
```

Add a small helper somewhere near the bottom of `app.go` (just above `historyFilePath` is fine):

```go
func onOff(b bool) string {
    if b {
        return "on"
    }
    return "off"
}

// scheduleMapOptionsSave will be implemented in the persistence task; for now
// it is a no-op so dispatch compiles.
func (s *Session) scheduleMapOptionsSave(_ mapper.MappingOptions) {}
```

- [ ] **Step 2: Build**

Run: `go build ./...`
Expected: builds clean.

- [ ] **Step 3: Smoke-test by running the command**

This is harder to unit-test since dispatch is wired through goroutines. Manual smoke later (Task 23). Just verify it builds.

- [ ] **Step 4: Commit**

```bash
git add internal/app/app.go
git commit -m "feat(app): dispatch /map option to mapper engine"
```

**CHECKPOINT B complete.** `/map option` is parsed and applied at runtime, not yet persisted.

---

## Checkpoint C — MXP `<ROOMNAME>` → mapper

### Task 10: `pendingName` latch and `SetIncomingRoomName`

**Files:**
- Modify: `internal/mapper/mapper.go`
- Modify: `internal/mapper/mapper_test.go`

- [ ] **Step 1: Write failing tests**

Add to `internal/mapper/mapper_test.go`:

```go
func TestSetIncomingRoomName_PendingMovement_NamesNewRoom(t *testing.T) {
    e := NewEngine("")
    if err := e.Create(""); err != nil {
        t.Fatalf("Create: %v", err)
    }
    e.SetMappingOptions(MappingOptions{Vnum: false, Hash: false})
    e.StartAutoMapping()

    if _, _, err := e.ProcessMovement("n"); err != nil {
        t.Fatalf("ProcessMovement: %v", err)
    }
    e.SetIncomingRoomName("Town Plaza")

    _, _, _, err := e.ProcessRoomData("\tA plaza.\nObvious exits: south.\n")
    if err != nil {
        t.Fatalf("ProcessRoomData: %v", err)
    }

    curr := e.GetCurrent()
    if curr == nil || curr.Name != "Town Plaza" {
        t.Errorf("current room name = %q, want %q", curr.Name, "Town Plaza")
    }
}

func TestSetIncomingRoomName_NoPending_RenamesCurrent(t *testing.T) {
    e := NewEngine("")
    if err := e.Create(""); err != nil {
        t.Fatalf("Create: %v", err)
    }
    e.SetIncomingRoomName("Override")
    if got := e.GetCurrent().Name; got != "Override" {
        t.Errorf("current name = %q, want %q", got, "Override")
    }
}

func TestSetIncomingRoomName_LastWriteWins(t *testing.T) {
    e := NewEngine("")
    if err := e.Create(""); err != nil {
        t.Fatalf("Create: %v", err)
    }
    e.SetMappingOptions(MappingOptions{Vnum: false, Hash: false})
    e.StartAutoMapping()
    if _, _, err := e.ProcessMovement("n"); err != nil {
        t.Fatalf("ProcessMovement: %v", err)
    }
    e.SetIncomingRoomName("First")
    e.SetIncomingRoomName("Second")
    if _, _, _, err := e.ProcessRoomData("\tdesc\nObvious exits: south.\n"); err != nil {
        t.Fatalf("ProcessRoomData: %v", err)
    }
    if got := e.GetCurrent().Name; got != "Second" {
        t.Errorf("name = %q, want %q (last writer wins)", got, "Second")
    }
}
```

- [ ] **Step 2: Run tests**

Run: `go test ./internal/mapper/ -run TestSetIncomingRoomName -v`
Expected: FAIL — `SetIncomingRoomName` undefined.

- [ ] **Step 3: Add `pendingName` field, `SetIncomingRoomName`, and consume in `createRoom`**

In `internal/mapper/mapper.go`, add `pendingName string` to `Engine` (with the other smart-mapping fields around line 130):

```go
    // Smart auto-mapping state
    hashIndex       map[string]string // Hash -> RoomID for loop detection
    pendingDir      Direction         // Direction we're moving in (for linking after room data arrives)
    pendingFromRoom string            // Room ID we're moving from
    pendingName     string            // One-slot latch for out-of-band room name (e.g. MXP <ROOMNAME>)
```

Add new method (place near `HasPendingMovement` around line 919):

```go
// SetIncomingRoomName attaches a name supplied out-of-band (e.g. via MXP
// <ROOMNAME>) to the room that the next ProcessRoomData / HandleGMCPRoomInfo
// call will create. If no movement is pending, the name is applied to the
// current room immediately. Repeated calls before consumption: last write
// wins.
func (e *Engine) SetIncomingRoomName(name string) {
    e.mu.Lock()
    defer e.mu.Unlock()
    if name == "" {
        return
    }
    if e.pendingDir != "" {
        e.pendingName = name
        return
    }
    if curr, ok := e.data.Rooms[e.data.CurrentRoom]; ok {
        curr.Name = name
    }
}
```

Update `createRoom` (around line 732) to consume `pendingName` when the caller passed an empty `name`. Replace the existing line:

```go
    room := &Room{
        ID:              id,
        Name:            name,
```

with:

```go
    effectiveName := name
    if effectiveName == "" || effectiveName == "New Room" {
        if e.pendingName != "" {
            effectiveName = e.pendingName
        }
    }
    e.pendingName = ""

    room := &Room{
        ID:              id,
        Name:            effectiveName,
```

Also clear `pendingName` in `completePendingMovement` (around line 716): the linker is reaching an existing room rather than creating one, so any latched name should be dropped to avoid leaking into a later `dig`. After the block that sets `e.pendingDir = ""` add:

```go
    e.pendingName = ""
```

inside `completePendingMovement`, immediately after the existing `e.pendingFromRoom = ""`.

And in `CancelPendingMovement` (around line 926), also clear `pendingName`:

```go
func (e *Engine) CancelPendingMovement() {
    e.mu.Lock()
    defer e.mu.Unlock()
    e.pendingDir = ""
    e.pendingFromRoom = ""
    e.pendingName = ""
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/mapper/ -run TestSetIncomingRoomName -v -race`
Expected: PASS for all three tests.

Run: `go test ./internal/mapper/ -v -race`
Expected: full mapper suite still green.

- [ ] **Step 5: Commit**

```bash
git add internal/mapper/mapper.go internal/mapper/mapper_test.go
git commit -m "feat(mapper): SetIncomingRoomName latch for MXP <ROOMNAME>"
```

---

### Task 11: Wire MXP `<ROOMNAME>` callback into mapper

**Files:**
- Modify: `internal/app/app.go:388` (MXP callback block)

- [ ] **Step 1: Update the callback**

Replace the current callback block (lines 388–394) with:

```go
    if m := findMXP(c); m != nil {
        m.SetRoomNameCallback(func(name string) {
            if name == "" {
                return
            }
            s.mapEngine.SetIncomingRoomName(name)
            s.trySendUI(ui.RoomNameMsg{Name: name})
        })
    }
```

- [ ] **Step 2: Build**

Run: `go build ./...`
Expected: clean.

- [ ] **Step 3: Commit**

```bash
git add internal/app/app.go
git commit -m "feat(app): forward MXP <ROOMNAME> to mapper engine"
```

---

### Task 12: Update GMCP path to honor `pendingName` when GMCP omits the room name

GMCP `Room.Info` already supplies a `Name`, but when the MUD only sends MXP `<ROOMNAME>` and an empty-name `Room.Info`, the latch should still fill in the name on creation. `createRoom` already consumes `pendingName` (Task 10), so the only change is: when `r.Name` is empty in `HandleGMCPRoomInfo`, do not pass it through; let `createRoom`'s fallback engage.

- [ ] **Step 1: Write failing test**

Add to `internal/mapper/mapper_test.go`:

```go
func TestHandleGMCPRoomInfo_EmptyName_UsesIncomingLatch(t *testing.T) {
    e := NewEngine("")
    if err := e.Create(""); err != nil {
        t.Fatalf("Create: %v", err)
    }
    e.SetMappingOptions(MappingOptions{Vnum: true, Hash: false})
    e.StartAutoMapping()

    if _, _, err := e.ProcessMovement("n"); err != nil {
        t.Fatalf("ProcessMovement: %v", err)
    }
    e.SetIncomingRoomName("MXP Plaza")

    _, _, _, err := e.HandleGMCPRoomInfo(GMCPRoom{Vnum: "100", Name: "", Description: "desc", Exits: []string{"s"}})
    if err != nil {
        t.Fatalf("HandleGMCPRoomInfo: %v", err)
    }
    curr := e.GetCurrent()
    if curr == nil || curr.Name != "MXP Plaza" {
        t.Errorf("current room name = %q, want %q", curr.Name, "MXP Plaza")
    }
}
```

- [ ] **Step 2: Run test**

Run: `go test ./internal/mapper/ -run TestHandleGMCPRoomInfo_EmptyName -v`
Expected: PASS already if Task 10's `createRoom` change passes empty name through. If the test fails because `r.Name` is being written as empty string into the room, ensure `createRoom` is called with `r.Name` (which can be empty) and `effectiveName` falls back via the pendingName latch — which is exactly Task 10's logic. No code change needed here; the test is a regression guard.

If this test FAILS, debug Task 10 — the latch consume in `createRoom` must trigger when `name == ""`.

- [ ] **Step 3: Commit if a fix was needed; otherwise skip**

```bash
git add internal/mapper/mapper_test.go
git commit -m "test(mapper): GMCP empty Name falls back to incoming latch"
```

**CHECKPOINT C complete.** Pause for review.

---

## Checkpoint D — Persistence in `gotin.json`

### Task 13: Extend `ConnectionAlias` and `gotinData` schemas

**Files:**
- Modify: `internal/input/handler.go:16` (`ConnectionAlias`)
- Modify: `internal/app/app.go:42` (`gotinData`)

The `MappingOptions` type lives in `mapper`. Adding `*mapper.MappingOptions` to `input.ConnectionAlias` requires `input` to import `mapper`. There is no cycle (mapper does not import input).

- [ ] **Step 1: Extend `ConnectionAlias`**

In `internal/input/handler.go`, update the imports block:

```go
import (
    "fmt"
    "strconv"
    "strings"

    "github.com/ayder/gotin/internal/command"
    "github.com/ayder/gotin/internal/mapper"
    "github.com/ayder/gotin/internal/parser"
)
```

Update the struct (line 16) to:

```go
type ConnectionAlias struct {
    Host           string                  `json:"host"`
    Port           int                     `json:"port"`
    Auto           bool                    `json:"auto"`
    Protocols      map[string]bool         `json:"protocols,omitempty"`
    MappingOptions *mapper.MappingOptions  `json:"mapping_options,omitempty"`
}
```

- [ ] **Step 2: Extend `gotinData`**

In `internal/app/app.go:42`, update:

```go
type gotinData struct {
    Aliases        map[string]string                `json:"aliases,omitempty"`
    Triggers       []config.TriggerConfig           `json:"triggers,omitempty"`
    Connections    map[string]input.ConnectionAlias `json:"connections,omitempty"`
    MappingOptions *mapper.MappingOptions           `json:"mapping_options,omitempty"`
}
```

- [ ] **Step 3: Add a top-level field on `Session` to remember the active scope**

Add to `Session` struct (around line 265):

```go
    // mappingOptionsScope is "alias" when the active connection came from a
    // saved alias (so /map option writes to that alias's MappingOptions);
    // "global" otherwise (writes to top-level gotinData.MappingOptions).
    mappingOptionsScope string
    mappingOptionsAlias string // populated when scope == "alias"
    mappingOptionsTop   *mapper.MappingOptions
```

- [ ] **Step 4: Build**

Run: `go build ./...`
Expected: builds clean.

- [ ] **Step 5: Commit**

```bash
git add internal/input/handler.go internal/app/app.go
git commit -m "feat(config): add mapping_options to ConnectionAlias and gotinData"
```

---

### Task 14: Resolve options on connect; persist on `/map option`

**Files:**
- Modify: `internal/app/app.go` (`loadGotinData`, `connect`, `scheduleMapOptionsSave`, `saveGotinData`)

- [ ] **Step 1: Resolve options on `loadGotinData`**

In `loadGotinData` (around line 937, after the existing parsing of `td`), add — before the `if td.Connections != nil` block — a step to remember the top-level options:

```go
    if td.MappingOptions != nil {
        s.mappingOptionsTop = td.MappingOptions
    }
```

- [ ] **Step 2: Apply options at connect time**

In `connect()` (around line 380, just after `installProtocols(...)`), add:

```go
    // Resolve mapping options: per-alias > top-level > default.
    var resolved mapper.MappingOptions
    s.mappingOptionsScope = "global"
    s.mappingOptionsAlias = ""
    switch {
    case s.autoConnectFromAlias && s.autoConnectAlias.MappingOptions != nil:
        resolved = *s.autoConnectAlias.MappingOptions
        s.mappingOptionsScope = "alias"
        // find the alias name
        for name, ca := range s.model.GetConnections() {
            if ca.Host == s.autoConnectAlias.Host && ca.Port == s.autoConnectAlias.Port {
                s.mappingOptionsAlias = name
                break
            }
        }
    case s.mappingOptionsTop != nil:
        resolved = *s.mappingOptionsTop
    default:
        resolved = mapper.DefaultMappingOptions()
    }
    s.mapEngine.SetMappingOptions(resolved)
```

- [ ] **Step 3: Implement `scheduleMapOptionsSave`**

Replace the no-op stub from Task 9 with:

```go
func (s *Session) scheduleMapOptionsSave(opts mapper.MappingOptions) {
    if s.mappingOptionsScope == "alias" && s.mappingOptionsAlias != "" {
        conns := s.model.GetConnections()
        if ca, ok := conns[s.mappingOptionsAlias]; ok {
            o := opts
            ca.MappingOptions = &o
            conns[s.mappingOptionsAlias] = ca
            s.model.SetConnections(conns)
        }
    } else {
        o := opts
        s.mappingOptionsTop = &o
    }
    s.persistGotinData()
}
```

Add a small wrapper to write `gotin.json` from current state. `saveGotinData` already does this for explicit `/save`; we need an implicit version that writes to `gotin.json`.

Add near the existing `saveGotinData` (around line 873):

```go
func (s *Session) persistGotinData() {
    aliases := s.model.GetAliases()
    triggers := s.te.ListTriggers()
    connections := s.model.GetConnections()

    td := gotinData{
        Aliases:        aliases,
        Triggers:       make([]config.TriggerConfig, len(triggers)),
        Connections:    connections,
        MappingOptions: s.mappingOptionsTop,
    }
    for i, t := range triggers {
        td.Triggers[i] = config.TriggerConfig{
            Pattern:  t.Pattern.String(),
            Response: t.Response,
        }
    }

    fileData, err := json.MarshalIndent(td, "", "  ")
    if err != nil {
        return
    }
    tmp := "gotin.json.tmp"
    if err := os.WriteFile(tmp, fileData, 0644); err != nil {
        return
    }
    _ = os.Rename(tmp, "gotin.json")
}
```

Also update the existing `saveGotinData` (around line 873) so manual `/save` includes top-level options. Add to its `td := gotinData{...}` initializer:

```go
        MappingOptions: s.mappingOptionsTop,
```

- [ ] **Step 4: Build**

Run: `go build ./...`
Expected: clean.

- [ ] **Step 5: Commit**

```bash
git add internal/app/app.go
git commit -m "feat(app): persist /map option per-connection or top-level"
```

---

### Task 15: Round-trip persistence test

**Files:**
- Create: `internal/app/persistence_test.go` (or extend `internal/app/app_test.go` if it exists)

Persistence is in `app` and depends on side effects (`gotin.json` in cwd). Use a `t.TempDir()` working-directory swap to isolate.

- [ ] **Step 1: Write failing test**

Create `internal/app/persistence_test.go`:

```go
package app

import (
    "encoding/json"
    "os"
    "path/filepath"
    "testing"

    "github.com/ayder/gotin/internal/mapper"
)

func TestGotinDataMappingOptions_RoundTrip(t *testing.T) {
    dir := t.TempDir()
    cwd, err := os.Getwd()
    if err != nil {
        t.Fatalf("Getwd: %v", err)
    }
    if err := os.Chdir(dir); err != nil {
        t.Fatalf("Chdir: %v", err)
    }
    t.Cleanup(func() { _ = os.Chdir(cwd) })

    opts := mapper.MappingOptions{Vnum: false, Hash: true}
    td := gotinData{MappingOptions: &opts}
    blob, err := json.MarshalIndent(td, "", "  ")
    if err != nil {
        t.Fatalf("Marshal: %v", err)
    }
    if err := os.WriteFile(filepath.Join(dir, "gotin.json"), blob, 0644); err != nil {
        t.Fatalf("WriteFile: %v", err)
    }

    var got gotinData
    raw, err := os.ReadFile(filepath.Join(dir, "gotin.json"))
    if err != nil {
        t.Fatalf("ReadFile: %v", err)
    }
    if err := json.Unmarshal(raw, &got); err != nil {
        t.Fatalf("Unmarshal: %v", err)
    }
    if got.MappingOptions == nil {
        t.Fatalf("MappingOptions is nil after round trip")
    }
    if *got.MappingOptions != opts {
        t.Errorf("got %+v, want %+v", *got.MappingOptions, opts)
    }
}
```

- [ ] **Step 2: Run test**

Run: `go test ./internal/app/ -run TestGotinDataMappingOptions -v`
Expected: PASS (the schema is already in place from Task 13).

- [ ] **Step 3: Commit**

```bash
git add internal/app/persistence_test.go
git commit -m "test(app): gotinData mapping_options round trip"
```

**CHECKPOINT D complete.** Persistence works. Pause for review.

---

## Checkpoint E — `protolog` package + hooks

### Task 16: Create `protolog` package skeleton

**Files:**
- Create: `internal/mudproto/protolog/protolog.go`
- Create: `internal/mudproto/protolog/protolog_test.go`

- [ ] **Step 1: Write failing test**

Create `internal/mudproto/protolog/protolog_test.go`:

```go
package protolog

import (
    "bytes"
    "encoding/hex"
    "encoding/json"
    "strings"
    "sync"
    "testing"
)

func TestEntryJSONShape(t *testing.T) {
    e := Entry{
        TS:     "2026-04-26T12:00:00.000000000Z",
        Source: "mxp",
        Dir:    "rx",
        Event:  "tag",
        UTF8:   `<ROOMNAME>X</ROOMNAME>`,
        Hex:    "3c524f4f4e414d453e583c2f524f4f4d4e414d453e",
        Parsed: map[string]any{"name": "ROOMNAME"},
    }
    blob, err := json.Marshal(e)
    if err != nil {
        t.Fatalf("Marshal: %v", err)
    }
    var got map[string]any
    if err := json.Unmarshal(blob, &got); err != nil {
        t.Fatalf("Unmarshal: %v", err)
    }
    for _, k := range []string{"ts", "source", "dir", "event", "utf8", "hex", "parsed"} {
        if _, ok := got[k]; !ok {
            t.Errorf("entry missing key %q in %s", k, string(blob))
        }
    }
}

func TestJSONLinesLogger_WritesLinePerEntry(t *testing.T) {
    var buf bytes.Buffer
    enabled := func() bool { return true }
    lg := NewJSONLinesLogger(&buf, enabled)
    lg.Log(Entry{Source: "mxp", Dir: "rx", Event: "chunk", UTF8: "x", Hex: "78"})
    lg.Log(Entry{Source: "gmcp", Dir: "rx", Event: "chunk", UTF8: "y", Hex: "79"})

    lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
    if len(lines) != 2 {
        t.Fatalf("got %d lines, want 2: %q", len(lines), buf.String())
    }
    for _, line := range lines {
        var e Entry
        if err := json.Unmarshal([]byte(line), &e); err != nil {
            t.Errorf("Unmarshal %q: %v", line, err)
        }
        if e.TS == "" {
            t.Errorf("entry has empty TS: %q", line)
        }
    }
}

func TestJSONLinesLogger_DisabledIsNoOp(t *testing.T) {
    var buf bytes.Buffer
    lg := NewJSONLinesLogger(&buf, func() bool { return false })
    lg.Log(Entry{Source: "mxp", Dir: "rx", Event: "chunk", UTF8: "x"})
    if buf.Len() != 0 {
        t.Errorf("disabled logger wrote bytes: %q", buf.String())
    }
}

func TestJSONLinesLogger_Concurrent(t *testing.T) {
    var buf bytes.Buffer
    lg := NewJSONLinesLogger(&buf, func() bool { return true })
    var wg sync.WaitGroup
    for i := 0; i < 100; i++ {
        wg.Add(1)
        go func() {
            defer wg.Done()
            lg.Log(Entry{Source: "raw", Dir: "rx", Event: "chunk", UTF8: "x", Hex: "78"})
        }()
    }
    wg.Wait()
    lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
    if len(lines) != 100 {
        t.Fatalf("got %d lines, want 100", len(lines))
    }
    for _, line := range lines {
        var e Entry
        if err := json.Unmarshal([]byte(line), &e); err != nil {
            t.Errorf("malformed line %q: %v", line, err)
        }
    }
}

func TestHexUTF8Reversibility(t *testing.T) {
    raw := []byte("<\x1b[1z>X\n")
    h := EncodeHex(raw)
    dec, err := hex.DecodeString(h)
    if err != nil {
        t.Fatalf("hex decode: %v", err)
    }
    if !bytes.Equal(dec, raw) {
        t.Errorf("hex round-trip mismatch")
    }
    s := EncodeUTF8(raw)
    if s != `"<\x1b[1z>X\n"` {
        t.Errorf("EncodeUTF8 = %q, want %q", s, `"<\x1b[1z>X\n"`)
    }
}
```

- [ ] **Step 2: Run test**

Run: `go test ./internal/mudproto/protolog/ -v`
Expected: FAIL — package does not yet exist.

- [ ] **Step 3: Implement the package**

Create `internal/mudproto/protolog/protolog.go`:

```go
// Package protolog emits structured JSON Lines diagnostic events for the MXP,
// GMCP, and raw network paths. Each line is a self-contained JSON object so
// the log is greppable, jq-compatible, and resilient to interleaving when
// multiple sources write to the same file.
//
// The logger is gated by a caller-supplied enabled() predicate so call sites
// can short-circuit hex encoding when debug is off.
package protolog

import (
    "encoding/hex"
    "encoding/json"
    "io"
    "strconv"
    "sync"
    "time"
)

// Entry is one emitted log record. All fields are JSON-serialized; UTF8 and
// Hex are populated from the same raw byte slice when set.
type Entry struct {
    TS     string         `json:"ts"`               // RFC3339Nano; filled by Log if empty.
    Source string         `json:"source"`           // "mxp" | "gmcp" | "raw"
    Dir    string         `json:"dir"`              // "rx" | "tx"
    Event  string         `json:"event"`            // "chunk" | "tag" | "mode" | "probe" | "room_info" | "room_name"
    UTF8   string         `json:"utf8,omitempty"`   // Go-quoted form of the raw bytes (escaped non-printables).
    Hex    string         `json:"hex,omitempty"`    // Lowercase hex of the raw bytes.
    Parsed map[string]any `json:"parsed,omitempty"` // Optional structured payload.
}

// Logger emits entries and reports whether logging is currently enabled. Call
// sites should check Enabled() before formatting expensive payloads.
type Logger interface {
    Enabled() bool
    Log(e Entry)
}

// JSONLinesLogger writes one JSON object per line to an io.Writer, gated by
// a caller-supplied predicate.
type JSONLinesLogger struct {
    w       io.Writer
    enabled func() bool
    mu      sync.Mutex
}

// NewJSONLinesLogger constructs a logger writing to w. enabled is called for
// every Log() to decide whether to emit; if nil, logging is always on.
func NewJSONLinesLogger(w io.Writer, enabled func() bool) *JSONLinesLogger {
    if enabled == nil {
        enabled = func() bool { return true }
    }
    return &JSONLinesLogger{w: w, enabled: enabled}
}

func (l *JSONLinesLogger) Enabled() bool { return l != nil && l.enabled() }

func (l *JSONLinesLogger) Log(e Entry) {
    if !l.Enabled() {
        return
    }
    if e.TS == "" {
        e.TS = time.Now().UTC().Format(time.RFC3339Nano)
    }
    blob, err := json.Marshal(e)
    if err != nil {
        return
    }
    blob = append(blob, '\n')
    l.mu.Lock()
    defer l.mu.Unlock()
    _, _ = l.w.Write(blob)
}

// EncodeHex returns the lowercase hex encoding of b.
func EncodeHex(b []byte) string { return hex.EncodeToString(b) }

// EncodeUTF8 returns the Go-quoted (strconv.Quote) form of b. Non-printable
// bytes appear as \x01, \n, etc.; result includes surrounding quotes.
func EncodeUTF8(b []byte) string { return strconv.Quote(string(b)) }
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/mudproto/protolog/ -v -race`
Expected: PASS for all five tests.

- [ ] **Step 5: Commit**

```bash
git add internal/mudproto/protolog/protolog.go internal/mudproto/protolog/protolog_test.go
git commit -m "feat(protolog): JSON Lines logger with enabled-gating"
```

---

### Task 17: Hook protolog into MXP `Filter` for chunks, tags, room_name, modes

**Files:**
- Modify: `internal/mudproto/mxp/mxp.go`
- Modify: `internal/mudproto/mxp/mxp_test.go`

- [ ] **Step 1: Write failing test**

Add to `internal/mudproto/mxp/mxp_test.go`:

```go
func TestFilter_LoggerEmitsExpectedEvents(t *testing.T) {
    var buf bytes.Buffer
    lg := protolog.NewJSONLinesLogger(&buf, func() bool { return true })
    p := New()
    p.SetLogger(lg)

    // Mixed input: a tag, a ROOMNAME pair, a mode-switch, a newline.
    in := "<VERSION><ROOMNAME>Hall</ROOMNAME>\x1b[1z<NOBR>\nAfter\n"
    _ = p.Filter(in)

    lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
    if len(lines) == 0 {
        t.Fatalf("no log lines")
    }

    var sawChunk, sawTag, sawRoomName, sawMode bool
    for _, line := range lines {
        var e protolog.Entry
        if err := json.Unmarshal([]byte(line), &e); err != nil {
            t.Fatalf("malformed line %q: %v", line, err)
        }
        if e.Source != "mxp" {
            t.Errorf("source = %q, want mxp", e.Source)
        }
        switch e.Event {
        case "chunk":
            sawChunk = true
        case "tag":
            sawTag = true
        case "room_name":
            sawRoomName = true
            if got, _ := e.Parsed["name"].(string); got != "Hall" {
                t.Errorf("room_name parsed.name = %v, want Hall", e.Parsed["name"])
            }
        case "mode":
            sawMode = true
        }
    }
    if !sawChunk || !sawTag || !sawRoomName || !sawMode {
        t.Errorf("missing events: chunk=%v tag=%v room_name=%v mode=%v", sawChunk, sawTag, sawRoomName, sawMode)
    }
}
```

You will need to add imports to `mxp_test.go`:

```go
import (
    "bytes"
    "encoding/json"
    "strings"
    "testing"

    "github.com/ayder/gotin/internal/mudproto/protolog"
)
```

(Keep existing imports; add only what's missing.)

- [ ] **Step 2: Run test**

Run: `go test ./internal/mudproto/mxp/ -run TestFilter_LoggerEmitsExpectedEvents -v`
Expected: FAIL — `SetLogger` undefined.

- [ ] **Step 3: Add `SetLogger` and emit events**

In `internal/mudproto/mxp/mxp.go`:

Add import:

```go
"github.com/ayder/gotin/internal/mudproto/protolog"
```

Add field to `Protocol` struct:

```go
type Protocol struct {
    mode        Mode
    defaultMode Mode
    ctx         network.Context
    onRoomName  func(name string)
    log         protolog.Logger
}
```

Add setter (place near `SetRoomNameCallback`):

```go
// SetLogger installs a structured logger; nil disables logging.
func (p *Protocol) SetLogger(l protolog.Logger) { p.log = l }
```

Add a small internal helper to test-and-emit:

```go
func (p *Protocol) logEvent(event string, raw []byte, parsed map[string]any) {
    if p.log == nil || !p.log.Enabled() {
        return
    }
    e := protolog.Entry{Source: "mxp", Dir: "rx", Event: event, Parsed: parsed}
    if len(raw) > 0 {
        e.UTF8 = protolog.EncodeUTF8(raw)
        e.Hex = protolog.EncodeHex(raw)
    }
    p.log.Log(e)
}
```

In `Filter` (line 128), at the very top after the early-return for plain text:

```go
    if p.log != nil && p.log.Enabled() {
        p.logEvent("chunk", []byte(s), nil)
    }
```

Inside the `case '<':` branch, after `p.handleProbe(body)` and `p.handleRoomName(...)` — emit a `tag` event for the tag we consumed:

```go
            if end := strings.IndexByte(s[i:], '>'); end >= 0 {
                body := s[i+1 : i+end]
                p.handleProbe(body)
                p.handleRoomName(s, i+1, i+end, &b)
                p.logEvent("tag", []byte(s[i:i+end+1]), map[string]any{"name": extractTagName(body), "body": body})
                i += end + 1
                continue
            }
```

Inside `handleRoomName`, immediately after `p.onRoomName(room)`:

```go
        p.logEvent("room_name", []byte(s[tagStart-1:tagEnd+1+idx+len(closeTag)]), map[string]any{"name": room})
```

Inside `applyMode`, before the switch returns, emit:

```go
func (p *Protocol) applyMode(n int) {
    var modeName string
    switch n {
    case 0: p.mode = Open;     modeName = "Open"
    case 1: p.mode = Secure;   modeName = "Secure"
    case 2: p.mode = Locked;   modeName = "Locked"
    case 3: p.mode = p.defaultMode; modeName = "Reset"
    case 4: p.mode = Secure;   modeName = "TempSecure"
    case 5: p.defaultMode = Open;   p.mode = Open;   modeName = "LockOpen"
    case 6: p.defaultMode = Secure; p.mode = Secure; modeName = "LockSecure"
    case 7: p.defaultMode = Locked; p.mode = Locked; modeName = "LockLocked"
    default: modeName = "User"
    }
    p.logEvent("mode", nil, map[string]any{"n": n, "mode": modeName})
}
```

In `sendVersion` and `sendSupports`, after the existing `_ = p.ctx.Send(...)`, add:

```go
    p.logEvent("probe", []byte(msg), map[string]any{"kind": "version"})
```

(and `"kind": "supports"` in `sendSupports`, with its `msg`). Note: `Dir` for these events is `"tx"`. Adjust by adding a parameter or by directly writing `protolog.Entry{Source: "mxp", Dir: "tx", ...}` in those two sites:

```go
func (p *Protocol) logTx(event string, raw []byte, parsed map[string]any) {
    if p.log == nil || !p.log.Enabled() {
        return
    }
    p.log.Log(protolog.Entry{
        Source: "mxp", Dir: "tx", Event: event,
        UTF8:   protolog.EncodeUTF8(raw),
        Hex:    protolog.EncodeHex(raw),
        Parsed: parsed,
    })
}
```

Then in `sendVersion`: replace its plain `Send` block with also calling `p.logTx("probe", []byte(msg), map[string]any{"kind": "version"})`. Same for `sendSupports`.

- [ ] **Step 4: Run tests**

Run: `go test ./internal/mudproto/mxp/ -v`
Expected: existing MXP tests still pass; new logger test passes.

- [ ] **Step 5: Commit**

```bash
git add internal/mudproto/mxp/mxp.go internal/mudproto/mxp/mxp_test.go
git commit -m "feat(mxp): emit structured protolog events from Filter and probes"
```

---

### Task 18: Hook protolog into GMCP

**Files:**
- Modify: `internal/mudproto/gmcp/gmcp.go`
- Modify: `internal/mudproto/gmcp/gmcp_test.go`

- [ ] **Step 1: Write failing test**

Add to `internal/mudproto/gmcp/gmcp_test.go`:

```go
import (
    // ...existing...
    "bytes"
    "encoding/json"
    "strings"

    "github.com/ayder/gotin/internal/mudproto/protolog"
)

// fakeContext is a minimal network.Context double; reuse if existing tests
// already define one.
type fakeCtx struct{}

func (fakeCtx) Send([]byte) error                             { return nil }
func (fakeCtx) SendSubneg(byte, []byte) error                 { return nil }
func (fakeCtx) Debug(string, ...any)                          {}
func (fakeCtx) NotifyProtocolStatus()                         {}
func (fakeCtx) SwapReader(func(io.Reader) io.Reader)          {}

func TestGMCP_LoggerEmitsChunkOnSubneg(t *testing.T) {
    var buf bytes.Buffer
    lg := protolog.NewJSONLinesLogger(&buf, func() bool { return true })
    p := New(nil)
    p.SetLogger(lg)

    payload := []byte(`Room.Info {"num":"123","name":"Hall"}`)
    if err := p.OnSubnegotiation(fakeCtx{}, payload); err != nil {
        t.Fatalf("OnSubnegotiation: %v", err)
    }
    lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
    var sawChunk bool
    for _, line := range lines {
        var e protolog.Entry
        if err := json.Unmarshal([]byte(line), &e); err != nil {
            t.Fatalf("malformed line %q: %v", line, err)
        }
        if e.Source == "gmcp" && e.Event == "chunk" {
            sawChunk = true
        }
    }
    if !sawChunk {
        t.Errorf("no gmcp chunk event in log: %s", buf.String())
    }
}
```

If the existing GMCP test file already defines a Context double, reuse it instead of `fakeCtx`. Check `gmcp_test.go` first; the new test should compile with whichever helper is in scope.

- [ ] **Step 2: Run test**

Run: `go test ./internal/mudproto/gmcp/ -run TestGMCP_LoggerEmitsChunkOnSubneg -v`
Expected: FAIL — `SetLogger` undefined.

- [ ] **Step 3: Add `SetLogger` and emit chunk event**

In `internal/mudproto/gmcp/gmcp.go`:

Add import:

```go
"github.com/ayder/gotin/internal/mudproto/protolog"
```

Add field, setter, and emit in `OnSubnegotiation`:

```go
type Protocol struct {
    cb     Callback
    dataRx bool
    log    protolog.Logger
}

func (p *Protocol) SetLogger(l protolog.Logger) { p.log = l }

func (p *Protocol) OnSubnegotiation(ctx network.Context, data []byte) error {
    firstRx := !p.dataRx
    p.dataRx = true
    if firstRx {
        ctx.NotifyProtocolStatus()
    }
    if p.log != nil && p.log.Enabled() {
        p.log.Log(protolog.Entry{
            Source: "gmcp", Dir: "rx", Event: "chunk",
            UTF8:   protolog.EncodeUTF8(data),
            Hex:    protolog.EncodeHex(data),
        })
    }
    pkg, payload := split(data)
    ctx.Debug("GMCP: recv pkg=%q payload=%q", pkg, string(payload))
    if p.cb == nil {
        ctx.Debug("GMCP: no callback installed, dropping")
        return nil
    }
    p.cb(pkg, payload)
    return nil
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/mudproto/gmcp/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/mudproto/gmcp/gmcp.go internal/mudproto/gmcp/gmcp_test.go
git commit -m "feat(gmcp): emit structured protolog chunk events"
```

---

### Task 19: Emit `room_info` event in app's GMCP handler

**Files:**
- Modify: `internal/app/app.go:505` (`onGMCPRoomInfo`)

- [ ] **Step 1: Add session field for the logger**

Add to `Session`:

```go
    protoLog *protolog.JSONLinesLogger
```

Add the import to `internal/app/app.go`:

```go
"github.com/ayder/gotin/internal/mudproto/protolog"
```

- [ ] **Step 2: Emit `room_info` event when handling Room.Info**

Update `onGMCPRoomInfo` (around line 505):

```go
func (s *Session) onGMCPRoomInfo(pkg string, payload []byte) {
    if pkg != "Room.Info" {
        return
    }
    room, err := gmcp.ParseRoomInfo(payload)
    if err != nil {
        return
    }

    if s.protoLog != nil && s.protoLog.Enabled() {
        s.protoLog.Log(protolog.Entry{
            Source: "gmcp", Dir: "rx", Event: "room_info",
            UTF8:   protolog.EncodeUTF8(payload),
            Hex:    protolog.EncodeHex(payload),
            Parsed: map[string]any{
                "vnum":    room.Vnum,
                "name":    room.Name,
                "area":    room.Area,
                "exits":   room.Exits,
                "desc_len": len(room.Description),
            },
        })
    }

    if room.Name != "" {
        s.trySendUI(ui.RoomNameMsg{Name: room.Name})
    }

    processed, roomName, loopDetected, err := s.mapEngine.HandleGMCPRoomInfo(mapper.GMCPRoom{
        Vnum:        room.Vnum,
        Name:        room.Name,
        Description: room.Description,
        Area:        room.Area,
        Exits:       room.Exits,
    })
    if processed {
        if err != nil {
            s.trySendUI(ui.StatusMsg{Message: fmt.Sprintf("[Map] GMCP Error: %v\n", err)})
        } else if loopDetected {
            s.trySendUI(ui.StatusMsg{Message: fmt.Sprintf("[Map] Loop detected! Linked to existing room: %s\n", roomName)})
        } else {
            s.trySendUI(ui.StatusMsg{Message: fmt.Sprintf("[Map] New room created: %s\n", roomName)})
        }
    }
}
```

- [ ] **Step 3: Build**

Run: `go build ./...`
Expected: clean.

- [ ] **Step 4: Commit**

```bash
git add internal/app/app.go
git commit -m "feat(app): emit gmcp room_info protolog event"
```

---

### Task 20: Hook protolog into raw network rx

**Files:**
- Modify: `internal/network/telnet.go`

- [ ] **Step 1: Add a setter and emit a `raw chunk` event in `ReadLoop`**

Add an import for protolog:

```go
"github.com/ayder/gotin/internal/mudproto/protolog"
```

Add a field on `Client`:

```go
    protoLog protolog.Logger
```

Add a setter near `SetDebug`:

```go
// SetProtocolLogger installs a structured logger that receives raw rx chunks
// (truncated at 4 KiB) before any protocol filtering. Nil disables logging.
func (c *Client) SetProtocolLogger(l protolog.Logger) { c.protoLog = l }
```

In `ReadLoop` (around line 453, where `clean` is decoded), insert immediately before `text := c.decoder.Decode(clean)`:

```go
                if c.protoLog != nil && c.protoLog.Enabled() {
                    raw := clean
                    truncated := false
                    const maxLen = 4 * 1024
                    if len(raw) > maxLen {
                        raw = raw[:maxLen]
                        truncated = true
                    }
                    parsed := map[string]any{"len": len(clean)}
                    if truncated {
                        parsed["truncated"] = true
                    }
                    c.protoLog.Log(protolog.Entry{
                        Source: "raw", Dir: "rx", Event: "chunk",
                        UTF8:   protolog.EncodeUTF8(raw),
                        Hex:    protolog.EncodeHex(raw),
                        Parsed: parsed,
                    })
                }
```

- [ ] **Step 2: Build**

Run: `go build ./...`
Expected: clean.

- [ ] **Step 3: Commit**

```bash
git add internal/network/telnet.go
git commit -m "feat(network): protolog raw rx chunks (truncated 4KiB)"
```

---

### Task 21: Wire the logger in `app.Run`

**Files:**
- Modify: `internal/app/app.go`

- [ ] **Step 1: Open `gotin.log` and instantiate a single logger**

In `app.Run`, after step 8 ("Shared logic components", around line 197), before the auto-load:

```go
    // Open the protolog file (separate handle from the network debug log to
    // avoid contention via Go's stdlib log.Logger mutex; both use O_APPEND so
    // line-sized writes are atomic on POSIX up to PIPE_BUF).
    if opts.Debug {
        if f, err := os.OpenFile("gotin.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644); err == nil {
            s.protoLog = protolog.NewJSONLinesLogger(f, func() bool { return s.opts.Debug })
        } else {
            log.Printf("protolog: open gotin.log: %v", err)
        }
    }
```

In `connect()`, after the existing `installProtocols(c, ...)` and before the GMCP/MXP callback wiring blocks, install the logger into protocols:

```go
    if s.protoLog != nil {
        if g := findGMCP(c); g != nil {
            g.SetLogger(s.protoLog)
        }
        if m := findMXP(c); m != nil {
            m.SetLogger(s.protoLog)
        }
        c.SetProtocolLogger(s.protoLog)
    }
```

(Place this block just after `installProtocols` and before the existing `if g := findGMCP(c); g != nil { g.SetCallback(...) }` block, so callbacks are set after the logger.)

- [ ] **Step 2: Build**

Run: `go build ./...`
Expected: clean.

- [ ] **Step 3: Commit**

```bash
git add internal/app/app.go
git commit -m "feat(app): instantiate and wire protolog logger when -debug"
```

---

### Task 22: Integration test: synthetic chunk through MXP filter writes valid JSON Lines

**Files:**
- Modify: `internal/mudproto/mxp/mxp_test.go` (or new file alongside)

The unit test in Task 17 already covers this. Add a separate end-to-end test that exercises the full chain (chunk + tag + room_name + mode + probe response written to the same buffer) and asserts `gotin.log` would be parseable.

- [ ] **Step 1: Add test**

Append to `internal/mudproto/mxp/mxp_test.go`:

```go
func TestFilter_FullStreamLogIsParseableJSONLines(t *testing.T) {
    var buf bytes.Buffer
    lg := protolog.NewJSONLinesLogger(&buf, func() bool { return true })
    p := New()
    p.SetLogger(lg)
    // Provide a context double so probe responses can fire.
    p.SetContext(noopCtx{})

    in := "<VERSION><ROOMNAME>Plaza</ROOMNAME>\x1b[1z<NOBR>\nDone\n"
    _ = p.Filter(in)

    rdr := bufio.NewScanner(&buf)
    var n int
    for rdr.Scan() {
        var e protolog.Entry
        if err := json.Unmarshal(rdr.Bytes(), &e); err != nil {
            t.Errorf("line %d not valid JSON: %v (%q)", n, err, rdr.Text())
        }
        n++
    }
    if n == 0 {
        t.Fatalf("no lines written")
    }
}
```

If `noopCtx` doesn't already exist in the test file, add a minimal one near the top of the file:

```go
type noopCtx struct{}

func (noopCtx) Send([]byte) error                                  { return nil }
func (noopCtx) SendSubneg(byte, []byte) error                      { return nil }
func (noopCtx) Debug(string, ...any)                               {}
func (noopCtx) NotifyProtocolStatus()                              {}
func (noopCtx) SwapReader(func(io.Reader) io.Reader)               {}
```

(If `network.Context` includes more methods than these, match the full interface.)

Add `bufio` and `io` to imports as needed.

- [ ] **Step 2: Run test**

Run: `go test ./internal/mudproto/mxp/ -run TestFilter_FullStreamLog -v`
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add internal/mudproto/mxp/mxp_test.go
git commit -m "test(mxp): full-stream filter writes valid JSON Lines"
```

**CHECKPOINT E complete.** Logging works end-to-end. Pause for review.

---

## Checkpoint F — Build, full test suite, manual verification

### Task 23: Final verification

- [ ] **Step 1: Full build**

Run: `go build ./...`
Expected: clean.

- [ ] **Step 2: Full test suite, race-checked**

Run: `go test ./... -race`
Expected: PASS for every package.

- [ ] **Step 3: Manual verification — defaults**

Run:
```bash
go build -o gotin ./cmd/gotin
./gotin -debug
```

Inside the client:
1. `/map create test_phase1.json`
2. `/map option` — confirm output: `mapping options: vnum=on, hash=off`
3. `/map option hash on`
4. `/map option` — confirm `hash=on`
5. `/map option none`
6. `/map option` — confirm both `off`
7. `/quit`
8. Inspect `gotin.json`: confirm a top-level `"mapping_options": {"vnum":false,"hash":false}` entry exists.

- [ ] **Step 4: Manual verification — t2tmud session**

Run:
```bash
rm -f gotin.log
./gotin -debug -host t2tmud.org -port 9999
```

Inside the client:
1. `/map create t2t_phase1.json`
2. `/map start`
3. Walk at least 10 rooms (mix of new and revisited), trigger at least one weather/time tick if possible.
4. `/map stop` then `/quit`.

Verify:
- `gotin.log` exists and contains JSON Lines: `head -5 gotin.log | jq .` should show structured records.
- Records exist for `source: "mxp"` and `source: "gmcp"`. Confirm at least one `event: "room_name"` (MXP) or `event: "room_info"` (GMCP).
- The map JSON `t2t_phase1.json` has 10 distinct room IDs (no false collapses).

- [ ] **Step 5: Hand the log to the user for Phase 2 inspection**

Save a copy of `gotin.log` and `t2t_phase1.json` for the Phase 2 inspection checklist (`docs/superpowers/notes/2026-04-26-mapper-resilience-phase2-reminder.md`).

- [ ] **Step 6: Commit any final adjustments**

If manual testing surfaced minor fixes, commit them now.

```bash
git status
git add -A   # only stage what's actually mapper-resilience-related
git commit -m "fix(mapper-resilience): manual-test adjustments"
```

- [ ] **Step 7: Push the branch and open a PR**

```bash
git push -u origin mapper-resilience
gh pr create --title "Mapper Phase 1: per-MUD strategy switch + protolog" \
    --body "$(cat docs/superpowers/specs/2026-04-26-mapper-resilience-phase1-design.md | head -60)"
```

(Adjust the body command if the user prefers a different summary.)

**CHECKPOINT F complete. Phase 1 ready for review and merge.**

---

## Self-Review

**Spec coverage check:**
- ✅ `MappingOptions` type + defaults — Task 1
- ✅ Engine getter/setter — Task 2
- ✅ `HandleGMCPRoomInfo` gating — Task 3
- ✅ `ProcessRoomData` gating — Task 4
- ✅ `createRoom` hash-index gating — Task 5
- ✅ Existing tests adjusted for new defaults — Task 6
- ✅ `MapOption` command — Task 7
- ✅ `/map option` parser — Task 8
- ✅ App dispatch — Task 9
- ✅ `pendingName` latch + `SetIncomingRoomName` — Task 10
- ✅ MXP callback wiring — Task 11
- ✅ GMCP empty-name fallback — Task 12
- ✅ `gotin.json` schema (per-conn + top-level) — Task 13
- ✅ Resolve options on connect, persist on change — Task 14
- ✅ Round-trip persistence test — Task 15
- ✅ `protolog` package + tests — Task 16
- ✅ MXP hooks (chunk/tag/room_name/mode/probe) — Task 17
- ✅ GMCP chunk hook — Task 18
- ✅ App-side GMCP `room_info` event — Task 19
- ✅ Network raw-rx hook (4 KiB truncation) — Task 20
- ✅ Logger wiring in `app.Run` — Task 21
- ✅ Full-stream JSON Lines test — Task 22
- ✅ Acceptance criteria 1–6 — Task 23

**Type consistency check:**
- `MappingOptions{Vnum, Hash bool}` used identically in mapper, command, input, app, gotin.json — consistent.
- `*command.MapOption` with `{Print, Strategy, Enable}` referenced from input parser and app dispatch — consistent.
- `protolog.Logger` interface (`Enabled()`, `Log(Entry)`) used uniformly across mxp, gmcp, network, app — consistent.
- `protolog.Entry` field names (TS, Source, Dir, Event, UTF8, Hex, Parsed) match between Task 16 and all hook tasks — consistent.

**Placeholder scan:** No "TBD" / "TODO" / "implement later" / vague guidance. Every step shows code or commands.

**Out-of-scope guard:** No task touches description hashing formula, dynamic-line filtering, or per-MUD profiles. All deferred to Phase 2.

---

## Execution Handoff

**Plan complete and saved to `docs/superpowers/plans/2026-04-26-mapper-resilience-phase1.md`. Two execution options:**

**1. Subagent-Driven (recommended)** — Dispatch a fresh subagent per task, review between tasks, fast iteration.

**2. Inline Execution** — Execute tasks in this session using executing-plans, batch execution with checkpoints.

**Which approach?**
