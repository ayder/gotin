# /map show — Side-by-Side Map Pane Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a live, terminal-native map pane rendered to the right of the MUD output, toggled by `/map show`, that draws the current layer of the mapper graph using a `(2 cols × 2 rows)` cell pitch, switches layers automatically when the player walks `up`/`down`/`in`/`out`, and renders cardinal/diagonal exits only when the data edge matches the coordinate offset.

**Architecture:** A new pure-renderer package `internal/ui/mappane` consumes a `*mapper.Map` snapshot plus a current-room ID, computes the layer connected component using only N/S/E/W/NE/NW/SE/SW exits, and emits a styled string sized exactly `paneWidth × paneHeight`. The `mapper.Engine` exposes `Snapshot()` to deliver a deep-copied map under `e.mu.RLock`. `internal/ui/model.go` holds the toggle state, splits the chat viewport via `lipgloss.JoinHorizontal`, and wires keyboard pan/recenter/layer-step keys gated on an empty input line. The renamed `/map mermaid` flow is untouched.

**Tech Stack:** Go 1.21+, Bubble Tea, lipgloss, `bubbles/viewport`, existing `internal/mapper`, existing `internal/command` and `internal/input` plumbing.

---

## File Structure

| Path | Status | Responsibility |
| --- | --- | --- |
| `internal/mapper/mapper.go` | modify | Add `Engine.Snapshot()` |
| `internal/mapper/snapshot_test.go` | create | Tests for `Snapshot()` |
| `internal/ui/mappane/types.go` | create | `View`, `Point` value types |
| `internal/ui/mappane/layer.go` | create | `LayerOf`, `AllLayers`, layer-label helper |
| `internal/ui/mappane/layer_test.go` | create | Layer logic tests |
| `internal/ui/mappane/render.go` | create | `Render` (header, body, footer composition) |
| `internal/ui/mappane/render_grid.go` | create | Grid construction, glyph placement, link drawing |
| `internal/ui/mappane/render_test.go` | create | Golden-string render tests |
| `internal/command/command.go` | modify | Add `MapShow` type and `isCommand` impl |
| `internal/input/handler.go` | modify | Add `case "show"` subcommand to `cmdMap` |
| `internal/input/handler.go` (help line / summary) | modify | Update `/map` summary and help line |
| `internal/ui/messages.go` | create | `MapPaneToggleMsg`, `MapPaneRecenterMsg` (avoid bloating `model.go`) |
| `internal/ui/model.go` | modify | Visibility state, snapshot accessor, split layout, key routing |
| `internal/ui/model_test.go` | create | Toggle, resize, key-routing integration tests |
| `internal/app/app.go` | modify | Wire `MapShow` command → `MapPaneToggleMsg`; inject snapshot accessor; auto-recenter |

---

## Conventions

- All commits skip nothing: `go build ./...` and `go test ./...` must pass before each commit.
- Place all test fixtures inline with `t.Helper()` builders unless reused; reused helpers go in `mappane/testhelp_test.go`.
- Render tests use raw multi-line string literals with `\n` line separators for golden compares; test failures should diff with `cmp.Diff` from `github.com/google/go-cmp/cmp` if it is already in the module, otherwise plain `t.Errorf("got:\n%q\nwant:\n%q", got, want)`.

Verify go-cmp is available now:

```bash
grep -F "go-cmp" /Users/sinanalyuruk/Vscode/gotin/go.mod
```

If absent, every render test uses the plain `t.Errorf` form shown in tasks.

---

## Checkpoint A — Foundation (Snapshot + Layer logic)

### Task 1: `Engine.Snapshot()`

**Files:**
- Modify: `internal/mapper/mapper.go` (insert immediately after `GetMUDProfile`, around line 215)
- Create: `internal/mapper/snapshot_test.go`

- [ ] **Step 1: Write the failing test**

```go
package mapper

import "testing"

func TestSnapshot_DeepCopyIndependence(t *testing.T) {
	e := NewEngine("")
	if err := e.Create(""); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := e.Dig(East, "Town Square"); err != nil {
		t.Fatalf("Dig: %v", err)
	}

	snap, currID := e.Snapshot()
	if snap == nil {
		t.Fatal("Snapshot returned nil map")
	}
	if currID == "" {
		t.Fatal("Snapshot returned empty current ID")
	}
	if _, ok := snap.Rooms[currID]; !ok {
		t.Fatalf("current ID %q not in snapshot rooms", currID)
	}

	// Mutate the snapshot — engine state must be unchanged.
	snap.Rooms[currID].Name = "MUTATED"
	delete(snap.Rooms, currID)

	live, _ := e.Snapshot()
	if live.Rooms[currID].Name == "MUTATED" {
		t.Fatal("engine state was mutated through snapshot")
	}
	if _, ok := live.Rooms[currID]; !ok {
		t.Fatal("engine room was deleted through snapshot")
	}
}

func TestSnapshot_NoMap(t *testing.T) {
	e := NewEngine("")
	// Engine has data but no Create has been called → CurrentRoom == "".
	snap, currID := e.Snapshot()
	if snap == nil {
		t.Fatal("Snapshot must not return nil when an empty map exists")
	}
	if currID != "" {
		t.Fatalf("expected empty currID before Create, got %q", currID)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/mapper/ -run TestSnapshot -count=1
```

Expected: FAIL — `e.Snapshot undefined`.

- [ ] **Step 3: Implement `Snapshot`**

Insert into `internal/mapper/mapper.go` directly after `func (e *Engine) GetMUDProfile()`:

```go
// Snapshot returns a deep-copied *Map plus the current room ID. Safe for
// rendering off-engine without holding e.mu. Returns a non-nil empty Map
// when the engine has not yet been Created; CurrentRoom may be "".
func (e *Engine) Snapshot() (*Map, string) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	if e.data == nil {
		return &Map{Rooms: make(map[string]*Room)}, ""
	}
	return e.data.deepCopy(), e.data.CurrentRoom
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
go test ./internal/mapper/ -run TestSnapshot -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/mapper/mapper.go internal/mapper/snapshot_test.go
git commit -m "feat(mapper): expose Snapshot for off-engine rendering"
```

---

### Task 2: `mappane` types

**Files:**
- Create: `internal/ui/mappane/types.go`

- [ ] **Step 1: Write the file**

```go
// Package mappane is a pure renderer that turns a *mapper.Map snapshot into
// a styled, fixed-size string suitable for placement in a Bubble Tea TUI.
// The package never holds the mapper engine lock and never mutates the map.
package mappane

import "github.com/ayder/gotin/internal/mapper"

// Point is a (col, row) pair in screen-cell units, not coordinate units.
type Point struct{ Col, Row int }

// View is the complete input contract for Render. Same View → same output.
type View struct {
	PaneWidth  int          // total cols including borders/padding
	PaneHeight int          // total rows
	Map        *mapper.Map  // map snapshot; may be nil
	CurrentID  string       // current room ID; "" or missing → fallback render
	PanOffset  Point        // pan in screen cells from current-room-centred default
	LayerKey   string       // representative room ID of layer to render; "" = layer of CurrentID
}
```

- [ ] **Step 2: Run build to verify the file compiles**

```bash
go build ./internal/ui/mappane/...
```

Expected: PASS (no functions yet, just types).

- [ ] **Step 3: Commit**

```bash
git add internal/ui/mappane/types.go
git commit -m "feat(mappane): introduce View and Point types"
```

---

### Task 3: `LayerOf` cardinal-only traversal

**Files:**
- Create: `internal/ui/mappane/layer.go`
- Create: `internal/ui/mappane/layer_test.go`

- [ ] **Step 1: Write the failing test**

```go
package mappane

import (
	"testing"

	"github.com/ayder/gotin/internal/mapper"
)

// makeRoom constructs a fully-formed *Room with no exits.
func makeRoom(id string, x, y, z int) *mapper.Room {
	return &mapper.Room{
		ID:    id,
		Name:  id,
		X:     x, Y: y, Z: z,
		Exits: map[mapper.Direction]string{},
	}
}

func TestLayerOf_DoesNotTraverseUpDownInOut(t *testing.T) {
	a := makeRoom("a", 0, 0, 0)
	b := makeRoom("b", 1, 0, 0)
	c := makeRoom("c", 0, 0, 1) // up from a
	d := makeRoom("d", 0, 0, 0) // unreachable second component

	a.Exits[mapper.East] = "b"
	b.Exits[mapper.West] = "a"
	a.Exits[mapper.Up] = "c"
	c.Exits[mapper.Down] = "a"

	m := &mapper.Map{
		CurrentRoom: "a",
		Rooms: map[string]*mapper.Room{
			"a": a, "b": b, "c": c, "d": d,
		},
	}

	got := LayerOf(m, "a")
	want := map[string]struct{}{"a": {}, "b": {}}
	if len(got) != len(want) {
		t.Fatalf("LayerOf size = %d, want %d (got %v)", len(got), len(want), got)
	}
	for id := range want {
		if _, ok := got[id]; !ok {
			t.Errorf("LayerOf missing %q", id)
		}
	}
	if _, ok := got["c"]; ok {
		t.Error("LayerOf must not traverse Up")
	}
	if _, ok := got["d"]; ok {
		t.Error("LayerOf must not include unreachable rooms")
	}
}

func TestLayerOf_TraversesAllEightCardinalsAndDiagonals(t *testing.T) {
	dirs := []mapper.Direction{
		mapper.North, mapper.South, mapper.East, mapper.West,
		mapper.NorthEast, mapper.NorthWest, mapper.SouthEast, mapper.SouthWest,
	}
	rooms := map[string]*mapper.Room{"hub": makeRoom("hub", 0, 0, 0)}
	hub := rooms["hub"]
	for i, d := range dirs {
		id := string(d)
		rooms[id] = makeRoom(id, i, i, 0)
		hub.Exits[d] = id
		rooms[id].Exits[mapper.ReverseDirection(d)] = "hub"
	}

	got := LayerOf(&mapper.Map{Rooms: rooms}, "hub")
	if len(got) != len(rooms) {
		t.Fatalf("LayerOf size = %d, want %d", len(got), len(rooms))
	}
}

func TestLayerOf_NilMapAndUnknownStart(t *testing.T) {
	if got := LayerOf(nil, "x"); len(got) != 0 {
		t.Errorf("nil map: got %v, want empty", got)
	}
	m := &mapper.Map{Rooms: map[string]*mapper.Room{"a": makeRoom("a", 0, 0, 0)}}
	if got := LayerOf(m, "missing"); len(got) != 0 {
		t.Errorf("unknown start: got %v, want empty", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/ui/mappane/ -run TestLayerOf -count=1
```

Expected: FAIL — `LayerOf undefined`.

- [ ] **Step 3: Implement `LayerOf`**

Create `internal/ui/mappane/layer.go`:

```go
package mappane

import "github.com/ayder/gotin/internal/mapper"

// cardinalDirs is the set of exits that stay within a layer.
var cardinalDirs = []mapper.Direction{
	mapper.North, mapper.South, mapper.East, mapper.West,
	mapper.NorthEast, mapper.NorthWest, mapper.SouthEast, mapper.SouthWest,
}

// LayerOf returns the set of room IDs reachable from start using only the
// eight cardinal/diagonal exits. Up/Down/In/Out are NOT traversed. Returns
// an empty (non-nil) map when m is nil or start is missing.
func LayerOf(m *mapper.Map, start string) map[string]struct{} {
	out := map[string]struct{}{}
	if m == nil {
		return out
	}
	if _, ok := m.Rooms[start]; !ok {
		return out
	}
	queue := []string{start}
	out[start] = struct{}{}
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		r, ok := m.Rooms[id]
		if !ok {
			continue
		}
		for _, d := range cardinalDirs {
			next, has := r.Exits[d]
			if !has {
				continue
			}
			if _, seen := out[next]; seen {
				continue
			}
			if _, exists := m.Rooms[next]; !exists {
				continue
			}
			out[next] = struct{}{}
			queue = append(queue, next)
		}
	}
	return out
}
```

