package mapper

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// ─── Helpers ───────────────────────────────────────────────────────────────

func newEngine(t *testing.T) *Engine {
	t.Helper()
	return NewEngine("")
}

func mustCreate(t *testing.T, filename string) *Engine {
	t.Helper()
	e := NewEngine("")
	if err := e.Create(filename); err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	return e
}

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
		go func() {
			defer wg.Done()
			e.SetMappingOptions(MappingOptions{Vnum: true, Hash: false})
		}()
		go func() {
			defer wg.Done()
			_ = e.GetMappingOptions()
		}()
	}
	wg.Wait()
}

func TestNormaliseDescription(t *testing.T) {
	in := "  A   plain.\n  Two  spaces.  "
	want := "A plain.\nTwo spaces."
	if got := normaliseDescription(in); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestComputeStructuralHash_Stable(t *testing.T) {
	a := ComputeStructuralHash("A room.", []string{"n", "e"})
	b := ComputeStructuralHash("A room.", []string{"e", "n"})
	if a != b {
		t.Errorf("hash should ignore exit order: %q vs %q", a, b)
	}
}

func TestComputeStructuralHash_DifferentExitsDiffer(t *testing.T) {
	a := ComputeStructuralHash("A room.", []string{"n"})
	b := ComputeStructuralHash("A room.", []string{"s"})
	if a == b {
		t.Errorf("hash should differ on exit set")
	}
}

func TestComputeStructuralHash_DistinctFromV1(t *testing.T) {
	v2 := ComputeStructuralHash("A room.", []string{"n"})
	v1 := ComputeRoomHash("A room.", []string{"n"})
	if !strings.HasPrefix(v2, "v2:") {
		t.Errorf("v2 hash missing prefix: %q", v2)
	}
	if v1 == v2 {
		t.Errorf("v1 and v2 hashes must not collide")
	}
	if strings.HasPrefix(v1, "v2:") {
		t.Errorf("v1 hash unexpectedly has v2 prefix: %q", v1)
	}
}

func TestEngineMUDProfile_RoundTrip(t *testing.T) {
	e := NewEngine("")
	got := e.GetMUDProfile()
	if got.Host != "t2tmud.org" {
		t.Errorf("default profile host = %q, want t2tmud.org", got.Host)
	}
	custom := MUDProfile{Host: "example.org", BlockStartTag: "ROOM"}
	e.SetMUDProfile(custom)
	if e.GetMUDProfile().Host != "example.org" {
		t.Errorf("SetMUDProfile did not persist host")
	}
}

func TestEngineSentinels(t *testing.T) {
	e := NewEngine("")
	if e.HasMXPSeen() {
		t.Errorf("default HasMXPSeen must be false")
	}
	if e.HasBlockStartSeen() {
		t.Errorf("default HasBlockStartSeen must be false")
	}
	e.MarkMXPSeen()
	if !e.HasMXPSeen() {
		t.Errorf("MarkMXPSeen did not set sawMXP")
	}
	if e.HasBlockStartSeen() {
		t.Errorf("MarkMXPSeen must NOT set sawBlockStart")
	}
	e.MarkBlockStartSeen()
	if !e.HasBlockStartSeen() {
		t.Errorf("MarkBlockStartSeen did not set sawBlockStart")
	}
	e.ResetBlockStart()
	if e.HasMXPSeen() {
		t.Errorf("ResetBlockStart did not clear sawMXP")
	}
	if e.HasBlockStartSeen() {
		t.Errorf("ResetBlockStart did not clear sawBlockStart")
	}
}

func TestProcessMovement_RequiresMXPAndBlockStartSentinels(t *testing.T) {
	e := mustCreate(t, "")
	e.SetMappingOptions(MappingOptions{Vnum: false, Hash: true})
	e.StartAutoMapping()

	processed, _, err := e.ProcessMovement("n")
	if err != nil {
		t.Fatalf("ProcessMovement: %v", err)
	}
	if processed {
		t.Errorf("processed=true without sawMXP sentinel")
	}
	if e.HasPendingMovement() {
		t.Errorf("pending state set without sawMXP")
	}

	e.MarkMXPSeen()
	processed, _, err = e.ProcessMovement("n")
	if err != nil {
		t.Fatalf("ProcessMovement: %v", err)
	}
	if processed {
		t.Errorf("processed=true without sawBlockStart sentinel")
	}
	if e.HasPendingMovement() {
		t.Errorf("pending state set without sawBlockStart")
	}
}

func TestHandleRoomBlock_NoOpWhenAutoMappingOff(t *testing.T) {
	e := mustCreate(t, "")
	processed, _, _, err := e.HandleRoomBlock(RoomBlock{Description: "x", Exits: []string{"n"}})
	if processed || err != nil {
		t.Errorf("processed=%v err=%v with auto-mapping off", processed, err)
	}
}

func TestHandleRoomBlock_NoOpWhenSentinelMissing(t *testing.T) {
	e := mustCreate(t, "")
	e.StartAutoMapping()
	e.MarkMXPSeen()
	processed, _, _, _ := e.HandleRoomBlock(RoomBlock{Description: "x", Exits: []string{"n"}})
	if processed {
		t.Errorf("processed=true without block-start sentinel")
	}
}

func TestHandleRoomBlock_PendingMovementCreatesNewRoom(t *testing.T) {
	e := mustCreate(t, "")
	e.SetMappingOptions(MappingOptions{Vnum: false, Hash: true})
	e.StartAutoMapping()
	e.MarkMXPSeen()
	e.MarkBlockStartSeen()
	if _, _, err := e.ProcessMovement("n"); err != nil {
		t.Fatalf("ProcessMovement: %v", err)
	}
	_, _, loop, err := e.HandleRoomBlock(RoomBlock{Description: "Plaza.", Exits: []string{"s"}})
	if err != nil {
		t.Fatalf("HandleRoomBlock: %v", err)
	}
	if loop {
		t.Errorf("first arrival should not be a loop")
	}
	curr := e.GetCurrent()
	if curr == nil || curr.Description == "" || !strings.HasPrefix(curr.DescriptionHash, "v2:") {
		t.Errorf("current room not populated with v2 hash: %+v", curr)
	}
}

func TestHandleRoomBlock_LoopDetectionByStructuralHash(t *testing.T) {
	e := mustCreate(t, "")
	e.SetMappingOptions(MappingOptions{Vnum: false, Hash: true})
	e.StartAutoMapping()
	e.MarkMXPSeen()
	e.MarkBlockStartSeen()

	_, _, _ = e.ProcessMovement("n")
	_, _, _, _ = e.HandleRoomBlock(RoomBlock{Description: "Plaza.", Exits: []string{"s"}})
	plazaID := e.data.CurrentRoom

	_, _, _ = e.ProcessMovement("s")
	_, _, _ = e.ProcessMovement("e")
	_, _, _, _ = e.HandleRoomBlock(RoomBlock{Description: "Market.", Exits: []string{"w"}})

	_, _, _ = e.ProcessMovement("n")
	_, _, loop, _ := e.HandleRoomBlock(RoomBlock{Description: "Plaza.", Exits: []string{"s"}})
	if !loop {
		t.Errorf("expected loop-detected back to plaza")
	}
	if e.data.CurrentRoom != plazaID {
		t.Errorf("current = %s, want plazaID %s", e.data.CurrentRoom, plazaID)
	}
}

func TestPlaceRoomLocalRelaxation(t *testing.T) {
	e := mustCreate(t, "")
	startID := e.GetCurrent().ID

	if err := e.Dig(North, "Northern Room"); err != nil {
		t.Fatalf("Dig N: %v", err)
	}
	if err := e.Goto(startID); err != nil {
		t.Fatalf("Goto start: %v", err)
	}
	if err := e.Dig(NorthEast, "NE Room"); err != nil {
		t.Fatalf("Dig NE: %v", err)
	}
	ne := e.GetCurrent()
	if ne.X != 1 || ne.Y != 1 {
		t.Errorf("NE seed: got (%d,%d), want (1,1)", ne.X, ne.Y)
	}

	if err := e.Goto(startID); err != nil {
		t.Fatalf("Goto start: %v", err)
	}
	if err := e.Dig(West, "West Room"); err != nil {
		t.Fatalf("Dig W: %v", err)
	}
	westID := e.GetCurrent().ID
	if err := e.Dig(NorthEast, "WestNE"); err != nil {
		t.Fatalf("Dig NE from west: %v", err)
	}
	wne := e.GetCurrent()
	if wne.X == 0 && wne.Y == 1 {
		t.Errorf("placement collided with existing room at (0,1)")
	}
	if sw, ok := e.data.Rooms[wne.ID].Exits[SouthWest]; !ok || sw != westID {
		t.Errorf("reverse SW link = %q, %v; want %q, true", sw, ok, westID)
	}
}

func TestProximityGate(t *testing.T) {
	e := mustCreate(t, "")
	e.SetMappingOptions(MappingOptions{Vnum: false, Hash: false})
	e.MarkMXPSeen()
	e.MarkBlockStartSeen()
	e.StartAutoMapping()
	start := e.GetCurrent()
	startID := start.ID
	start.Name = "Start"
	start.Description = "You stand at the meeting point of three old stone paths."

	if _, _, err := e.ProcessMovement("s"); err != nil {
		t.Fatalf("move s: %v", err)
	}
	if ok, _, _, err := e.HandleRoomBlock(RoomBlock{
		Name:        "South Room",
		Description: "A room south of the meeting point.",
		Exits:       []string{"n", "nw"},
	}); err != nil || !ok {
		t.Fatalf("south block processed=%v err=%v", ok, err)
	}

	if _, _, err := e.ProcessMovement("nw"); err != nil {
		t.Fatalf("move nw: %v", err)
	}
	if ok, _, _, err := e.HandleRoomBlock(RoomBlock{
		Name:        "NW Room",
		Description: "A room northwest of the southern room.",
		Exits:       []string{"se", "e"},
	}); err != nil || !ok {
		t.Fatalf("nw block processed=%v err=%v", ok, err)
	}

	if _, _, err := e.ProcessMovement("e"); err != nil {
		t.Fatalf("move e: %v", err)
	}
	_, _, looped, err := e.HandleRoomBlock(RoomBlock{
		Name:        "Start",
		Description: "You stand at the meeting point of three old stone paths.",
		Exits:       []string{"s", "nw", "e"},
	})
	if err != nil {
		t.Fatalf("closing block: %v", err)
	}
	if !looped {
		t.Errorf("expected proximity loop detection")
	}
	if got := e.GetCurrent().ID; got != startID {
		t.Errorf("current room = %q; want start %q", got, startID)
	}
	if n := len(e.data.Rooms); n != 3 {
		t.Errorf("rooms = %d; want 3", n)
	}
}

func TestProximityGateRejectsDifferentExits(t *testing.T) {
	e := mustCreate(t, "")
	e.SetMappingOptions(MappingOptions{Vnum: false, Hash: false})
	e.MarkMXPSeen()
	e.MarkBlockStartSeen()
	e.StartAutoMapping()
	startID := e.GetCurrent().ID

	nearby := &Room{
		ID:          "nearby",
		Name:        "A misty forest",
		Description: "A misty forest with thick trees and a quiet trail ahead.",
		Exits:       map[Direction]string{North: "x", South: "y"},
		X:           1,
		Y:           1,
	}
	e.data.Rooms[nearby.ID] = nearby

	if _, _, err := e.ProcessMovement("ne"); err != nil {
		t.Fatalf("move ne: %v", err)
	}
	_, _, looped, err := e.HandleRoomBlock(RoomBlock{
		Name:        "A misty forest",
		Description: "A misty forest with thick trees and a quiet trail ahead.",
		Exits:       []string{"e", "w"},
	})
	if err != nil {
		t.Fatalf("ne block: %v", err)
	}
	if looped {
		t.Errorf("proximity gate merged rooms with different exit sets")
	}
	if got := e.GetCurrent().ID; got == nearby.ID || got == startID {
		t.Errorf("current room = %q; expected a freshly-created room", got)
	}
}

func TestLoadPreservesDiscoveryCoordinates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.map")
	raw := `{"rooms":{"A":{"id":"A","name":"A","exits":{"e":"B"},"x":7,"y":11,"z":0},"B":{"id":"B","name":"B","exits":{"w":"A"},"x":7,"y":11,"z":0}},"current_room":"A"}`
	if err := os.WriteFile(path, []byte(raw), 0644); err != nil {
		t.Fatal(err)
	}
	e := NewEngine("")
	if err := e.Create(path); err != nil {
		t.Fatal(err)
	}
	if err := e.Save(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var got Map
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	for id, r := range got.Rooms {
		if r.X != 7 || r.Y != 11 {
			t.Fatalf("room %s discovery coordinates changed: %+v", id, r)
		}
	}
	if len(e.undo) != 0 {
		t.Fatal("loading a map created an undo entry")
	}
}

func BenchmarkPlaceRoomDenseHub(b *testing.B) {
	e := NewEngine("")
	if err := e.Create(""); err != nil {
		b.Fatalf("Create: %v", err)
	}
	dirs := []Direction{North, South, East, West, NorthEast, NorthWest, SouthEast, SouthWest}
	startID := e.GetCurrent().ID
	for i := 0; i < 20; i++ {
		_ = e.Goto(startID)
		d := dirs[i%len(dirs)]
		_ = e.Dig(d, fmt.Sprintf("r%d", i))
	}

	fromRoom := e.data.Rooms[startID]
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _, _ = e.placeRoom(fromRoom, North)
	}
}