- [ ] **Step 4: Run tests**

```bash
go test ./internal/ui/mappane/ -run TestLayerOf -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/ui/mappane/layer.go internal/ui/mappane/layer_test.go
git commit -m "feat(mappane): LayerOf cardinal-only BFS"
```

---

### Task 4: `AllLayers` deterministic ordering

**Files:**
- Modify: `internal/ui/mappane/layer.go`
- Modify: `internal/ui/mappane/layer_test.go`

- [ ] **Step 1: Add the failing test**

Append to `internal/ui/mappane/layer_test.go`:

```go
func TestAllLayers_DeterministicAndOnePerComponent(t *testing.T) {
	a := makeRoom("a", 0, 0, 0)
	b := makeRoom("b", 1, 0, 0)
	c := makeRoom("c", 0, 0, 1) // separate Z layer
	d := makeRoom("d", 5, 5, 0) // separate component
	a.Exits[mapper.East] = "b"
	b.Exits[mapper.West] = "a"

	m := &mapper.Map{Rooms: map[string]*mapper.Room{"a": a, "b": b, "c": c, "d": d}}

	got1 := AllLayers(m)
	got2 := AllLayers(m)
	if len(got1) != 3 {
		t.Fatalf("AllLayers len = %d, want 3 (a/b component, c, d), got %v", len(got1), got1)
	}
	for i := range got1 {
		if got1[i] != got2[i] {
			t.Errorf("AllLayers not deterministic: %v vs %v", got1, got2)
		}
	}
	// First-room-by-id of each component is the rep; sorted ascending.
	want := []string{"a", "c", "d"}
	for i, w := range want {
		if got1[i] != w {
			t.Errorf("AllLayers[%d] = %q, want %q", i, got1[i], w)
		}
	}
}

func TestAllLayers_NilAndEmpty(t *testing.T) {
	if got := AllLayers(nil); len(got) != 0 {
		t.Errorf("nil: got %v", got)
	}
	if got := AllLayers(&mapper.Map{Rooms: map[string]*mapper.Room{}}); len(got) != 0 {
		t.Errorf("empty: got %v", got)
	}
}
```

- [ ] **Step 2: Verify it fails**

```bash
go test ./internal/ui/mappane/ -run TestAllLayers -count=1
```

Expected: FAIL — `AllLayers undefined`.

- [ ] **Step 3: Implement `AllLayers`**

Append to `internal/ui/mappane/layer.go`:

```go
import "sort"

// AllLayers returns one representative room ID per distinct connected
// component (under cardinal+diagonal exits). The representative is the
// alphabetically smallest room ID in the component, and the returned slice
// is sorted ascending so output is deterministic across calls. Used by
// '[' / ']' navigation in the UI.
func AllLayers(m *mapper.Map) []string {
	if m == nil || len(m.Rooms) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	var reps []string
	// Iterate rooms in sorted order so each new component's smallest ID is
	// the one we discover first.
	ids := make([]string, 0, len(m.Rooms))
	for id := range m.Rooms {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		if _, done := seen[id]; done {
			continue
		}
		layer := LayerOf(m, id)
		if len(layer) == 0 {
			continue
		}
		// Find the smallest ID in this layer; that is the rep.
		rep := id
		for lid := range layer {
			if lid < rep {
				rep = lid
			}
			seen[lid] = struct{}{}
		}
		reps = append(reps, rep)
	}
	sort.Strings(reps)
	return reps
}
```

Note the `sort` import: if `layer.go` does not yet have an `import` block with `sort`, merge them into a single block:

```go
import (
	"sort"

	"github.com/ayder/gotin/internal/mapper"
)
```

- [ ] **Step 4: Run tests**

```bash
go test ./internal/ui/mappane/ -count=1
```

Expected: PASS for both `TestLayerOf*` and `TestAllLayers*`.

- [ ] **Step 5: Commit**

```bash
git add internal/ui/mappane/layer.go internal/ui/mappane/layer_test.go
git commit -m "feat(mappane): AllLayers deterministic component listing"
```

---

### Task 5: Layer label heuristic

**Files:**
- Modify: `internal/ui/mappane/layer.go`
- Modify: `internal/ui/mappane/layer_test.go`

- [ ] **Step 1: Add failing tests**

Append to `internal/ui/mappane/layer_test.go`:

```go
func TestLayerLabel_ModeOfNamesWinsTiesByLex(t *testing.T) {
	// Layer with three rooms named "Bree", two "Bree", one "Inn" -> "Bree".
	rooms := map[string]*mapper.Room{
		"a": makeRoom("a", 0, 0, 0), "b": makeRoom("b", 1, 0, 0), "c": makeRoom("c", 2, 0, 0),
	}
	rooms["a"].Name = "Bree"
	rooms["b"].Name = "Bree"
	rooms["c"].Name = "Inn"
	rooms["a"].Exits = map[mapper.Direction]string{mapper.East: "b"}
	rooms["b"].Exits = map[mapper.Direction]string{mapper.West: "a", mapper.East: "c"}
	rooms["c"].Exits = map[mapper.Direction]string{mapper.West: "b"}
	m := &mapper.Map{Rooms: rooms}

	got := LayerLabel(m, LayerOf(m, "a"), "a")
	if got != "Bree" {
		t.Errorf("LayerLabel = %q, want %q", got, "Bree")
	}
}

func TestLayerLabel_IgnoresPlaceholderNames(t *testing.T) {
	rooms := map[string]*mapper.Room{
		"a": makeRoom("a", 0, 0, 0), "b": makeRoom("b", 1, 0, 0),
	}
	rooms["a"].Name = "New Room"
	rooms["b"].Name = ""
	rooms["a"].Exits = map[mapper.Direction]string{mapper.East: "b"}
	rooms["b"].Exits = map[mapper.Direction]string{mapper.West: "a"}
	m := &mapper.Map{Rooms: rooms}
	if got := LayerLabel(m, LayerOf(m, "a"), "a"); got != "Unnamed Layer" {
		t.Errorf("LayerLabel = %q, want %q", got, "Unnamed Layer")
	}
}

func TestLayerLabel_ZAnnotation(t *testing.T) {
	rooms := map[string]*mapper.Room{
		"a": makeRoom("a", 0, 0, 1), "b": makeRoom("b", 1, 0, 1),
	}
	rooms["a"].Name, rooms["b"].Name = "Floor", "Floor"
	rooms["a"].Exits = map[mapper.Direction]string{mapper.East: "b"}
	rooms["b"].Exits = map[mapper.Direction]string{mapper.West: "a"}
	m := &mapper.Map{Rooms: rooms}

	got := LayerLabel(m, LayerOf(m, "a"), "a")
	if got != "Floor [Z=1]" {
		t.Errorf("LayerLabel = %q, want %q", got, "Floor [Z=1]")
	}
}
```

- [ ] **Step 2: Verify failure**

```bash
go test ./internal/ui/mappane/ -run TestLayerLabel -count=1
```

Expected: FAIL — `LayerLabel undefined`.

- [ ] **Step 3: Implement `LayerLabel`**

Append to `internal/ui/mappane/layer.go`:

```go
import "fmt"

// placeholderName returns true for names that should not vote in the
// layer-label heuristic.
func placeholderName(s string) bool {
	switch s {
	case "", "New Room", "Start":
		return true
	}
	return false
}

// LayerLabel produces the human-friendly heading rendered above the map
// pane body. layer must be the result of LayerOf(m, currentID); currentID
// is used as the final fallback when no real names are available.
func LayerLabel(m *mapper.Map, layer map[string]struct{}, currentID string) string {
	if m == nil || len(layer) == 0 {
		return "Unnamed Layer"
	}
	counts := map[string]int{}
	for id := range layer {
		r, ok := m.Rooms[id]
		if !ok {
			continue
		}
		if placeholderName(r.Name) {
			continue
		}
		counts[r.Name]++
	}

	primary := ""
	if len(counts) > 0 {
		// Mode wins; ties broken alphabetically.
		var names []string
		for n := range counts {
			names = append(names, n)
		}
		sort.Strings(names)
		bestCount := -1
		for _, n := range names {
			if counts[n] > bestCount {
				bestCount = counts[n]
				primary = n
			}
		}
	}
	if primary == "" {
		if cur, ok := m.Rooms[currentID]; ok && !placeholderName(cur.Name) {
			primary = cur.Name
		}
	}
	if primary == "" {
		primary = "Unnamed Layer"
	}

	// Z annotation.
	zSet := map[int]struct{}{}
	for id := range layer {
		if r, ok := m.Rooms[id]; ok {
			zSet[r.Z] = struct{}{}
		}
	}
	if len(zSet) > 1 {
		return primary + " [Z=mixed]"
	}
	for z := range zSet {
		if z != 0 {
			return fmt.Sprintf("%s [Z=%d]", primary, z)
		}
	}
	return primary
}
```

Update the `import` block at the top of `layer.go` so it now reads:

```go
import (
	"fmt"
	"sort"

	"github.com/ayder/gotin/internal/mapper"
)
```

- [ ] **Step 4: Run tests**

```bash
go test ./internal/ui/mappane/ -count=1
```

Expected: PASS — all layer-related tests green.

- [ ] **Step 5: Commit**

```bash
git add internal/ui/mappane/layer.go internal/ui/mappane/layer_test.go
git commit -m "feat(mappane): LayerLabel mode-of-names heuristic with Z suffix"
```

---

## Checkpoint A complete

Run the full test suite once before moving on:

```bash
go test ./... -count=1
```

Expected: previous 340 tests plus new `mappane` and `Snapshot` tests, all green.

---

## Checkpoint B — Render: empty / single / cardinal

Renderer surface lives across two files for clarity:
- `render.go` — public `Render` entry, header/footer composition, fallbacks.
- `render_grid.go` — internal grid construction and glyph placement.

### Task 6: `Render` empty/invalid fallbacks

**Files:**
- Create: `internal/ui/mappane/render.go`
- Create: `internal/ui/mappane/render_grid.go`
- Create: `internal/ui/mappane/render_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/ui/mappane/render_test.go`:

```go
package mappane

import (
	"strings"
	"testing"

	"github.com/ayder/gotin/internal/mapper"
)

// padLine right-pads s with spaces so it matches the requested width. Render
// outputs are fixed-size, so test goldens must match exactly.
func padLine(s string, w int) string {
	if n := w - lipglossWidthFallback(s); n > 0 {
		return s + strings.Repeat(" ", n)
	}
	return s
}

// lipglossWidthFallback measures rune-width for ASCII content. We use it
// because the renderer outputs include Unicode glyphs (■▣╱╲↑) that lipgloss
// counts as 1 cell. Tests can assume single-cell width per rune for the
// glyphs we emit.
func lipglossWidthFallback(s string) int { return len([]rune(s)) }

func joinLines(lines []string) string { return strings.Join(lines, "\n") }

func TestRender_NilMap(t *testing.T) {
	got := Render(View{PaneWidth: 30, PaneHeight: 6})
	want := joinLines([]string{
		padLine("Unnamed Layer", 30),
		padLine("", 30),
		padLine("(no map — try /map create)", 30),
		padLine("", 30),
		padLine("", 30),
		padLine("(no layer bridges)", 30),
	})
	if got != want {
		t.Errorf("nil map render mismatch\nGOT:\n%s\nWANT:\n%s", got, want)
	}
}

func TestRender_MissingCurrent(t *testing.T) {
	m := &mapper.Map{Rooms: map[string]*mapper.Room{}}
	got := Render(View{PaneWidth: 30, PaneHeight: 6, Map: m, CurrentID: "ghost"})
	want := joinLines([]string{
		padLine("Unnamed Layer", 30),
		padLine("", 30),
		padLine("(current room missing from map)", 30),
		padLine("", 30),
		padLine("", 30),
		padLine("(no layer bridges)", 30),
	})
	if got != want {
		t.Errorf("missing current render mismatch\nGOT:\n%s\nWANT:\n%s", got, want)
	}
}

func TestRender_PaneTooShort(t *testing.T) {
	got := Render(View{PaneWidth: 10, PaneHeight: 3})
	want := padLine("pane too short", 10)
	if got != want {
		t.Errorf("pane too short mismatch\nGOT:%q\nWANT:%q", got, want)
	}
}
```