// ─── Engine / Create ───────────────────────────────────────────────────────

func TestNewEngine(t *testing.T) {
	e := NewEngine("/tmp/test.json")
	if e == nil {
		t.Fatal("NewEngine returned nil")
	}
	if len(e.data.Rooms) != 0 {
		t.Fatalf("expected 0 rooms, got %d", len(e.data.Rooms))
	}
	if len(e.paths) != len(DefaultPaths()) {
		t.Fatalf("expected %d default paths, got %d", len(DefaultPaths()), len(e.paths))
	}
}

func TestCreateFresh(t *testing.T) {
	e := mustCreate(t, "")
	if len(e.data.Rooms) != 1 {
		t.Fatalf("expected 1 room after Create, got %d", len(e.data.Rooms))
	}
	curr := e.GetCurrent()
	if curr == nil {
		t.Fatal("expected current room after Create")
	}
	if curr.Name != "Start" {
		t.Fatalf("expected Start room, got %s", curr.Name)
	}
}

func TestCreateLoadExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "testmap.json")

	// Create and populate a map, then save it
	e1 := mustCreate(t, path)
	if err := e1.Dig(North, "North Room"); err != nil {
		t.Fatalf("Dig failed: %v", err)
	}
	// Set a description so rebuildHashIndex has something to index
	e1.SetDescription("A well-described room")
	if err := e1.Save(); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// Load it into a new engine
	e2 := mustCreate(t, path)
	if len(e2.data.Rooms) != 2 {
		t.Fatalf("expected 2 rooms after load, got %d", len(e2.data.Rooms))
	}
	// rebuildHashIndex should have run
	if len(e2.hashIndex) == 0 {
		t.Fatal("expected hashIndex rebuilt after load")
	}
}

func TestCreateLoadInvalidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(path, []byte("not json"), 0644); err != nil {
		t.Fatal(err)
	}
	e := NewEngine("")
	if err := e.Create(path); err == nil {
		t.Fatal("expected error loading invalid JSON")
	}
}

// ─── Paths ─────────────────────────────────────────────────────────────────

func TestSetPathsGetPaths(t *testing.T) {
	e := newEngine(t)
	custom := []Direction{North, South, East, West}
	e.SetPaths(custom)
	got := e.GetPaths()
	if len(got) != len(custom) {
		t.Fatalf("expected %d paths, got %d", len(custom), len(got))
	}
	for i, d := range custom {
		if got[i] != d {
			t.Fatalf("expected path %d = %s, got %s", i, d, got[i])
		}
	}
}

func TestIsValidDirection(t *testing.T) {
	e := newEngine(t)
	if !e.IsValidDirection(North) {
		t.Fatal("expected North to be valid")
	}
	// Restrict paths and retest
	e.SetPaths([]Direction{North, South})
	if e.IsValidDirection(East) {
		t.Fatal("expected East to be invalid after restricting paths")
	}
}

// ─── Dig ───────────────────────────────────────────────────────────────────

func TestDigHappyPath(t *testing.T) {
	e := mustCreate(t, "")
	start := e.GetCurrent()
	if start == nil {
		t.Fatal("no start room")
	}

	if err := e.Dig(North, "North Room"); err != nil {
		t.Fatalf("Dig failed: %v", err)
	}

	// Should have 2 rooms
	if len(e.data.Rooms) != 2 {
		t.Fatalf("expected 2 rooms, got %d", len(e.data.Rooms))
	}

	// Current room should be the new one
	curr := e.GetCurrent()
	if curr == nil || curr.Name != "North Room" {
		t.Fatalf("expected current room = North Room, got %+v", curr)
	}

	// Start room should have exit North
	start = e.data.Rooms[start.ID]
	if start == nil {
		t.Fatal("start room disappeared")
	}
	if start.Exits[North] == "" {
		t.Fatal("expected start room to have North exit")
	}

	// New room should have reverse exit South
	newRoomID := start.Exits[North]
	newRoom := e.data.Rooms[newRoomID]
	if newRoom == nil {
		t.Fatal("new room not found")
	}
	if newRoom.Exits[South] != start.ID {
		t.Fatalf("expected new room to have South exit back to start")
	}
}

func TestDigInvalidDirection(t *testing.T) {
	e := mustCreate(t, "")
	e.SetPaths([]Direction{North, South})
	if err := e.Dig(East, "East Room"); err == nil {
		t.Fatal("expected error for invalid direction")
	}
}

func TestDigDuplicateExit(t *testing.T) {
	e := mustCreate(t, "")
	start := e.GetCurrent()
	if err := e.Dig(North, "North Room"); err != nil {
		t.Fatalf("first Dig failed: %v", err)
	}
	// Go back to start room; it already has a North exit
	_ = e.Goto(start.ID)
	if err := e.Dig(North, "Another North"); err == nil {
		t.Fatal("expected error for duplicate exit")
	}
}

func TestDigNoCurrentRoom(t *testing.T) {
	e := NewEngine("")
	// No Create called — no rooms, no current room
	if err := e.Dig(North, "Room"); err == nil {
		t.Fatal("expected error when no current room exists")
	}
}

// ─── Link ──────────────────────────────────────────────────────────────────

func TestLinkHappyPath(t *testing.T) {
	e := mustCreate(t, "")
	start := e.GetCurrent()
	if err := e.Dig(North, "North Room"); err != nil {
		t.Fatal(err)
	}
	northRoom := e.GetCurrent()

	// Go back to start
	_ = e.Goto(start.ID)

	// Link start East to northRoom
	if err := e.Link(East, northRoom.ID); err != nil {
		t.Fatalf("Link failed: %v", err)
	}

	start = e.data.Rooms[start.ID]
	if start.Exits[East] != northRoom.ID {
		t.Fatal("expected East exit from start to north room")
	}
	// Link is one-way; northRoom should NOT have West back
	northRoom = e.data.Rooms[northRoom.ID]
	if northRoom.Exits[West] != "" {
		t.Fatal("expected one-way link, north room should not have West exit")
	}
}

func TestLinkInvalidDirection(t *testing.T) {
	e := mustCreate(t, "")
	if err := e.Link(East, "nonexistent"); err == nil {
		t.Fatal("expected error for invalid direction")
	}
}

func TestLinkTargetNotFound(t *testing.T) {
	e := mustCreate(t, "")
	if err := e.Link(North, "nonexistent"); err == nil {
		t.Fatal("expected error for missing target")
	}
}

func TestLinkSelf(t *testing.T) {
	e := mustCreate(t, "")
	start := e.GetCurrent()
	if err := e.Link(North, start.ID); err == nil {
		t.Fatal("expected error linking room to itself")
	}
}

// ─── Delete ────────────────────────────────────────────────────────────────