- [ ] **Step 2: Verify failure**

```bash
go test ./internal/ui/mappane/ -run TestRender -count=1
```

Expected: FAIL — `Render undefined`.

- [ ] **Step 3: Implement minimum `Render`**

Create `internal/ui/mappane/render.go`:

```go
package mappane

import "strings"

// Render returns a styled string sized exactly View.PaneWidth × View.PaneHeight.
// Pure: same input → same output. No goroutines, no I/O.
func Render(v View) string {
	if v.PaneHeight < 5 {
		return padRight("pane too short", v.PaneWidth)
	}

	header := layerHeaderLines(v)
	footer := footerLine(v)
	bodyRows := v.PaneHeight - len(header) - 1 // 1 row for footer
	body := bodyLines(v, bodyRows)

	all := make([]string, 0, v.PaneHeight)
	all = append(all, header...)
	all = append(all, body...)
	all = append(all, footer)
	return strings.Join(all, "\n")
}

// padRight right-pads s with spaces to width w. Truncates if longer.
func padRight(s string, w int) string {
	if w <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) >= w {
		return string(r[:w])
	}
	return s + strings.Repeat(" ", w-len(r))
}
```

Create `internal/ui/mappane/render_grid.go`:

```go
package mappane

// layerHeaderLines returns the two header rows: layer label + current room
// name. Both are padded to PaneWidth.
func layerHeaderLines(v View) []string {
	label := "Unnamed Layer"
	currentName := ""
	if v.Map != nil && v.CurrentID != "" {
		layer := LayerOf(v.Map, v.CurrentID)
		if len(layer) > 0 {
			label = LayerLabel(v.Map, layer, v.CurrentID)
		}
		if r, ok := v.Map.Rooms[v.CurrentID]; ok {
			currentName = r.Name
		}
	}
	return []string{
		padRight(label, v.PaneWidth),
		padRight(currentName, v.PaneWidth),
	}
}

// footerLine returns the bridge / dig hint footer (one line).
func footerLine(v View) string {
	// Concrete logic added in Task 13. For now, "(no layer bridges)" is the
	// safe default that satisfies fallback tests.
	return padRight("(no layer bridges)", v.PaneWidth)
}

// bodyLines returns rows rows of body content, padded to v.PaneWidth. The
// first cut renders fallbacks only; cardinal/diagonal drawing is layered in
// in subsequent tasks.
func bodyLines(v View, rows int) []string {
	out := make([]string, rows)
	for i := range out {
		out[i] = padRight("", v.PaneWidth)
	}
	if rows == 0 {
		return out
	}
	switch {
	case v.Map == nil:
		out[0] = padRight("(no map — try /map create)", v.PaneWidth)
	case v.CurrentID == "":
		out[0] = padRight("(current room missing from map)", v.PaneWidth)
	case v.Map.Rooms[v.CurrentID] == nil:
		out[0] = padRight("(current room missing from map)", v.PaneWidth)
	}
	return out
}
```

- [ ] **Step 4: Run tests**

```bash
go test ./internal/ui/mappane/ -run TestRender -count=1
```

Expected: PASS for the three fallback tests.

- [ ] **Step 5: Commit**

```bash
git add internal/ui/mappane/render.go internal/ui/mappane/render_grid.go internal/ui/mappane/render_test.go
git commit -m "feat(mappane): Render with empty/invalid/short-pane fallbacks"
```

---

### Task 7: Single-room layer renders centered current-room glyph

**Files:**
- Modify: `internal/ui/mappane/render_grid.go`
- Modify: `internal/ui/mappane/render_test.go`

- [ ] **Step 1: Add failing test**

Append to `internal/ui/mappane/render_test.go`:

```go
func TestRender_SingleRoomCentered(t *testing.T) {
	a := makeRoom("a", 0, 0, 0)
	a.Name = "Town Square"
	m := &mapper.Map{
		CurrentRoom: "a",
		Rooms:       map[string]*mapper.Room{"a": a},
	}
	w, h := 12, 7
	got := Render(View{PaneWidth: w, PaneHeight: h, Map: m, CurrentID: "a"})
	lines := strings.Split(got, "\n")
	if len(lines) != h {
		t.Fatalf("expected %d lines, got %d", h, len(lines))
	}
	if lines[0] != padLine("Town Square", w) {
		t.Errorf("header[0] = %q", lines[0])
	}
	if lines[1] != padLine("Town Square", w) {
		t.Errorf("header[1] = %q", lines[1])
	}
	// Body rows are h - 3 = 4. Center body row = (4-1)/2 snapped to even = 2.
	bodyTop := 2
	wantCenterRow := bodyTop + 2 // bodyRows/2 = 2 → header offset 2 → row 4
	if !strings.Contains(lines[wantCenterRow], "▣") {
		t.Errorf("expected current-room glyph ▣ on line %d, got %q", wantCenterRow, lines[wantCenterRow])
	}
}
```

- [ ] **Step 2: Verify failure**

```bash
go test ./internal/ui/mappane/ -run TestRender_SingleRoomCentered -count=1
```

Expected: FAIL — body has no `▣`.

- [ ] **Step 3: Implement body grid for single room**

Replace `bodyLines` in `internal/ui/mappane/render_grid.go` with:

```go
const (
	glyphRoom        = '■'
	glyphCurrentRoom = '▣'
	glyphPlaceholder = '□'
	glyphHLink       = '─'
	glyphVLink       = '│'
	glyphDiagNESW    = '╱'
	glyphDiagNWSE    = '╲'
)

// bodyLines builds the map drawing area as a slice of row strings.
func bodyLines(v View, rows int) []string {
	if rows <= 0 {
		return []string{}
	}
	cols := v.PaneWidth
	grid := newGrid(cols, rows)

	// Fallback strings keep the previous behaviour for invalid input.
	if v.Map == nil {
		grid.setLine(0, "(no map — try /map create)")
		return grid.toLines()
	}
	current, ok := v.Map.Rooms[v.CurrentID]
	if !ok {
		grid.setLine(0, "(current room missing from map)")
		return grid.toLines()
	}

	// Determine layer membership.
	layer := LayerOf(v.Map, v.CurrentID)
	if len(layer) == 0 {
		layer = map[string]struct{}{v.CurrentID: {}}
	}

	// Anchor: render coordinates relative to the current room.
	cx, cy := current.X, current.Y
	centerCol := (cols / 2) &^ 1 // snap to even
	centerRow := (rows / 2) &^ 1

	// Place the rooms.
	for id := range layer {
		r := v.Map.Rooms[id]
		if r == nil {
			continue
		}
		col := centerCol + 2*(r.X-cx) - v.PanOffset.Col
		row := centerRow + 2*(cy-r.Y) - v.PanOffset.Row // y inverted
		if col < 0 || col >= cols || row < 0 || row >= rows {
			continue
		}
		switch {
		case id == v.CurrentID:
			grid.set(col, row, glyphCurrentRoom)
		case placeholderName(r.Name):
			grid.set(col, row, glyphPlaceholder)
		default:
			grid.set(col, row, glyphRoom)
		}
	}

	return grid.toLines()
}

// grid is a 2-D rune buffer that fills with spaces and renders to lines.
type grid struct {
	cells [][]rune
	cols  int
	rows  int
}

func newGrid(cols, rows int) *grid {
	g := &grid{cols: cols, rows: rows, cells: make([][]rune, rows)}
	for r := range g.cells {
		g.cells[r] = make([]rune, cols)
		for c := range g.cells[r] {
			g.cells[r][c] = ' '
		}
	}
	return g
}

func (g *grid) set(col, row int, r rune) {
	if col < 0 || col >= g.cols || row < 0 || row >= g.rows {
		return
	}
	g.cells[row][col] = r
}

func (g *grid) get(col, row int) rune {
	if col < 0 || col >= g.cols || row < 0 || row >= g.rows {
		return 0
	}
	return g.cells[row][col]
}

func (g *grid) setLine(row int, s string) {
	if row < 0 || row >= g.rows {
		return
	}
	r := []rune(s)
	for i := 0; i < g.cols; i++ {
		if i < len(r) {
			g.cells[row][i] = r[i]
		} else {
			g.cells[row][i] = ' '
		}
	}
}

func (g *grid) toLines() []string {
	out := make([]string, g.rows)
	for r := range out {
		out[r] = string(g.cells[r])
	}
	return out
}
```

- [ ] **Step 4: Run tests**

```bash
go test ./internal/ui/mappane/ -count=1
```

Expected: PASS — all render tests including the new single-room case.

- [ ] **Step 5: Commit**

```bash
git add internal/ui/mappane/render_grid.go internal/ui/mappane/render_test.go
git commit -m "feat(mappane): center current room and place layer rooms on grid"
```

---

### Task 8: Cardinal links E/W/N/S with connectivity rule

**Files:**
- Modify: `internal/ui/mappane/render_grid.go`
- Modify: `internal/ui/mappane/render_test.go`

- [ ] **Step 1: Add failing test**

Append to `internal/ui/mappane/render_test.go`:

```go
func TestRender_TwoRoomsLinkedEast(t *testing.T) {
	a := makeRoom("a", 0, 0, 0)
	b := makeRoom("b", 1, 0, 0)
	a.Name, b.Name = "A", "B"
	a.Exits[mapper.East] = "b"
	b.Exits[mapper.West] = "a"
	m := &mapper.Map{
		CurrentRoom: "a",
		Rooms:       map[string]*mapper.Room{"a": a, "b": b},
	}
	got := Render(View{PaneWidth: 14, PaneHeight: 7, Map: m, CurrentID: "a"})
	if !strings.Contains(got, "▣─■") {
		t.Errorf("expected ▣─■ in body, got:\n%s", got)
	}
}

func TestRender_TwoRoomsLinkedNorth(t *testing.T) {
	a := makeRoom("a", 0, 0, 0)
	b := makeRoom("b", 0, 1, 0)
	a.Exits[mapper.North] = "b"
	b.Exits[mapper.South] = "a"
	m := &mapper.Map{
		CurrentRoom: "a",
		Rooms:       map[string]*mapper.Room{"a": a, "b": b},
	}
	got := Render(View{PaneWidth: 14, PaneHeight: 9, Map: m, CurrentID: "a"})
	// Vertical link is in the row above the current room glyph.
	lines := strings.Split(got, "\n")
	bodyStart := 2
	// Find current room row to make assertion robust to centering math.
	var curRow int
	for i := bodyStart; i < len(lines); i++ {
		if strings.ContainsRune(lines[i], glyphCurrentRoom) {
			curRow = i
			break
		}
	}
	if curRow == 0 || curRow-1 < bodyStart {
		t.Fatalf("could not locate current-room row in:\n%s", got)
	}
	if !strings.ContainsRune(lines[curRow-1], glyphVLink) {
		t.Errorf("expected │ on row above current room, got %q", lines[curRow-1])
	}
}
```