func TestDeleteByNameOrID(t *testing.T) {
	e := mustCreate(t, "")
	start := e.GetCurrent()
	if err := e.Dig(North, "North Room"); err != nil {
		t.Fatal(err)
	}
	northRoom := e.GetCurrent()

	// Delete by partial ID
	if err := e.DeleteByNameOrID(northRoom.ID[:8]); err != nil {
		t.Fatalf("DeleteByNameOrID failed: %v", err)
	}

	if len(e.data.Rooms) != 1 {
		t.Fatalf("expected 1 room after delete, got %d", len(e.data.Rooms))
	}
	// Start room should no longer have North exit
	start = e.data.Rooms[start.ID]
	if start.Exits[North] != "" {
		t.Fatal("expected North exit removed from start room")
	}
}

func TestDeleteByNameOrIDNotFound(t *testing.T) {
	e := mustCreate(t, "")
	if err := e.DeleteByNameOrID("nonexistent"); err == nil {
		t.Fatal("expected error for missing room")
	}
}

func TestDeleteRoom(t *testing.T) {
	e := mustCreate(t, "")
	if err := e.Dig(North, "North Room"); err != nil {
		t.Fatal(err)
	}
	northRoom := e.GetCurrent()

	if err := e.DeleteRoom(northRoom.ID); err != nil {
		t.Fatalf("DeleteRoom failed: %v", err)
	}
	if len(e.data.Rooms) != 1 {
		t.Fatalf("expected 1 room, got %d", len(e.data.Rooms))
	}
}

// ─── Undo ──────────────────────────────────────────────────────────────────

func TestUndoAfterDig(t *testing.T) {
	e := mustCreate(t, "")
	start := e.GetCurrent()
	if err := e.Dig(North, "North Room"); err != nil {
		t.Fatal(err)
	}
	if len(e.data.Rooms) != 2 {
		t.Fatal("expected 2 rooms before undo")
	}

	if err := e.Undo(); err != nil {
		t.Fatalf("Undo failed: %v", err)
	}

	if len(e.data.Rooms) != 1 {
		t.Fatalf("expected 1 room after undo, got %d", len(e.data.Rooms))
	}
	if e.GetCurrent().ID != start.ID {
		t.Fatal("expected current room restored to start")
	}
}

func TestUndoAfterLink(t *testing.T) {
	e := mustCreate(t, "")
	if err := e.Dig(North, "North Room"); err != nil {
		t.Fatal(err)
	}
	northRoom := e.GetCurrent()
	_ = e.Goto(e.data.Rooms[northRoom.ID].Exits[South]) // back to start

	if err := e.Link(East, northRoom.ID); err != nil {
		t.Fatal(err)
	}
	start := e.GetCurrent()
	if start.Exits[East] == "" {
		t.Fatal("expected East exit before undo")
	}

	if err := e.Undo(); err != nil {
		t.Fatalf("Undo failed: %v", err)
	}
	start = e.GetCurrent()
	if start.Exits[East] != "" {
		t.Fatal("expected East exit removed after undo")
	}
}

func TestUndoAfterDelete(t *testing.T) {
	e := mustCreate(t, "")
	if err := e.Dig(North, "North Room"); err != nil {
		t.Fatal(err)
	}
	northRoom := e.GetCurrent()
	if err := e.DeleteByNameOrID(northRoom.ID); err != nil {
		t.Fatal(err)
	}
	if len(e.data.Rooms) != 1 {
		t.Fatal("expected 1 room after delete")
	}

	if err := e.Undo(); err != nil {
		t.Fatalf("Undo failed: %v", err)
	}
	if len(e.data.Rooms) != 2 {
		t.Fatalf("expected 2 rooms after undo, got %d", len(e.data.Rooms))
	}
}

func TestUndoEmptyStack(t *testing.T) {
	e := mustCreate(t, "")
	if err := e.Undo(); err == nil {
		t.Fatal("expected error undoing with empty stack")
	}
}

// ─── Goto / Search / SetName / SetDescription ──────────────────────────────

func TestGoto(t *testing.T) {
	e := mustCreate(t, "")
	start := e.GetCurrent()
	if err := e.Dig(North, "North Room"); err != nil {
		t.Fatal(err)
	}
	northRoom := e.GetCurrent()

	// By ID
	if err := e.Goto(start.ID); err != nil {
		t.Fatalf("Goto by ID failed: %v", err)
	}
	if e.GetCurrent().ID != start.ID {
		t.Fatal("expected current room = start")
	}

	// By partial ID
	if err := e.Goto(northRoom.ID[:8]); err != nil {
		t.Fatalf("Goto by partial ID failed: %v", err)
	}
	if e.GetCurrent().ID != northRoom.ID {
		t.Fatal("expected current room = northRoom")
	}

	// By name
	if err := e.Goto("Start"); err != nil {
		t.Fatalf("Goto by name failed: %v", err)
	}
	if e.GetCurrent().ID != start.ID {
		t.Fatal("expected current room = start")
	}

	// By partial name
	if err := e.Goto("North"); err != nil {
		t.Fatalf("Goto by partial name failed: %v", err)
	}
	if e.GetCurrent().ID != northRoom.ID {
		t.Fatal("expected current room = northRoom")
	}
}

func TestGotoNotFound(t *testing.T) {
	e := mustCreate(t, "")
	if err := e.Goto("nonexistent"); err == nil {
		t.Fatal("expected error for missing room")
	}
}

func TestSearch(t *testing.T) {
	e := mustCreate(t, "")
	if err := e.Dig(North, "North Room"); err != nil {
		t.Fatal(err)
	}
	results := e.Search("North")
	if len(results) != 1 {
		t.Fatalf("expected 1 search result, got %d", len(results))
	}
	if !strings.Contains(results[0], "North Room") {
		t.Fatalf("expected result to contain 'North Room', got %s", results[0])
	}
}

func TestSearchEmpty(t *testing.T) {
	e := mustCreate(t, "")
	results := e.Search("xyznonexistent")
	if len(results) != 0 {
		t.Fatalf("expected 0 results, got %d", len(results))
	}
}

func TestSetName(t *testing.T) {
	e := mustCreate(t, "")
	e.SetName("Renamed Start")
	if e.GetCurrent().Name != "Renamed Start" {
		t.Fatalf("expected name 'Renamed Start', got %s", e.GetCurrent().Name)
	}
}

func TestSetDescription(t *testing.T) {
	e := mustCreate(t, "")
	e.SetDescription("A dark cave")
	curr := e.GetCurrent()
	if curr.Description != "A dark cave" {
		t.Fatalf("expected description 'A dark cave', got %s", curr.Description)
	}
	if curr.DescriptionHash == "" {
		t.Fatal("expected DescriptionHash to be set")
	}
}

// ─── Auto-mapping State ────────────────────────────────────────────────────