- [ ] **Step 2: Verify failure**

```bash
go test ./internal/ui/mappane/ -run "TestRender_TwoRoomsLinked" -count=1
```

Expected: FAIL — links are not yet drawn.

- [ ] **Step 3: Implement cardinal links**

Add a `drawLinks` step to `bodyLines` in `internal/ui/mappane/render_grid.go`. Replace the body-construction tail (everything after the room-placement loop) with:

```go
	// Draw links — one pass per direction. To avoid double-drawing, only
	// emit a link when the source ID is alphabetically smaller than the
	// destination ID. Connectivity rule: the destination must be in the
	// layer AND its X/Y must match the expected offset.
	for id := range layer {
		r := v.Map.Rooms[id]
		if r == nil {
			continue
		}
		for d, otherID := range r.Exits {
			if id >= otherID {
				continue
			}
			other, ok := v.Map.Rooms[otherID]
			if !ok {
				continue
			}
			if _, inLayer := layer[otherID]; !inLayer {
				continue
			}
			dx, dy, ok := cardinalOffset(d)
			if !ok {
				continue
			}
			if other.X != r.X+dx || other.Y != r.Y+dy {
				continue
			}
			drawLink(grid, r.X-cx, cy-r.Y, dx, -dy, centerCol, centerRow, v.PanOffset)
		}
	}

	return grid.toLines()
}

// cardinalOffset returns the X/Y deltas a direction implies, plus an "ok"
// flag for the eight in-layer directions. Up/Down/In/Out return false.
func cardinalOffset(d mapper.Direction) (int, int, bool) {
	switch d {
	case mapper.North:
		return 0, 1, true
	case mapper.South:
		return 0, -1, true
	case mapper.East:
		return 1, 0, true
	case mapper.West:
		return -1, 0, true
	case mapper.NorthEast:
		return 1, 1, true
	case mapper.NorthWest:
		return -1, 1, true
	case mapper.SouthEast:
		return 1, -1, true
	case mapper.SouthWest:
		return -1, -1, true
	}
	return 0, 0, false
}

// drawLink writes the gutter glyph between (relX, relY) and (relX+dx, relY+dy)
// in coord-space. screenDX/screenDY are the pre-flipped screen-space offsets
// (Y inverted). The gutter glyph sits at the half-step.
func drawLink(g *grid, relX, relY, dx, screenDY int, centerCol, centerRow int, pan Point) {
	// Source screen position.
	sCol := centerCol + 2*relX - pan.Col
	sRow := centerRow + 2*relY - pan.Row
	gCol := sCol + dx
	gRow := sRow + screenDY
	switch {
	case dx != 0 && screenDY == 0:
		g.set(gCol, gRow, glyphHLink)
	case dx == 0 && screenDY != 0:
		g.set(gCol, gRow, glyphVLink)
	case dx > 0 && screenDY < 0, dx < 0 && screenDY > 0:
		g.set(gCol, gRow, glyphDiagNESW) // ╱
	case dx > 0 && screenDY > 0, dx < 0 && screenDY < 0:
		g.set(gCol, gRow, glyphDiagNWSE) // ╲
	}
}
```

Also add `mapper` to the import block of `render_grid.go`:

```go
import "github.com/ayder/gotin/internal/mapper"
```

- [ ] **Step 4: Run tests**

```bash
go test ./internal/ui/mappane/ -count=1
```

Expected: PASS for cardinal-link tests; existing single-room test still green.

- [ ] **Step 5: Commit**

```bash
git add internal/ui/mappane/render_grid.go internal/ui/mappane/render_test.go
git commit -m "feat(mappane): cardinal link drawing with strict connectivity check"
```

---

## Checkpoint B complete

```bash
go test ./... -count=1
```

Expected: all green.

---

## Checkpoint C — Diagonals, edge omission, coord overlap

### Task 9: Diagonal link rendering

**Files:**
- Modify: `internal/ui/mappane/render_test.go`

The implementation in Task 8 already covers diagonals via `drawLink`. This task is purely a **golden test** to lock the behaviour in.

- [ ] **Step 1: Add failing-then-passing test**

Append to `internal/ui/mappane/render_test.go`:

```go
func TestRender_NorthEastDiagonalLink(t *testing.T) {
	a := makeRoom("a", 0, 0, 0)
	b := makeRoom("b", 1, 1, 0)
	a.Exits[mapper.NorthEast] = "b"
	b.Exits[mapper.SouthWest] = "a"
	m := &mapper.Map{
		CurrentRoom: "a",
		Rooms:       map[string]*mapper.Room{"a": a, "b": b},
	}
	got := Render(View{PaneWidth: 14, PaneHeight: 9, Map: m, CurrentID: "a"})
	if !strings.ContainsRune(got, glyphDiagNESW) {
		t.Errorf("expected ╱ in render output:\n%s", got)
	}
}

func TestRender_MixedGridAllEightNeighbours(t *testing.T) {
	// 3x3 grid centred on "c" with all eight neighbours.
	rooms := map[string]*mapper.Room{}
	put := func(id string, x, y int) { rooms[id] = makeRoom(id, x, y, 0) }
	put("nw", -1, 1); put("n", 0, 1); put("ne", 1, 1)
	put("w", -1, 0); put("c", 0, 0); put("e", 1, 0)
	put("sw", -1, -1); put("s", 0, -1); put("se", 1, -1)
	c := rooms["c"]
	c.Exits = map[mapper.Direction]string{
		mapper.North: "n", mapper.South: "s", mapper.East: "e", mapper.West: "w",
		mapper.NorthEast: "ne", mapper.NorthWest: "nw",
		mapper.SouthEast: "se", mapper.SouthWest: "sw",
	}
	for id, r := range rooms {
		if id == "c" {
			continue
		}
		// Reverse links so the layer BFS sees them.
		for d, dest := range c.Exits {
			if dest == id {
				r.Exits[mapper.ReverseDirection(d)] = "c"
			}
		}
	}
	m := &mapper.Map{CurrentRoom: "c", Rooms: rooms}
	got := Render(View{PaneWidth: 18, PaneHeight: 11, Map: m, CurrentID: "c"})
	for _, want := range []rune{glyphHLink, glyphVLink, glyphDiagNESW, glyphDiagNWSE, glyphCurrentRoom} {
		if !strings.ContainsRune(got, want) {
			t.Errorf("missing rune %q in render:\n%s", want, got)
		}
	}
	if strings.Count(got, string(glyphCurrentRoom)) != 1 {
		t.Error("expected exactly one current-room glyph")
	}
	if strings.Count(got, string(glyphRoom)) != 8 {
		t.Errorf("expected 8 normal rooms, got %d:\n%s", strings.Count(got, string(glyphRoom)), got)
	}
}
```

- [ ] **Step 2: Run**

```bash
go test ./internal/ui/mappane/ -run "TestRender_NorthEastDiagonalLink|TestRender_MixedGridAllEightNeighbours" -count=1
```

Expected: PASS (Task 8 already implemented diagonals — these are confirmation tests).

If they fail, revisit `drawLink` in Task 8.

- [ ] **Step 3: Commit**

```bash
git add internal/ui/mappane/render_test.go
git commit -m "test(mappane): lock diagonal and 3x3 grid render behaviour"
```

---

### Task 10: Edge omission when data does not match

**Files:**
- Modify: `internal/ui/mappane/render_test.go`

- [ ] **Step 1: Add failing-then-passing test**

```go
func TestRender_OmitsEdgeWhenCoordsDoNotMatch(t *testing.T) {
	// A says exits[E]=B, but B's X is 5 (not 1). The link must NOT be drawn.
	a := makeRoom("a", 0, 0, 0)
	b := makeRoom("b", 5, 0, 0)
	a.Exits[mapper.East] = "b"
	b.Exits[mapper.West] = "a"
	m := &mapper.Map{
		CurrentRoom: "a",
		Rooms:       map[string]*mapper.Room{"a": a, "b": b},
	}
	got := Render(View{PaneWidth: 30, PaneHeight: 9, Map: m, CurrentID: "a"})
	if strings.ContainsRune(got, glyphHLink) {
		t.Errorf("expected NO ─ when destination coord does not match offset:\n%s", got)
	}
}
```

- [ ] **Step 2: Run**

```bash
go test ./internal/ui/mappane/ -run TestRender_OmitsEdgeWhenCoordsDoNotMatch -count=1
```

Expected: PASS — Task 8's `cardinalOffset` mismatch check already enforces this. If it fails, the check at `if other.X != r.X+dx || other.Y != r.Y+dy { continue }` is missing.

- [ ] **Step 3: Commit**

```bash
git add internal/ui/mappane/render_test.go
git commit -m "test(mappane): assert link omitted when data and coords disagree"
```

---

### Task 11: Coord overlap nudge with `!` flag

**Files:**
- Modify: `internal/ui/mappane/render_grid.go`
- Modify: `internal/ui/mappane/render_test.go`

- [ ] **Step 1: Add failing test**

```go
func TestRender_CoordOverlapNudgesAndFlags(t *testing.T) {
	// a and b both live at (0,0). They share a layer because b lists a as
	// its east neighbour even though the coordinates do not match — that
	// link is omitted by the connectivity rule, but LayerOf still treats
	// them as connected because BFS trusts the exit map.
	a := makeRoom("a", 0, 0, 0); a.Name = "A"
	b := makeRoom("b", 0, 0, 0); b.Name = "B"
	a.Exits[mapper.West] = "b"
	b.Exits[mapper.East] = "a"
	m := &mapper.Map{
		CurrentRoom: "a",
		Rooms:       map[string]*mapper.Room{"a": a, "b": b},
	}
	got := Render(View{PaneWidth: 22, PaneHeight: 9, Map: m, CurrentID: "a"})
	if !strings.ContainsRune(got, '!') {
		t.Errorf("expected ! overlap flag in render:\n%s", got)
	}
	if strings.Count(got, string(glyphRoom)) < 1 {
		t.Errorf("expected at least one normal room glyph in render:\n%s", got)
	}
}
```

- [ ] **Step 2: Verify failure**

```bash
go test ./internal/ui/mappane/ -run TestRender_CoordOverlapNudgesAndFlags -count=1
```

Expected: FAIL — current implementation overwrites overlapping cells silently.

- [ ] **Step 3: Implement nudge**

Replace the room-placement loop in `bodyLines` (`internal/ui/mappane/render_grid.go`) with one that detects coordinate collisions and applies the offset. Add this helper above `bodyLines`:

```go
// orderedLayerIDs returns layer IDs in ascending order (deterministic).
func orderedLayerIDs(layer map[string]struct{}) []string {
	out := make([]string, 0, len(layer))
	for id := range layer {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}
```

`render_grid.go` will need `import "sort"` if not already present; merge it with the existing import block.

Replace the room-placement loop with:

```go
	// Track collisions: key is "(rx,ry)" string, value is occurrence index.
	collisions := map[[2]int]int{}
	overlapFlagged := map[[2]int]bool{}
	for _, id := range orderedLayerIDs(layer) {
		r := v.Map.Rooms[id]
		if r == nil {
			continue
		}
		key := [2]int{r.X, r.Y}
		nudge := collisions[key]
		collisions[key] = nudge + 1

		col := centerCol + 2*(r.X-cx) - v.PanOffset.Col + nudge
		row := centerRow + 2*(cy-r.Y) - v.PanOffset.Row
		if col < 0 || col >= cols || row < 0 || row >= rows {
			continue
		}
		switch {
		case id == v.CurrentID:
			grid.set(col, row, glyphCurrentRoom)
		case placeholderName(r.Name):
			grid.set(col, row, glyphPlaceholder)
		default:
			grid.set(col, row, glyphRoom)
		}

		if nudge >= 1 && !overlapFlagged[key] {
			grid.set(col+1, row, '!')
			overlapFlagged[key] = true
		}
	}
```

- [ ] **Step 4: Run tests**

```bash
go test ./internal/ui/mappane/ -count=1
```

Expected: all PASS, including overlap test.

- [ ] **Step 5: Commit**

```bash
git add internal/ui/mappane/render_grid.go internal/ui/mappane/render_test.go
git commit -m "feat(mappane): nudge coord-overlapping rooms with ! flag"
```

---

## Checkpoint C complete

```bash
go test ./... -count=1
```

Expected: all green.

---

## Checkpoint D — Bridges, footer, layer label header

### Task 12: Bridge overlay glyphs ↑ ↓ ▶ ◀

**Files:**
- Modify: `internal/ui/mappane/render_grid.go`
- Modify: `internal/ui/mappane/render_test.go`

- [ ] **Step 1: Add failing test**

```go
func TestRender_BridgeOverlay_CardinalWinsOverArrow(t *testing.T) {
	a := makeRoom("a", 0, 0, 0); a.Name = "A"
	b := makeRoom("b", 0, 1, 0); b.Name = "B"
	c := makeRoom("c", 0, 0, 1); c.Name = "C"
	a.Exits[mapper.North] = "b"
	b.Exits[mapper.South] = "a"
	a.Exits[mapper.Up] = "c"
	c.Exits[mapper.Down] = "a"
	m := &mapper.Map{
		CurrentRoom: "a",
		Rooms:       map[string]*mapper.Room{"a": a, "b": b, "c": c},
	}
	got := Render(View{PaneWidth: 18, PaneHeight: 9, Map: m, CurrentID: "a"})
	lines := strings.Split(got, "\n")
	// Find current row.
	var curRow int
	for i, l := range lines {
		if strings.ContainsRune(l, glyphCurrentRoom) {
			curRow = i
			break
		}
	}
	above := lines[curRow-1]
	if !strings.ContainsRune(above, glyphVLink) {
		t.Errorf("expected │ on row above current room, got %q", above)
	}
	if strings.ContainsRune(above, '↑') {
		t.Errorf("↑ must NOT appear when │ already occupies the gutter, got %q", above)
	}
}

func TestRender_BridgeOverlay_DownAndIn(t *testing.T) {
	a := makeRoom("a", 0, 0, 0)
	b := makeRoom("b", 0, 0, -1)
	c := makeRoom("c", 0, 0, 0)
	a.Exits[mapper.Down] = "b"
	b.Exits[mapper.Up] = "a"
	a.Exits[mapper.In] = "c"
	c.Exits[mapper.Out] = "a"
	m := &mapper.Map{
		CurrentRoom: "a",
		Rooms:       map[string]*mapper.Room{"a": a, "b": b, "c": c},
	}
	got := Render(View{PaneWidth: 18, PaneHeight: 9, Map: m, CurrentID: "a"})
	if !strings.ContainsRune(got, '↓') {
		t.Errorf("expected ↓ overlay below current room:\n%s", got)
	}
	if !strings.ContainsRune(got, '▶') {
		t.Errorf("expected ▶ overlay east of current room:\n%s", got)
	}
}
```

- [ ] **Step 2: Verify failure**

```bash
go test ./internal/ui/mappane/ -run "TestRender_BridgeOverlay" -count=1
```

Expected: FAIL — overlays not implemented.

- [ ] **Step 3: Implement bridge overlay**

Append (after the link-drawing loop) in `bodyLines`:

```go
	// Bridge overlays handled in helper for clarity.
	drawBridgeOverlays(grid, v, layer, cx, cy, centerCol, centerRow, cols, rows)
```

Add the helper at the bottom of `internal/ui/mappane/render_grid.go`:

```go
// drawBridgeOverlays writes the up/down/in/out arrow glyphs into the
// gutters around the current-room cell, but only when the current room is
// part of the rendered layer (otherwise we are viewing a remote layer and
// the player's room belongs elsewhere). A glyph is written only when its
// gutter cell is still ' ' — cardinal/diagonal links keep precedence.
func drawBridgeOverlays(g *grid, v View, layer map[string]struct{}, cx, cy, centerCol, centerRow, cols, rows int) {
	if _, currInLayer := layer[v.CurrentID]; !currInLayer {
		return
	}
	curr, ok := v.Map.Rooms[v.CurrentID]
	if !ok {
		return
	}
	curCol := centerCol + 2*(curr.X-cx) - v.PanOffset.Col
	curRow := centerRow + 2*(cy-curr.Y) - v.PanOffset.Row
	type slot struct {
		dCol, dRow int
		glyph      rune
	}
	bridges := []struct {
		dir  mapper.Direction
		slot slot
	}{
		{mapper.Up, slot{0, -1, '↑'}},
		{mapper.Down, slot{0, +1, '↓'}},
		{mapper.In, slot{+1, 0, '▶'}},
		{mapper.Out, slot{-1, 0, '◀'}},
	}
	for _, br := range bridges {
		if _, has := curr.Exits[br.dir]; !has {
			continue
		}
		tCol := curCol + br.slot.dCol
		tRow := curRow + br.slot.dRow
		if tCol < 0 || tCol >= cols || tRow < 0 || tRow >= rows {
			continue
		}
		if g.get(tCol, tRow) == ' ' {
			g.set(tCol, tRow, br.slot.glyph)
		}
	}
}
```

- [ ] **Step 4: Run tests**

```bash
go test ./internal/ui/mappane/ -count=1
```

Expected: PASS for both bridge-overlay tests; existing tests still green.

- [ ] **Step 5: Commit**

```bash
git add internal/ui/mappane/render_grid.go internal/ui/mappane/render_test.go
git commit -m "feat(mappane): bridge arrow overlays with cardinal-wins precedence"
```

---

### Task 13: Footer composition (bridges + dig hint)

**Files:**
- Modify: `internal/ui/mappane/render_grid.go`
- Modify: `internal/ui/mappane/render_test.go`

- [ ] **Step 1: Add failing tests**

```go
func TestFooter_NoBridges(t *testing.T) {
	a := makeRoom("a", 0, 0, 0); a.Name = "A"
	m := &mapper.Map{CurrentRoom: "a", Rooms: map[string]*mapper.Room{"a": a}}
	got := Render(View{PaneWidth: 30, PaneHeight: 6, Map: m, CurrentID: "a"})
	lines := strings.Split(got, "\n")
	if lines[len(lines)-1] != padLine("dig: u/d/in/out", 30) {
		t.Errorf("footer mismatch: %q", lines[len(lines)-1])
	}
}

func TestFooter_ExistingBridgesAndPartialDigHints(t *testing.T) {
	a := makeRoom("a", 0, 0, 0); a.Name = "A"
	b := makeRoom("b", 0, 0, 1); b.Name = "Stairs"
	a.Exits[mapper.Up] = "b"
	b.Exits[mapper.Down] = "a"
	m := &mapper.Map{CurrentRoom: "a", Rooms: map[string]*mapper.Room{"a": a, "b": b}}
	got := Render(View{PaneWidth: 40, PaneHeight: 6, Map: m, CurrentID: "a"})
	lines := strings.Split(got, "\n")
	want := padLine("↑ Stairs | dig: d/in/out", 40)
	if lines[len(lines)-1] != want {
		t.Errorf("footer mismatch:\nGOT:  %q\nWANT: %q", lines[len(lines)-1], want)
	}
}

func TestFooter_AllFourBridgesNoDigHint(t *testing.T) {
	a := makeRoom("a", 0, 0, 0)
	a.Exits = map[mapper.Direction]string{
		mapper.Up: "u", mapper.Down: "d", mapper.In: "i", mapper.Out: "o",
	}
	rooms := map[string]*mapper.Room{
		"a": a,
		"u": {ID: "u", Name: "UpRoom", Exits: map[mapper.Direction]string{mapper.Down: "a"}},
		"d": {ID: "d", Name: "DnRoom", Exits: map[mapper.Direction]string{mapper.Up: "a"}},
		"i": {ID: "i", Name: "InRoom", Exits: map[mapper.Direction]string{mapper.Out: "a"}},
		"o": {ID: "o", Name: "OutRoom", Exits: map[mapper.Direction]string{mapper.In: "a"}},
	}
	m := &mapper.Map{CurrentRoom: "a", Rooms: rooms}
	got := Render(View{PaneWidth: 60, PaneHeight: 6, Map: m, CurrentID: "a"})
	lines := strings.Split(got, "\n")
	want := padLine("↑ UpRoom | ↓ DnRoom | ▶ InRoom | ◀ OutRoom", 60)
	if lines[len(lines)-1] != want {
		t.Errorf("footer mismatch:\nGOT:  %q\nWANT: %q", lines[len(lines)-1], want)
	}
}
```

- [ ] **Step 2: Verify failure**

```bash
go test ./internal/ui/mappane/ -run TestFooter -count=1
```

Expected: FAIL — current `footerLine` is a stub.

- [ ] **Step 3: Implement `footerLine`**

Replace `footerLine` in `internal/ui/mappane/render_grid.go`:

```go
// footerLine returns the bridge / dig hint footer (single padded row).
//
//   ↑ <name> | ↓ <name> | ▶ <name> | ◀ <name> | dig: <missing dirs>
//
// Only directions that exist on the current room appear in the bridge
// segment. The dig segment lists the subset of {u, d, in, out} that has no
// exit yet. When all bridges are linked, dig is omitted; when none are
// linked and the current room is unknown, the literal `(no layer bridges)`
// is shown.
func footerLine(v View) string {
	if v.Map == nil {
		return padRight("(no layer bridges)", v.PaneWidth)
	}
	curr, ok := v.Map.Rooms[v.CurrentID]
	if !ok {
		return padRight("(no layer bridges)", v.PaneWidth)
	}

	type pair struct {
		dir    mapper.Direction
		arrow  string
		digTok string
	}
	pairs := []pair{
		{mapper.Up, "↑", "u"},
		{mapper.Down, "↓", "d"},
		{mapper.In, "▶", "in"},
		{mapper.Out, "◀", "out"},
	}

	var bridgeSegs []string
	var digMissing []string
	for _, p := range pairs {
		if dest, has := curr.Exits[p.dir]; has {
			name := dest
			if r, ok := v.Map.Rooms[dest]; ok && r.Name != "" {
				name = r.Name
			}
			bridgeSegs = append(bridgeSegs, p.arrow+" "+name)
		} else {
			digMissing = append(digMissing, p.digTok)
		}
	}

	var parts []string
	if len(bridgeSegs) > 0 {
		parts = append(parts, strings.Join(bridgeSegs, " | "))
	}
	if len(digMissing) > 0 {
		parts = append(parts, "dig: "+strings.Join(digMissing, "/"))
	}
	if len(parts) == 0 {
		return padRight("(no layer bridges)", v.PaneWidth)
	}
	return padRight(strings.Join(parts, " | "), v.PaneWidth)
}
```

- [ ] **Step 4: Run tests**

```bash
go test ./internal/ui/mappane/ -count=1
```

Expected: PASS for all footer tests + earlier tests.

- [ ] **Step 5: Commit**

```bash
git add internal/ui/mappane/render_grid.go internal/ui/mappane/render_test.go
git commit -m "feat(mappane): footer with bridge names and dig hint"
```

---

### Task 14: Layer label `(viewing remote)` suffix when LayerKey overrides