func TestIsCreated(t *testing.T) {
	e := newEngine(t)
	if e.IsCreated() {
		t.Fatal("expected IsCreated false on fresh engine with empty path")
	}
	tmp := t.TempDir() + "/m.json"
	if err := e.Create(tmp); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if !e.IsCreated() {
		t.Fatal("expected IsCreated true after Create")
	}
}

func TestAutoMappingState(t *testing.T) {
	e := newEngine(t)
	if e.IsAutoMapping() {
		t.Fatal("expected auto-mapping off by default")
	}
	e.StartAutoMapping()
	e.MarkMXPSeen()
	e.MarkBlockStartSeen()
	if !e.IsAutoMapping() {
		t.Fatal("expected auto-mapping on after Start")
	}
	e.StopAutoMapping()
	if e.IsAutoMapping() {
		t.Fatal("expected auto-mapping off after Stop")
	}
}

func TestHasPendingMovement(t *testing.T) {
	e := mustCreate(t, "")
	e.StartAutoMapping()
	e.MarkMXPSeen()
	e.MarkBlockStartSeen()
	if e.HasPendingMovement() {
		t.Fatal("expected no pending movement initially")
	}
	_, _, _ = e.ProcessMovement("n")
	if !e.HasPendingMovement() {
		t.Fatal("expected pending movement after ProcessMovement")
	}
	e.CancelPendingMovement()
	if e.HasPendingMovement() {
		t.Fatal("expected no pending movement after cancel")
	}
}

// ─── ProcessMovement ───────────────────────────────────────────────────────

func TestProcessMovementNotAutoMapping(t *testing.T) {
	e := mustCreate(t, "")
	// auto-mapping is off
	processed, _, _ := e.ProcessMovement("n")
	if processed {
		t.Fatal("expected no processing when auto-mapping is off")
	}
}

func TestProcessMovementExistingExit(t *testing.T) {
	e := mustCreate(t, "")
	e.StartAutoMapping()
	e.MarkMXPSeen()
	e.MarkBlockStartSeen()
	if err := e.Dig(North, "North Room"); err != nil {
		t.Fatal(err)
	}
	northRoom := e.GetCurrent()
	_ = e.Goto(northRoom.Exits[South]) // back to start

	processed, roomName, err := e.ProcessMovement("n")
	if err != nil {
		t.Fatalf("ProcessMovement failed: %v", err)
	}
	if !processed {
		t.Fatal("expected movement to be processed")
	}
	if roomName != "North Room" {
		t.Fatalf("expected roomName 'North Room', got %s", roomName)
	}
	if e.HasPendingMovement() {
		t.Fatal("expected no pending movement when exit exists")
	}
}

func TestProcessMovementPending(t *testing.T) {
	e := mustCreate(t, "")
	e.StartAutoMapping()
	e.MarkMXPSeen()
	e.MarkBlockStartSeen()
	processed, roomName, err := e.ProcessMovement("n")
	if err != nil {
		t.Fatalf("ProcessMovement failed: %v", err)
	}
	if !processed {
		t.Fatal("expected movement to be processed")
	}
	if roomName != "[pending room data]" {
		t.Fatalf("expected pending marker, got %s", roomName)
	}
	if !e.HasPendingMovement() {
		t.Fatal("expected pending movement after unknown direction")
	}
}

func TestProcessMovementInvalidDirection(t *testing.T) {
	e := mustCreate(t, "")
	e.StartAutoMapping()
	e.MarkMXPSeen()
	e.MarkBlockStartSeen()
	processed, _, _ := e.ProcessMovement("xyz")
	if processed {
		t.Fatal("expected no processing for invalid direction")
	}
}

// ─── ProcessRoomData ───────────────────────────────────────────────────────

func TestProcessRoomDataNewRoom(t *testing.T) {
	e := mustCreate(t, "")
	e.StartAutoMapping()
	e.MarkMXPSeen()
	e.MarkBlockStartSeen()
	_, _, _ = e.ProcessMovement("n")

	text := "\tA sunny meadow\nObvious exits are north, south, east and west."
	processed, roomName, loopDetected, err := e.ProcessRoomData(text)
	if err != nil {
		t.Fatalf("ProcessRoomData failed: %v", err)
	}
	if !processed {
		t.Fatal("expected room data to be processed")
	}
	if loopDetected {
		t.Fatal("expected no loop detection for new room")
	}
	if roomName != "New Room" {
		t.Fatalf("expected 'New Room', got %s", roomName)
	}
	if e.HasPendingMovement() {
		t.Fatal("expected pending cleared after room data")
	}
	// Should now have 2 rooms
	if len(e.data.Rooms) != 2 {
		t.Fatalf("expected 2 rooms, got %d", len(e.data.Rooms))
	}
}

func TestProcessRoomDataLoopDetection(t *testing.T) {
	e := mustCreate(t, "")
	e.SetMappingOptions(MappingOptions{Vnum: true, Hash: true})
	e.StartAutoMapping()
	e.MarkMXPSeen()
	e.MarkBlockStartSeen()
	// Create a room north of start via auto-mapping
	_, _, _ = e.ProcessMovement("n")
	text := "\tA sunny meadow\nObvious exits are north, south, east and west."
	_, _, _, _ = e.ProcessRoomData(text)
	northRoom := e.GetCurrent()
	start := e.data.Rooms[northRoom.Exits[South]]

	// Approach the SAME room from the EAST (no exit yet) with identical data
	_ = e.Goto(start.ID)
	_, _, _ = e.ProcessMovement("e")
	processed, roomName, loopDetected, err := e.ProcessRoomData(text)
	if err != nil {
		t.Fatalf("ProcessRoomData failed: %v", err)
	}
	if !processed {
		t.Fatal("expected room data to be processed")
	}
	if !loopDetected {
		t.Fatal("expected loop detection")
	}
	if roomName != "New Room" {
		t.Fatalf("expected 'New Room', got %s", roomName)
	}
	// Should still have 2 rooms (no new room created)
	if len(e.data.Rooms) != 2 {
		t.Fatalf("expected 2 rooms (loop), got %d", len(e.data.Rooms))
	}
	// Start room should now have East exit linking to the north room
	start = e.data.Rooms[start.ID]
	if start.Exits[East] != northRoom.ID {
		t.Fatal("expected start room East exit to link to existing room")
	}
}

func TestProcessRoomData_HashOff_DoesNotUseHashWithoutProximity(t *testing.T) {
	e := mustCreate(t, "")
	e.SetMappingOptions(MappingOptions{Vnum: false, Hash: false})
	e.StartAutoMapping()
	e.MarkMXPSeen()
	e.MarkBlockStartSeen()

	sample := "\tA grey room.\nObvious exits: south.\n"
	differentExits := "\tA grey room.\nObvious exits: west.\n"
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
	_, _, loop, err := e.ProcessRoomData(differentExits)
	if err != nil {
		t.Fatalf("ProcessRoomData (2): %v", err)
	}
	if loop {
		t.Errorf("loopDetected = true with Hash=false; want false")
	}
	if len(e.hashIndex) != 0 {
		t.Errorf("hashIndex size = %d, want 0 with Hash=false", len(e.hashIndex))
	}
}

func TestProcessRoomData_HashOn_PreservesLegacyBehavior(t *testing.T) {
	e := mustCreate(t, "")
	e.SetMappingOptions(MappingOptions{Vnum: false, Hash: true})
	e.StartAutoMapping()
	e.MarkMXPSeen()
	e.MarkBlockStartSeen()

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
	_, _, loop, err := e.ProcessRoomData(sample)
	if err != nil {
		t.Fatalf("ProcessRoomData (2): %v", err)
	}
	if !loop {
		t.Errorf("loopDetected = false with Hash=true; want true")
	}
}

func TestCreateRoom_HashOff_NoIndexEntry(t *testing.T) {
	e := mustCreate(t, "")
	e.SetMappingOptions(MappingOptions{Vnum: false, Hash: false})
	e.StartAutoMapping()
	e.MarkMXPSeen()
	e.MarkBlockStartSeen()

	if _, _, err := e.ProcessMovement("n"); err != nil {
		t.Fatalf("ProcessMovement: %v", err)
	}
	if _, _, _, err := e.ProcessRoomData("\tA room.\nObvious exits: south.\n"); err != nil {
		t.Fatalf("ProcessRoomData: %v", err)
	}

	if _, hasEmpty := e.hashIndex[""]; hasEmpty {
		t.Errorf("hashIndex contains empty-string key; should not pollute index when Hash=false")
	}
	if len(e.hashIndex) != 0 {
		t.Errorf("hashIndex size = %d; want 0 when Hash=false", len(e.hashIndex))
	}
}

func TestProcessRoomDataNoPending(t *testing.T) {
	e := mustCreate(t, "")
	e.StartAutoMapping()
	e.MarkMXPSeen()
	e.MarkBlockStartSeen()
	text := "\tA dark cave\nObvious exits are north and east."
	processed, _, _, _ := e.ProcessRoomData(text)
	if processed {
		t.Fatal("expected no processing when no pending movement")
	}
	// Current room description should be updated
	curr := e.GetCurrent()
	if curr.Description == "" {
		t.Fatal("expected current room description updated")
	}
}

func TestProcessRoomDataNotAutoMapping(t *testing.T) {
	e := mustCreate(t, "")
	// auto-mapping off
	processed, _, _, _ := e.ProcessRoomData("some text")
	if processed {
		t.Fatal("expected no processing when auto-mapping is off")
	}
}

// ─── HandleGMCPRoomInfo ────────────────────────────────────────────────────

func TestHandleGMCPRoomInfoNewRoom(t *testing.T) {
	e := mustCreate(t, "")
	e.StartAutoMapping()
	e.MarkMXPSeen()
	e.MarkBlockStartSeen()
	_, _, _ = e.ProcessMovement("n")

	room := GMCPRoom{
		Vnum:        "room-123",
		Name:        "GMCP Room",
		Description: "A room from GMCP",
		Exits:       []string{"n", "s", "e", "w"},
	}
	processed, roomName, loopDetected, err := e.HandleGMCPRoomInfo(room)
	if err != nil {
		t.Fatalf("HandleGMCPRoomInfo failed: %v", err)
	}
	if !processed {
		t.Fatal("expected GMCP room to be processed")
	}
	if loopDetected {
		t.Fatal("expected no loop for new room")
	}
	if roomName != "GMCP Room" {
		t.Fatalf("expected 'GMCP Room', got %s", roomName)
	}
	// Room should use vnum as ID
	if _, ok := e.data.Rooms["room-123"]; !ok {
		t.Fatal("expected room with vnum ID to exist")
	}
}

func TestHandleGMCPRoomInfoVnumLoop(t *testing.T) {
	e := mustCreate(t, "")
	e.StartAutoMapping()
	e.MarkMXPSeen()
	e.MarkBlockStartSeen()
	// Create a room with a known vnum via GMCP north of start
	_, _, _ = e.ProcessMovement("n")
	room := GMCPRoom{
		Vnum:        "room-123",
		Name:        "First Room",
		Description: "Desc",
		Exits:       []string{"s"},
	}
	_, _, _, _ = e.HandleGMCPRoomInfo(room)
	firstRoom := e.GetCurrent()
	start := e.data.Rooms[firstRoom.Exits[South]]

	// Approach the SAME room from the EAST (no exit yet) with same vnum
	_ = e.Goto(start.ID)
	_, _, _ = e.ProcessMovement("e")
	room2 := GMCPRoom{
		Vnum:        "room-123",
		Name:        "First Room",
		Description: "Desc",
		Exits:       []string{"s"},
	}
	processed, _, loopDetected, err := e.HandleGMCPRoomInfo(room2)
	if err != nil {
		t.Fatalf("HandleGMCPRoomInfo failed: %v", err)
	}
	if !processed {
		t.Fatal("expected processing")
	}
	if !loopDetected {
		t.Fatal("expected vnum loop detection")
	}
	// Start should now have East exit to firstRoom
	start = e.data.Rooms[start.ID]
	if start.Exits[East] != firstRoom.ID {
		t.Fatal("expected start room East exit to link to existing room")
	}
}

func TestHandleGMCPRoomInfoHashLoop(t *testing.T) {
	e := mustCreate(t, "")
	e.SetMappingOptions(MappingOptions{Vnum: true, Hash: true})
	e.StartAutoMapping()
	e.MarkMXPSeen()
	e.MarkBlockStartSeen()
	// Create room north of start via GMCP with no vnum
	_, _, _ = e.ProcessMovement("n")
	room1 := GMCPRoom{
		Vnum:        "",
		Name:        "Mystical Forest",
		Description: "A mystical forest",
		Exits:       []string{"north", "south"},
	}
	_, _, _, _ = e.HandleGMCPRoomInfo(room1)
	firstRoom := e.GetCurrent()
	start := e.data.Rooms[firstRoom.Exits[South]]

	// Approach the SAME room from the EAST (no exit yet) via GMCP with no vnum
	_ = e.Goto(start.ID)
	_, _, _ = e.ProcessMovement("e")
	room2 := GMCPRoom{
		Vnum:        "",
		Name:        "Mystical Forest",
		Description: "A mystical forest",
		Exits:       []string{"north", "south"},
	}
	processed, _, loopDetected, err := e.HandleGMCPRoomInfo(room2)
	if err != nil {
		t.Fatalf("HandleGMCPRoomInfo failed: %v", err)
	}
	if !processed {
		t.Fatal("expected processing")
	}
	if !loopDetected {
		t.Fatal("expected hash-based loop detection")
	}
	// Start should now have East exit to firstRoom
	start = e.data.Rooms[start.ID]
	if start.Exits[East] != firstRoom.ID {
		t.Fatal("expected start room East exit to link to existing room")
	}
}