**Files:**
- Modify: `internal/ui/mappane/render_grid.go`
- Modify: `internal/ui/mappane/render_test.go`

The label heuristic is already wired (Task 5). This task adds the `(viewing remote)` suffix and the LayerKey override path.

- [ ] **Step 1: Add failing tests**

```go
func TestRender_TwoLayers_DefaultShowsCurrent(t *testing.T) {
	a := makeRoom("a", 0, 0, 0); a.Name = "Square"
	b := makeRoom("b", 0, 0, 1); b.Name = "Loft"
	a.Exits[mapper.Up] = "b"
	b.Exits[mapper.Down] = "a"
	m := &mapper.Map{CurrentRoom: "a", Rooms: map[string]*mapper.Room{"a": a, "b": b}}
	got := Render(View{PaneWidth: 30, PaneHeight: 7, Map: m, CurrentID: "a"})
	lines := strings.Split(got, "\n")
	if !strings.HasPrefix(strings.TrimRight(lines[0], " "), "Square") {
		t.Errorf("expected layer label to start with Square, got %q", lines[0])
	}
	// Layer "b" (Loft) must NOT appear.
	if strings.Contains(got, "Loft") {
		t.Errorf("Loft must not appear when rendering layer A:\n%s", got)
	}
}

func TestRender_TwoLayers_LayerKeyOverridesAndAddsViewingRemote(t *testing.T) {
	a := makeRoom("a", 0, 0, 0); a.Name = "Square"
	b := makeRoom("b", 0, 0, 1); b.Name = "Loft"
	a.Exits[mapper.Up] = "b"
	b.Exits[mapper.Down] = "a"
	m := &mapper.Map{CurrentRoom: "a", Rooms: map[string]*mapper.Room{"a": a, "b": b}}
	got := Render(View{PaneWidth: 36, PaneHeight: 7, Map: m, CurrentID: "a", LayerKey: "b"})
	lines := strings.Split(got, "\n")
	want := "Loft [Z=1] (viewing remote)"
	if strings.TrimRight(lines[0], " ") != want {
		t.Errorf("layer header = %q, want %q", lines[0], want)
	}
	// Should now show layer B's room glyph and not A's.
	if !strings.ContainsRune(got, glyphRoom) && !strings.ContainsRune(got, glyphCurrentRoom) {
		t.Errorf("expected at least one room glyph for remote layer:\n%s", got)
	}
}
```

- [ ] **Step 2: Verify failure**

```bash
go test ./internal/ui/mappane/ -run "TestRender_TwoLayers" -count=1
```

Expected: FAIL — `LayerKey` is currently ignored; `(viewing remote)` not appended.

- [ ] **Step 3: Implement LayerKey resolution**

In `internal/ui/mappane/render_grid.go`, replace the existing existence-check + layer-resolution prelude inside `bodyLines` (the block that today reads `current, ok := v.Map.Rooms[v.CurrentID]` … `cx, cy := current.X, current.Y`) with:

```go
	if _, ok := v.Map.Rooms[v.CurrentID]; !ok {
		grid.setLine(0, "(current room missing from map)")
		return grid.toLines()
	}

	// Determine which room id anchors the layer being rendered.
	anchorID := v.CurrentID
	if v.LayerKey != "" && v.LayerKey != v.CurrentID {
		if _, ok := v.Map.Rooms[v.LayerKey]; ok && !sameLayer(v.Map, v.CurrentID, v.LayerKey) {
			anchorID = v.LayerKey
		}
	}
	layer := LayerOf(v.Map, anchorID)
	if len(layer) == 0 {
		layer = map[string]struct{}{anchorID: {}}
	}

	// Anchor the body on the chosen layer's anchor room. When viewing remote,
	// the current room is not in `layer`, so no current-room glyph is drawn
	// — that is the desired behaviour.
	anchor := v.Map.Rooms[anchorID]
	cx, cy := anchor.X, anchor.Y
```

Add helper to `internal/ui/mappane/layer.go`:

```go
// sameLayer reports whether a and b are in the same connected component.
func sameLayer(m *mapper.Map, a, b string) bool {
	if a == "" || b == "" || a == b {
		return true
	}
	la := LayerOf(m, a)
	if _, ok := la[b]; ok {
		return true
	}
	return false
}
```

In `layerHeaderLines` (in `render_grid.go`), append `(viewing remote)` when `LayerKey` references a different layer:

```go
func layerHeaderLines(v View) []string {
	label := "Unnamed Layer"
	currentName := ""
	if v.Map != nil {
		anchorID := v.CurrentID
		remote := false
		if v.LayerKey != "" && v.LayerKey != v.CurrentID {
			if _, ok := v.Map.Rooms[v.LayerKey]; ok && !sameLayer(v.Map, v.CurrentID, v.LayerKey) {
				anchorID = v.LayerKey
				remote = true
			}
		}
		if anchorID != "" {
			layer := LayerOf(v.Map, anchorID)
			if len(layer) > 0 {
				label = LayerLabel(v.Map, layer, anchorID)
			}
			if remote {
				label += " (viewing remote)"
			}
		}
		if r, ok := v.Map.Rooms[v.CurrentID]; ok {
			currentName = r.Name
		}
	}
	return []string{
		padRight(label, v.PaneWidth),
		padRight(currentName, v.PaneWidth),
	}
}
```

The body must not show the current-room glyph if the current room is on a different layer than the rendered one. Adjust the placement loop:

```go
		switch {
		case id == v.CurrentID:
			grid.set(col, row, glyphCurrentRoom)
		case placeholderName(r.Name):
			grid.set(col, row, glyphPlaceholder)
		default:
			grid.set(col, row, glyphRoom)
		}
```

This already only places `glyphCurrentRoom` when iterating an ID equal to `CurrentID`. When viewing a remote layer, `CurrentID` is not in the layer set, so no current-room glyph appears — correct behaviour.

- [ ] **Step 4: Run tests**

```bash
go test ./internal/ui/mappane/ -count=1
```

Expected: PASS for two-layer tests + all previous tests.

- [ ] **Step 5: Commit**

```bash
git add internal/ui/mappane/render_grid.go internal/ui/mappane/layer.go internal/ui/mappane/render_test.go
git commit -m "feat(mappane): LayerKey override and (viewing remote) suffix"
```

---

## Checkpoint D complete

```bash
go test ./... -count=1
```

Expected: all green.

---

## Checkpoint E — Pan offset, recenter, edge indicator

### Task 15: Pan offset and `*` clipped-current indicator

**Files:**
- Modify: `internal/ui/mappane/render_grid.go`
- Modify: `internal/ui/mappane/render_test.go`

Pan offset is already applied in coordinate math; this task adds the **clipped indicator** when the current room is panned off the body.

- [ ] **Step 1: Add failing test**

```go
func TestRender_PanOffsetMovesRoomsAndClipsCurrent(t *testing.T) {
	a := makeRoom("a", 0, 0, 0); a.Name = "A"
	m := &mapper.Map{CurrentRoom: "a", Rooms: map[string]*mapper.Room{"a": a}}
	w, h := 18, 9
	got := Render(View{
		PaneWidth: w, PaneHeight: h, Map: m, CurrentID: "a",
		PanOffset: Point{Col: 100, Row: 0}, // pans far past current → clipped
	})
	if strings.ContainsRune(got, glyphCurrentRoom) {
		t.Errorf("current room must be clipped at this pan offset:\n%s", got)
	}
	if !strings.ContainsRune(got, '*') {
		t.Errorf("expected * indicator when current is clipped:\n%s", got)
	}
}
```

- [ ] **Step 2: Verify failure**

```bash
go test ./internal/ui/mappane/ -run TestRender_PanOffsetMovesRoomsAndClipsCurrent -count=1
```

Expected: FAIL — no `*` placed.

- [ ] **Step 3: Implement clipped indicator**

Append at the end of `bodyLines` (before `return grid.toLines()`):

```go
	// Clipped current-room indicator: if the current room exists in the
	// rendered layer but its placement falls outside the body, place a '*'
	// at the body edge nearest the room's logical direction.
	if curr, ok := v.Map.Rooms[v.CurrentID]; ok {
		if _, isInLayer := layer[v.CurrentID]; isInLayer {
			col := centerCol + 2*(curr.X-cx) - v.PanOffset.Col
			row := centerRow + 2*(cy-curr.Y) - v.PanOffset.Row
			if col < 0 || col >= cols || row < 0 || row >= rows {
				eCol := col
				eRow := row
				if eCol < 0 {
					eCol = 0
				} else if eCol >= cols {
					eCol = cols - 1
				}
				if eRow < 0 {
					eRow = 0
				} else if eRow >= rows {
					eRow = rows - 1
				}
				grid.set(eCol, eRow, '*')
			}
		}
	}
```

- [ ] **Step 4: Run tests**

```bash
go test ./internal/ui/mappane/ -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/ui/mappane/render_grid.go internal/ui/mappane/render_test.go
git commit -m "feat(mappane): clipped-current * edge indicator"
```

---

## Checkpoint E complete — pure renderer is feature-complete.

Run the test suite:

```bash
go test ./... -count=1
```

Expected: all green.

---

## Checkpoint F — Model integration

### Task 16: `MapShow` command type

**Files:**
- Modify: `internal/command/command.go`

- [ ] **Step 1: Add the type**

In `internal/command/command.go`, add to the Mapper section (after `MapMermaid`):

```go
type MapShow struct{}
```

and in the `isCommand` block:

```go
func (*MapShow) isCommand()    {}
```

- [ ] **Step 2: Verify build**

```bash
go build ./...
```

Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add internal/command/command.go
git commit -m "feat(command): add MapShow command type"
```

---

### Task 17: `/map show` parser case

**Files:**
- Modify: `internal/input/handler.go` (subcommand list line, summary, dispatch)

- [ ] **Step 1: Add failing test**

Append to `internal/input/handler_test.go`:

```go
func TestCmdMap_Show(t *testing.T) {
	h := NewHandler()
	res := h.HandleInput("/map show")
	if len(res) != 1 {
		t.Fatalf("expected 1 result, got %d", len(res))
	}
	if _, ok := res[0].Command.(*command.MapShow); !ok {
		t.Errorf("expected *command.MapShow, got %T", res[0].Command)
	}
}
```

- [ ] **Step 2: Verify failure**

```bash
go test ./internal/input/ -run TestCmdMap_Show -count=1
```

Expected: FAIL — currently returns "Unknown subcommand" or similar.

- [ ] **Step 3: Add the case**

In `internal/input/handler.go`, inside `cmdMap`, add a `case` (placement: alphabetical near `mermaid`):

```go
	case "show":
		return ParseResult{Command: &command.MapShow{}, Response: "Toggling map pane..."}
```

Update the subcommand summary line (search for `Subcommands: create, paths, dig, undo, delete, teleport, link, name, search, mermaid, info, option, start, stop, exit`):

```go
				Response: "Usage: /map <subcommand> [args...]\nSubcommands: create, paths, dig, undo, delete, teleport, link, name, search, mermaid, show, info, option, start, stop, exit",
```

Search for the `/help` block in `handler.go` that lists `/map …` lines, and insert after the `/map mermaid` row:

```go
  /map show                           Toggle the side-by-side map pane
```

- [ ] **Step 4: Run tests**

```bash
go test ./internal/input/ -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/input/handler.go internal/input/handler_test.go
git commit -m "feat(input): /map show parser and help text"
```

---

### Task 18: UI messages file

**Files:**
- Create: `internal/ui/messages.go`

- [ ] **Step 1: Write the file**

```go
package ui

// MapPaneToggleMsg is sent by the app layer when the user issues
// `/map show`. The model flips visibility, recomputes split widths, and
// rejects the toggle if the terminal is too narrow.
type MapPaneToggleMsg struct{}