func TestHandleGMCPRoomInfo_VnumOff_DoesNotCollapse(t *testing.T) {
	e := mustCreate(t, "")
	e.SetMappingOptions(MappingOptions{Vnum: false, Hash: false})
	e.StartAutoMapping()
	e.MarkMXPSeen()
	e.MarkBlockStartSeen()

	if _, _, err := e.ProcessMovement("n"); err != nil {
		t.Fatalf("ProcessMovement: %v", err)
	}
	if _, _, _, err := e.HandleGMCPRoomInfo(GMCPRoom{Vnum: "100", Name: "Plaza", Description: "A plaza.", Exits: []string{"s"}}); err != nil {
		t.Fatalf("HandleGMCPRoomInfo: %v", err)
	}
	if _, _, err := e.ProcessMovement("s"); err != nil {
		t.Fatalf("ProcessMovement: %v", err)
	}
	if _, _, err := e.ProcessMovement("e"); err != nil {
		t.Fatalf("ProcessMovement: %v", err)
	}
	_, _, loop, err := e.HandleGMCPRoomInfo(GMCPRoom{Vnum: "100", Name: "Plaza", Description: "A plaza.", Exits: []string{"w"}})
	if err != nil {
		t.Fatalf("HandleGMCPRoomInfo (2nd): %v", err)
	}
	if loop {
		t.Errorf("loopDetected = true with Vnum=false, want false")
	}
}

func TestHandleGMCPRoomInfo_VnumOn_CollapsesByVnum(t *testing.T) {
	e := mustCreate(t, "")
	e.SetMappingOptions(MappingOptions{Vnum: true, Hash: false})
	e.StartAutoMapping()
	e.MarkMXPSeen()
	e.MarkBlockStartSeen()

	if _, _, err := e.ProcessMovement("n"); err != nil {
		t.Fatalf("ProcessMovement: %v", err)
	}
	if _, _, _, err := e.HandleGMCPRoomInfo(GMCPRoom{Vnum: "100", Name: "Plaza", Description: "A plaza.", Exits: []string{"s"}}); err != nil {
		t.Fatalf("HandleGMCPRoomInfo: %v", err)
	}
	if _, _, err := e.ProcessMovement("s"); err != nil {
		t.Fatalf("ProcessMovement: %v", err)
	}
	if _, _, err := e.ProcessMovement("e"); err != nil {
		t.Fatalf("ProcessMovement: %v", err)
	}
	_, _, loop, err := e.HandleGMCPRoomInfo(GMCPRoom{Vnum: "100", Name: "Plaza", Description: "A plaza.", Exits: []string{"w"}})
	if err != nil {
		t.Fatalf("HandleGMCPRoomInfo (2nd): %v", err)
	}
	if !loop {
		t.Errorf("loopDetected = false with Vnum=true, want true")
	}
}

func TestHandleGMCPRoomInfo_HashOff_DoesNotPopulateIndex(t *testing.T) {
	e := mustCreate(t, "")
	e.SetMappingOptions(MappingOptions{Vnum: false, Hash: false})
	e.StartAutoMapping()
	e.MarkMXPSeen()
	e.MarkBlockStartSeen()

	if _, _, err := e.ProcessMovement("n"); err != nil {
		t.Fatalf("ProcessMovement: %v", err)
	}
	if _, _, _, err := e.HandleGMCPRoomInfo(GMCPRoom{Name: "X", Description: "desc", Exits: []string{"s"}}); err != nil {
		t.Fatalf("HandleGMCPRoomInfo: %v", err)
	}
	if len(e.hashIndex) != 0 {
		t.Errorf("hashIndex size = %d after Hash=false, want 0", len(e.hashIndex))
	}
}

func TestHandleGMCPRoomInfo_EmptyName_UsesIncomingLatch(t *testing.T) {
	e := mustCreate(t, "")
	e.SetMappingOptions(MappingOptions{Vnum: true, Hash: false})
	e.StartAutoMapping()
	e.MarkMXPSeen()
	e.MarkBlockStartSeen()

	if _, _, err := e.ProcessMovement("n"); err != nil {
		t.Fatalf("ProcessMovement: %v", err)
	}
	e.SetIncomingRoomName("MXP Plaza")

	if _, _, _, err := e.HandleGMCPRoomInfo(GMCPRoom{Vnum: "100", Description: "desc", Exits: []string{"s"}}); err != nil {
		t.Fatalf("HandleGMCPRoomInfo: %v", err)
	}
	if got := e.GetCurrent().Name; got != "MXP Plaza" {
		t.Errorf("current room name = %q, want %q", got, "MXP Plaza")
	}
}

func TestSetIncomingRoomName_PendingMovement_NamesNewRoom(t *testing.T) {
	e := mustCreate(t, "")
	e.SetMappingOptions(MappingOptions{Vnum: false, Hash: false})
	e.StartAutoMapping()
	e.MarkMXPSeen()
	e.MarkBlockStartSeen()

	if _, _, err := e.ProcessMovement("n"); err != nil {
		t.Fatalf("ProcessMovement: %v", err)
	}
	e.SetIncomingRoomName("Town Plaza")

	if _, _, _, err := e.ProcessRoomData("\tA plaza.\nObvious exits: south.\n"); err != nil {
		t.Fatalf("ProcessRoomData: %v", err)
	}
	if got := e.GetCurrent().Name; got != "Town Plaza" {
		t.Errorf("current room name = %q, want %q", got, "Town Plaza")
	}
}

func TestSetIncomingRoomName_NoPending_RenamesCurrent(t *testing.T) {
	e := mustCreate(t, "")
	e.SetIncomingRoomName("Override")
	if got := e.GetCurrent().Name; got != "Override" {
		t.Errorf("current name = %q, want %q", got, "Override")
	}
}

func TestSetIncomingRoomName_LastWriteWins(t *testing.T) {
	e := mustCreate(t, "")
	e.SetMappingOptions(MappingOptions{Vnum: false, Hash: false})
	e.StartAutoMapping()
	e.MarkMXPSeen()
	e.MarkBlockStartSeen()

	if _, _, err := e.ProcessMovement("n"); err != nil {
		t.Fatalf("ProcessMovement: %v", err)
	}
	e.SetIncomingRoomName("First")
	e.SetIncomingRoomName("Second")
	if _, _, _, err := e.ProcessRoomData("\tdesc\nObvious exits: south.\n"); err != nil {
		t.Fatalf("ProcessRoomData: %v", err)
	}
	if got := e.GetCurrent().Name; got != "Second" {
		t.Errorf("name = %q, want %q", got, "Second")
	}
}