// MapPaneRecenterMsg is sent by the app layer after a mapper mutation
// (e.g. movement, dig) so the model can clear pan offset and snap the pane
// back to the (possibly new) current room.
type MapPaneRecenterMsg struct{}
```

- [ ] **Step 2: Verify build**

```bash
go build ./...
```

Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add internal/ui/messages.go
git commit -m "feat(ui): MapPaneToggle and Recenter messages"
```

---

### Task 19: Model state + snapshot accessor

**Files:**
- Modify: `internal/ui/model.go`

- [ ] **Step 1: Add fields, helpers, and setter**

In the `Model` struct (after `pendingTick    bool`), add:

```go
	// Map pane (side-by-side renderer for /map show).
	mapPaneVisible    bool
	mapPaneWidth      int
	mapPanOffset      mappane.Point
	mapPaneLayerKey   string
	mapPaneLastCurrID string
	mapEngineSnapshot func() (*mapper.Map, string)
```

Add the import group entries: `"github.com/ayder/gotin/internal/mapper"` and `"github.com/ayder/gotin/internal/ui/mappane"`.

Below `New(...)`, add the setter:

```go
// SetMapEngineSnapshot wires a snapshot accessor into the model. The
// accessor must be safe for concurrent use; it is invoked from the model's
// View method and also from MapPaneRecenterMsg handling.
func (m *Model) SetMapEngineSnapshot(fn func() (*mapper.Map, string)) {
	m.mapEngineSnapshot = fn
}
```

Add helpers near the end of the file (before the closing brace of the package `ui`):

```go
// computeMapPaneWidth returns the requested pane width given a terminal
// width, applying the spec's clamp and minimum-chat-width rule. Returns 0
// when the pane should not be shown.
func computeMapPaneWidth(termW int) int {
	if termW < 65 {
		return 0
	}
	w := (termW + 2) / 3 // ceil(termW / 3)
	if w < 24 {
		w = 24
	}
	if w > 48 {
		w = 48
	}
	return w
}
```

- [ ] **Step 2: Build to confirm no syntax error**

```bash
go build ./internal/ui/...
```

Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add internal/ui/model.go
git commit -m "feat(ui): map pane state and snapshot setter"
```

---

### Task 20: Model handles MapPaneToggleMsg

**Files:**
- Modify: `internal/ui/model.go`
- Create: `internal/ui/model_test.go` (if absent — current code does not have one yet)

- [ ] **Step 1: Add failing test**

Create `internal/ui/model_test.go`:

```go
package ui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ayder/gotin/internal/mapper"
)

func freshModel(termW, termH int) Model {
	m := New(nil, nil)
	tm, _ := m.Update(tea.WindowSizeMsg{Width: termW, Height: termH})
	return tm.(Model)
}

func TestMapPaneToggle_VisibleAt80x24(t *testing.T) {
	m := freshModel(80, 24)
	tm, _ := m.Update(MapPaneToggleMsg{})
	m = tm.(Model)
	if !m.mapPaneVisible {
		t.Fatal("pane should be visible after toggle")
	}
	if m.mapPaneWidth == 0 {
		t.Fatal("expected nonzero pane width")
	}
	if m.viewport.Width != 80-m.mapPaneWidth-1 {
		t.Errorf("chat viewport width = %d, want %d", m.viewport.Width, 80-m.mapPaneWidth-1)
	}
}

func TestMapPaneToggle_RejectsNarrowTerminal(t *testing.T) {
	m := freshModel(50, 24)
	tm, _ := m.Update(MapPaneToggleMsg{})
	m = tm.(Model)
	if m.mapPaneVisible {
		t.Error("pane must not toggle on at width<65")
	}
}

func TestMapPaneToggle_OffRestoresFullViewport(t *testing.T) {
	m := freshModel(80, 24)
	tm, _ := m.Update(MapPaneToggleMsg{})
	m = tm.(Model)
	tm, _ = m.Update(MapPaneToggleMsg{})
	m = tm.(Model)
	if m.mapPaneVisible {
		t.Fatal("expected pane invisible after second toggle")
	}
	if m.viewport.Width != 80 {
		t.Errorf("expected viewport width restored to 80, got %d", m.viewport.Width)
	}
}

// quiet unused-import: mapper used by later tests.
var _ = mapper.Direction("")
```

- [ ] **Step 2: Verify failure**

```bash
go test ./internal/ui/ -count=1
```

Expected: FAIL — `MapPaneToggleMsg` is not handled.

- [ ] **Step 3: Add handler**

In the `Update` switch in `internal/ui/model.go`, add a case (place it between `case ConfirmConnectMsg:` and `case SetLocalEchoMsg:`):

```go
	case MapPaneToggleMsg:
		if !m.mapPaneVisible {
			pw := computeMapPaneWidth(m.width)
			if pw == 0 {
				m.statusMsg = "terminal too narrow for /map show"
				return m, nil
			}
			m.mapPaneVisible = true
			m.mapPaneWidth = pw
			m.viewport.Width = m.width - pw - 1
			m.mapPanOffset = mappane.Point{}
			m.mapPaneLayerKey = ""
		} else {
			m.mapPaneVisible = false
			m.mapPaneWidth = 0
			m.viewport.Width = m.width
		}
		return m, nil

	case MapPaneRecenterMsg:
		m.mapPanOffset = mappane.Point{}
		m.mapPaneLayerKey = ""
		return m, nil
```

Also update the `WindowSizeMsg` handler to recompute pane width if visible. Replace the existing `viewportHeight` block with:

```go
		viewportHeight := m.height - InputHeight
		viewportW := m.width
		if m.mapPaneVisible {
			pw := computeMapPaneWidth(m.width)
			if pw == 0 {
				m.mapPaneVisible = false
				m.mapPaneWidth = 0
			} else {
				m.mapPaneWidth = pw
				viewportW = m.width - pw - 1
			}
		}

		if !m.ready {
			m.viewport = viewport.New(viewportW, viewportHeight)
			m.viewport.SetContent("")
			m.ready = true
		} else {
			m.viewport.Width = viewportW
			m.viewport.Height = viewportHeight
		}
```

- [ ] **Step 4: Run tests**

```bash
go test ./internal/ui/ -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/ui/model.go internal/ui/model_test.go
git commit -m "feat(ui): MapPaneToggle/Recenter handlers and resize integration"
```

---

### Task 21: View() composes split layout

**Files:**
- Modify: `internal/ui/model.go`
- Modify: `internal/ui/model_test.go`

- [ ] **Step 1: Add failing assertion**

Append to `internal/ui/model_test.go`:

```go
func TestMapPaneView_RendersSplit(t *testing.T) {
	m := freshModel(80, 24)
	// Inject a tiny snapshot accessor so the pane has something to draw.
	rooms := map[string]*mapper.Room{
		"a": {ID: "a", Name: "Square", Exits: map[mapper.Direction]string{}},
	}
	mm := &mapper.Map{CurrentRoom: "a", Rooms: rooms}
	m.SetMapEngineSnapshot(func() (*mapper.Map, string) { return mm, "a" })
	tm, _ := m.Update(MapPaneToggleMsg{})
	m = tm.(Model)
	out := m.View()
	if !strings.Contains(out, "Square") {
		t.Errorf("expected pane header to contain Square in:\n%s", out)
	}
}
```

Add `"strings"` to the test imports if not already there.

- [ ] **Step 2: Verify failure**

```bash
go test ./internal/ui/ -run TestMapPaneView_RendersSplit -count=1
```

Expected: FAIL — `View()` does not render the pane yet.

- [ ] **Step 3: Implement split View()**

In `internal/ui/model.go`, near the end of `View()` (before the `if mm.showConfirmConnect` line), insert:

```go
	if mm.mapPaneVisible {
		view = mm.composeMapSplit(view)
	}
```

Add the helper:

```go
// composeMapSplit joins the existing left view with a freshly rendered map
// pane on the right. The left block is split into its viewport area and the
// status/input/badges block; only the viewport gets compressed horizontally.
func (mm Model) composeMapSplit(leftView string) string {
	if mm.mapEngineSnapshot == nil {
		return leftView
	}
	snap, currID := mm.mapEngineSnapshot()
	paneW := mm.mapPaneWidth
	paneH := mm.height - InputHeight
	pane := mappane.Render(mappane.View{
		PaneWidth:  paneW,
		PaneHeight: paneH,
		Map:        snap,
		CurrentID:  currID,
		PanOffset:  mm.mapPanOffset,
		LayerKey:   mm.mapPaneLayerKey,
	})

	// Split leftView into the viewport block (top, paneH lines) and the rest.
	lines := strings.SplitN(leftView, "\n", paneH+1)
	if len(lines) < paneH+1 {
		// Not enough lines to cleanly compose; fall back to leftView.
		return leftView
	}
	top := strings.Join(lines[:paneH], "\n")
	rest := lines[paneH]

	separator := strings.Repeat("│\n", paneH-1) + "│"
	joined := lipgloss.JoinHorizontal(lipgloss.Top, top, separator, pane)
	return joined + "\n" + rest
}
```

- [ ] **Step 4: Run tests**

```bash
go test ./internal/ui/ -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/ui/model.go internal/ui/model_test.go
git commit -m "feat(ui): compose split view via JoinHorizontal"
```

---

### Task 22: Pane key routing (`h/j/k/l`, arrows when input empty)

**Files:**
- Modify: `internal/ui/model.go`
- Modify: `internal/ui/model_test.go`

- [ ] **Step 1: Add failing tests**

Append:

```go
func TestPaneKeys_PanOnlyWhenInputEmpty(t *testing.T) {
	m := freshModel(80, 24)
	mm := &mapper.Map{CurrentRoom: "a", Rooms: map[string]*mapper.Room{
		"a": {ID: "a", Exits: map[mapper.Direction]string{}},
	}}
	m.SetMapEngineSnapshot(func() (*mapper.Map, string) { return mm, "a" })
	tm, _ := m.Update(MapPaneToggleMsg{})
	m = tm.(Model)

	// Pane visible, input empty: 'l' should pan +2 cols.
	tm, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}})
	m = tm.(Model)
	if m.mapPanOffset.Col != 2 {
		t.Errorf("pan col = %d, want 2", m.mapPanOffset.Col)
	}
	if m.textinput.Value() != "" {
		t.Errorf("textinput must remain empty, got %q", m.textinput.Value())
	}

	// Now type some characters → pane keys must NOT pan.
	m.textinput.SetValue("look")
	tm, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}})
	m = tm.(Model)
	if m.mapPanOffset.Col != 2 {
		t.Errorf("pan col changed while typing: got %d", m.mapPanOffset.Col)
	}
}

func TestPaneKeys_RecenterOnC(t *testing.T) {
	m := freshModel(80, 24)
	mm := &mapper.Map{CurrentRoom: "a", Rooms: map[string]*mapper.Room{
		"a": {ID: "a", Exits: map[mapper.Direction]string{}},
	}}
	m.SetMapEngineSnapshot(func() (*mapper.Map, string) { return mm, "a" })
	tm, _ := m.Update(MapPaneToggleMsg{})
	m = tm.(Model)
	m.mapPanOffset = mappane.Point{Col: 4, Row: 4}

	tm, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	m = tm.(Model)
	if m.mapPanOffset != (mappane.Point{}) {
		t.Errorf("expected pan reset on c, got %+v", m.mapPanOffset)
	}
}
```

Imports: ensure `tea "github.com/charmbracelet/bubbletea"` is in test imports.

- [ ] **Step 2: Verify failure**

```bash
go test ./internal/ui/ -run "TestPaneKeys" -count=1
```

Expected: FAIL — keys are routed straight to textinput today.

- [ ] **Step 3: Implement key routing**

At the very top of the `case tea.KeyMsg:` branch (before the existing `if m.showConfirmConnect` block), insert:

```go
		if m.mapPaneVisible && m.textinput.Value() == "" {
			if handled, nm, cmd := m.handleMapPaneKey(msg); handled {
				return nm, cmd
			}
		}
```

Add the handler method anywhere after `Update`:

```go
// handleMapPaneKey returns (handled, model, cmd). It is invoked only when
// the pane is visible AND the textinput is empty. It consumes pan, recenter,
// layer-step, and Esc keys; everything else falls through to normal input
// processing.
func (m Model) handleMapPaneKey(msg tea.KeyMsg) (bool, tea.Model, tea.Cmd) {
	step := 2
	switch msg.Type {
	case tea.KeyLeft:
		m.mapPanOffset.Col -= step
		return true, m, nil
	case tea.KeyRight:
		m.mapPanOffset.Col += step
		return true, m, nil
	case tea.KeyUp:
		m.mapPanOffset.Row -= step
		return true, m, nil
	case tea.KeyDown:
		m.mapPanOffset.Row += step
		return true, m, nil
	case tea.KeyEsc:
		m.mapPaneVisible = false
		m.viewport.Width = m.width
		return true, m, nil
	}
	switch msg.String() {
	case "h":
		m.mapPanOffset.Col -= step
		return true, m, nil
	case "l":
		m.mapPanOffset.Col += step
		return true, m, nil
	case "k":
		m.mapPanOffset.Row -= step
		return true, m, nil
	case "j":
		m.mapPanOffset.Row += step
		return true, m, nil
	case "c":
		m.mapPanOffset = mappane.Point{}
		m.mapPaneLayerKey = ""
		return true, m, nil
	case "[":
		return true, m.stepLayer(-1), nil
	case "]":
		return true, m.stepLayer(+1), nil
	}
	return false, m, nil
}

// stepLayer advances the LayerKey by direction d (-1 prev, +1 next) using
// the current snapshot. Out-of-range wraps around. Recenters pan when
// switching.
func (m Model) stepLayer(d int) Model {
	if m.mapEngineSnapshot == nil {
		return m
	}
	snap, currID := m.mapEngineSnapshot()
	if snap == nil {
		return m
	}
	reps := mappane.AllLayers(snap)
	if len(reps) <= 1 {
		return m
	}
	// Anchor: which rep are we currently rendering?
	anchor := m.mapPaneLayerKey
	if anchor == "" {
		// Find the rep of the current room's layer.
		for _, r := range reps {
			la := mappane.LayerOf(snap, r)
			if _, ok := la[currID]; ok {
				anchor = r
				break
			}
		}
	}
	idx := 0
	for i, r := range reps {
		if r == anchor {
			idx = i
			break
		}
	}
	idx = (idx + d + len(reps)) % len(reps)
	m.mapPaneLayerKey = reps[idx]
	m.mapPanOffset = mappane.Point{}
	return m
}
```

- [ ] **Step 4: Run tests**

```bash
go test ./internal/ui/ -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/ui/model.go internal/ui/model_test.go
git commit -m "feat(ui): pane key routing (pan, recenter, [/], esc)"
```

---

### Task 23: Esc closes pane only when input empty

**Files:**
- Modify: `internal/ui/model_test.go`

The Esc behaviour is already wired by Task 22 (the `handleMapPaneKey` runs only when input is empty). This task asserts the behaviour and the no-op when input is non-empty.

- [ ] **Step 1: Add failing-then-passing test**

```go
func TestPaneKeys_EscClosesWhenEmpty(t *testing.T) {
	m := freshModel(80, 24)
	tm, _ := m.Update(MapPaneToggleMsg{})
	m = tm.(Model)
	tm, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = tm.(Model)
	if m.mapPaneVisible {
		t.Error("Esc did not close pane")
	}
}

func TestPaneKeys_EscDoesNotCloseWhenTyping(t *testing.T) {
	m := freshModel(80, 24)
	tm, _ := m.Update(MapPaneToggleMsg{})
	m = tm.(Model)
	m.textinput.SetValue("look")
	tm, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = tm.(Model)
	if !m.mapPaneVisible {
		t.Error("Esc must not close pane while user is typing")
	}
}
```

- [ ] **Step 2: Run**

```bash
go test ./internal/ui/ -run TestPaneKeys_Esc -count=1
```

Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add internal/ui/model_test.go
git commit -m "test(ui): assert Esc pane-close gating"
```

---

### Task 24: App layer wiring — toggle and snapshot injection

**Files:**
- Modify: `internal/app/app.go`

- [ ] **Step 1: Inject snapshot accessor after engine creation**

After `s.mapEngine = mapper.NewEngine("")` (around line 206), add:

```go
	s.model.SetMapEngineSnapshot(func() (*mapper.Map, string) {
		return s.mapEngine.Snapshot()
	})
```

Imports: `mapper` is already imported in `app.go`.

- [ ] **Step 2: Dispatch `MapShow` in `dispatch`**

Find the `case *command.MapMermaid:` block. Add immediately after it:

```go
	case *command.MapShow:
		s.program.Send(ui.MapPaneToggleMsg{})
		return false, false
```

- [ ] **Step 3: Build to confirm**

```bash
go build ./...
```

Expected: PASS.

- [ ] **Step 4: Smoke-test the dispatch**

Start a session manually:

```bash
go run ./cmd/gotin
```

Type `/map create` (no host needed). Type `/map show`. Pane should toggle on (or "terminal too narrow" if narrow). `Esc` should close.

(Manual; no Go test for this step.)

- [ ] **Step 5: Commit**

```bash
git add internal/app/app.go
git commit -m "feat(app): wire /map show to MapPaneToggleMsg + snapshot accessor"
```

---

### Task 25: Auto-recenter on `[Map]` status messages

**Files:**
- Modify: `internal/ui/model.go`
- Modify: `internal/ui/model_test.go`

- [ ] **Step 1: Add failing test**

```go
func TestStatusMsg_TriggersRecenterWhenCurrentRoomChanges(t *testing.T) {
	m := freshModel(80, 24)
	curr := "a"
	rooms := map[string]*mapper.Room{
		"a": {ID: "a", Name: "A", Exits: map[mapper.Direction]string{}},
		"b": {ID: "b", Name: "B", Exits: map[mapper.Direction]string{}},
	}
	mm := &mapper.Map{CurrentRoom: "a", Rooms: rooms}
	m.SetMapEngineSnapshot(func() (*mapper.Map, string) { return mm, curr })
	tm, _ := m.Update(MapPaneToggleMsg{})
	m = tm.(Model)
	m.mapPanOffset = mappane.Point{Col: 6, Row: 6}
	m.mapPaneLastCurrID = "a"

	// Simulate a movement; current ID flips.
	curr = "b"
	mm.CurrentRoom = "b"
	tm, _ = m.Update(StatusMsg{Message: "[Map] Moved to: B"})
	m = tm.(Model)

	if m.mapPanOffset != (mappane.Point{}) {
		t.Errorf("expected recenter on room change, offset = %+v", m.mapPanOffset)
	}
	if m.mapPaneLastCurrID != "b" {
		t.Errorf("expected lastCurrID=b, got %q", m.mapPaneLastCurrID)
	}
}
```

- [ ] **Step 2: Verify failure**

```bash
go test ./internal/ui/ -run TestStatusMsg_TriggersRecenter -count=1
```

Expected: FAIL.

- [ ] **Step 3: Hook into `case StatusMsg:`**

In the existing `case StatusMsg:` branch in `Update`, immediately after the `m.statusMsg = trimmed` line (single-line status update path), insert:

```go
		if strings.HasPrefix(trimmed, "[Map]") && m.mapPaneVisible && m.mapEngineSnapshot != nil {
			_, currID := m.mapEngineSnapshot()
			if currID != "" && currID != m.mapPaneLastCurrID {
				m.mapPanOffset = mappane.Point{}
				m.mapPaneLayerKey = ""
				m.mapPaneLastCurrID = currID
			}
		}
```

The same hook should fire for the multi-line StatusMsg path; insert the same block right before `return m, tickCmd` in that branch.

- [ ] **Step 4: Run tests**

```bash
go test ./internal/ui/ -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/ui/model.go internal/ui/model_test.go
git commit -m "feat(ui): auto-recenter pane on [Map] status when current room changes"
```

---

## Checkpoint F complete

```bash
go test ./... -count=1
```

Expected: all green.

---

## Checkpoint G — Smoke + sweep

### Task 26: Manual smoke checklist

**Files:** none (manual).

Run a real session against t2tmud and verify each item:

```bash
go run ./cmd/gotin -host t2tmud.org -port 9999 -debug
```

Steps:

- [ ] `/map create world.json` — pane will be empty until first room.
- [ ] Log in, walk to a room → `/map show`. Confirm pane appears, room glyph centred.
- [ ] Walk `e`, `n`, `nw` → cardinal/diagonal links draw cleanly between adjacent boxes. No links to non-adjacent rooms.
- [ ] `/map dig u stairs` (on a stairwell) → `↑` overlay appears on current room. Walking `up` switches the pane to the new layer; old layer disappears.
- [ ] `[` / `]` rotate through layers; `(viewing remote)` suffix appears. `c` snaps back.
- [ ] Resize the terminal narrower than 65 cols → status line says "terminal too narrow"; pane closes if it was on.
- [ ] Type a command (`look`) — `h`/`j`/`k`/`l` characters reach the input, do not pan.
- [ ] `Esc` while input empty closes the pane; `Esc` while typing leaves it alone.
- [ ] `/map mermaid` still produces the Mermaid flowchart (regression check).

If any step fails, stop and write a regression test before fixing.

### Task 27: Final sweep + dev fixture cleanup

- [ ] **Step 1:** Re-run the full suite under race detection:

```bash
go test ./... -race -count=1
```

Expected: all green.

- [ ] **Step 2:** Confirm `go vet`:

```bash
go vet ./...
```

Expected: no findings.

- [ ] **Step 3:** Update `README.md` map command table — add a row for `/map show`:

Find the existing line `| /map mermaid [radius|all] | Render Mermaid graph of nearby rooms |` and add right after:

```
| `/map show` | Toggle a side-by-side terminal map pane on the right of the MUD output |
```

- [ ] **Step 4:** Update `docs/README_mapping.md` similarly. Find the `Visualizing the Map` section and add right above the `/map mermaid` entry:

```
- `/map show`
  Toggles the live, terminal-native map pane to the right of the MUD output.
  Cell pitch (2,2). Layer auto-switches on up/down/in/out. Pan with h/j/k/l
  while the input line is empty; `c` recenters; `[`/`]` step layers; `Esc`
  closes.
```

- [ ] **Step 5:** Commit:

```bash
git add README.md docs/README_mapping.md
git commit -m "docs: document /map show side-by-side pane"
```

---

## Self-Review Checklist

Use this once after the final commit:

1. `/map show` toggles the pane on widths ≥ 65 → Tasks 17, 19, 20, 24.
2. Cardinal/diagonal links honour the connectivity rule → Tasks 8, 9, 10.
3. Layer switches automatically on up/down/in/out → Task 25.
4. Bridge overlays + footer → Tasks 12, 13.
5. Pan, recenter, layer step → Task 22.
6. Esc gating → Tasks 22, 23.
7. Auto-recenter on `[Map]` status → Task 25.
8. `/map mermaid` untouched → no edits to that case in `app.go`.
9. 340 existing tests + new ones all green → Task 27 final sweep.