func TestHandleGMCPRoomInfoNoPending(t *testing.T) {
	e := mustCreate(t, "")
	e.StartAutoMapping()
	e.MarkMXPSeen()
	e.MarkBlockStartSeen()
	room := GMCPRoom{
		Vnum:        "room-456",
		Name:        "Update Room",
		Description: "Updated desc",
		Exits:       []string{"n"},
	}
	processed, _, _, _ := e.HandleGMCPRoomInfo(room)
	if processed {
		t.Fatal("expected no processing when no pending movement")
	}
	// Current room should be updated
	curr := e.GetCurrent()
	if curr.Name != "Update Room" {
		t.Fatalf("expected room name updated to 'Update Room', got %s", curr.Name)
	}
}

func TestHandleGMCPRoomInfoNotAutoMapping(t *testing.T) {
	e := mustCreate(t, "")
	// auto-mapping off
	room := GMCPRoom{Vnum: "r1", Name: "Room"}
	processed, _, _, _ := e.HandleGMCPRoomInfo(room)
	if processed {
		t.Fatal("expected no processing when auto-mapping is off")
	}
}

// ─── Move ──────────────────────────────────────────────────────────────────

func TestMove(t *testing.T) {
	e := mustCreate(t, "")
	if err := e.Dig(North, "North Room"); err != nil {
		t.Fatal(err)
	}
	northRoom := e.GetCurrent()
	_ = e.Goto(northRoom.Exits[South]) // back to start

	if err := e.Move(North); err != nil {
		t.Fatalf("Move failed: %v", err)
	}
	if e.GetCurrent().ID != northRoom.ID {
		t.Fatal("expected to be in north room")
	}
}

func TestMoveNoExit(t *testing.T) {
	e := mustCreate(t, "")
	if err := e.Move(North); err == nil {
		t.Fatal("expected error moving through nonexistent exit")
	}
}

// ─── Save ──────────────────────────────────────────────────────────────────

func TestSave(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "map.json")
	e := mustCreate(t, path)
	if err := e.Dig(North, "North Room"); err != nil {
		t.Fatal(err)
	}

	if err := e.Save(); err != nil {
		t.Fatalf("Save failed: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read saved file: %v", err)
	}
	if !strings.Contains(string(data), "North Room") {
		t.Fatal("expected saved file to contain 'North Room'")
	}
}

func TestSaveNoPath(t *testing.T) {
	e := mustCreate(t, "")
	if err := e.Save(); err == nil {
		t.Fatal("expected error when no path set")
	}
}

// ─── Show ──────────────────────────────────────────────────────────────────

func TestShow(t *testing.T) {
	e := mustCreate(t, "")
	if err := e.Dig(North, "North Room"); err != nil {
		t.Fatal(err)
	}
	output := e.Show(1)
	if output == "" {
		t.Fatal("expected non-empty Show output")
	}
	if !strings.Contains(output, "flowchart") {
		t.Fatal("expected Mermaid flowchart output")
	}
}

func TestShowAll(t *testing.T) {
	e := mustCreate(t, "")
	if err := e.Dig(North, "North Room"); err != nil {
		t.Fatal(err)
	}
	output := e.Show(-1)
	if !strings.Contains(output, "North Room") {
		t.Fatal("expected Show(-1) to contain 'North Room'")
	}
}

func TestShowNoMap(t *testing.T) {
	e := NewEngine("")
	output := e.Show(1)
	if output != "No map." {
		t.Fatalf("expected 'No map.', got %s", output)
	}
}

// ─── Utility Functions ─────────────────────────────────────────────────────

func TestParseDirection(t *testing.T) {
	tests := []struct {
		input    string
		want     Direction
		wantBool bool
	}{
		{"n", North, true},
		{"north", North, true},
		{"N", North, true},
		{"ne", NorthEast, true},
		{"northeast", NorthEast, true},
		{"u", Up, true},
		{"up", Up, true},
		{"in", In, true},
		{"out", Out, true},
		{"xyz", "", false},
	}
	for _, tc := range tests {
		d, ok := ParseDirection(tc.input)
		if ok != tc.wantBool {
			t.Fatalf("ParseDirection(%q) ok=%v, want %v", tc.input, ok, tc.wantBool)
		}
		if ok && d != tc.want {
			t.Fatalf("ParseDirection(%q) = %v, want %v", tc.input, d, tc.want)
		}
	}
}

func TestReverseDirection(t *testing.T) {
	tests := []struct {
		dir  Direction
		want Direction
	}{
		{North, South},
		{South, North},
		{East, West},
		{West, East},
		{NorthEast, SouthWest},
		{SouthWest, NorthEast},
		{NorthWest, SouthEast},
		{SouthEast, NorthWest},
		{Up, Down},
		{Down, Up},
		{In, Out},
		{Out, In},
		{"xyz", ""},
	}
	for _, tc := range tests {
		got := ReverseDirection(tc.dir)
		if got != tc.want {
			t.Fatalf("ReverseDirection(%s) = %s, want %s", tc.dir, got, tc.want)
		}
	}
}

func TestHashDescription(t *testing.T) {
	h1 := HashDescription("hello")
	h2 := HashDescription("hello")
	h3 := HashDescription("world")
	if h1 != h2 {
		t.Fatal("expected same hash for same input")
	}
	if h1 == h3 {
		t.Fatal("expected different hash for different input")
	}
	if len(h1) != 64 {
		t.Fatalf("expected SHA256 hex = 64 chars, got %d", len(h1))
	}
}

func TestComputeRoomHash(t *testing.T) {
	h1 := ComputeRoomHash("desc", []string{"n", "s"})
	h2 := ComputeRoomHash("desc", []string{"s", "n"})
	if h1 != h2 {
		t.Fatal("expected same hash regardless of exit order")
	}
	h3 := ComputeRoomHash("desc", []string{"n", "e"})
	if h1 == h3 {
		t.Fatal("expected different hash for different exits")
	}
}

func TestParseRoomData(t *testing.T) {
	text := "\tA beautiful garden\nObvious exits are north, south, east and west."
	rd := ParseRoomData(text)
	if rd == nil {
		t.Fatal("expected non-nil RoomData")
	}
	if rd.Description != "A beautiful garden" {
		t.Fatalf("expected description 'A beautiful garden', got %s", rd.Description)
	}
	if len(rd.Exits) < 2 {
		t.Fatalf("expected exits parsed, got %v", rd.Exits)
	}
	if rd.RawText != text {
		t.Fatal("expected RawText to preserve original")
	}
}

func TestParseRoomDataFallback(t *testing.T) {
	// No tab-indented line, no "obvious exit" — should fall back to first non-empty line
	text := "A simple room\n"
	rd := ParseRoomData(text)
	if rd.Description != "A simple room" {
		t.Fatalf("expected fallback description 'A simple room', got %s", rd.Description)
	}
}
