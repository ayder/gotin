package mapper

import (
	"os"
	"path/filepath"
	"strings"
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

func TestAutoMappingState(t *testing.T) {
	e := newEngine(t)
	if e.IsAutoMapping() {
		t.Fatal("expected auto-mapping off by default")
	}
	e.StartAutoMapping()
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
	processed, _, _ := e.ProcessMovement("xyz")
	if processed {
		t.Fatal("expected no processing for invalid direction")
	}
}

// ─── ProcessRoomData ───────────────────────────────────────────────────────

func TestProcessRoomDataNewRoom(t *testing.T) {
	e := mustCreate(t, "")
	e.StartAutoMapping()
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
	e.StartAutoMapping()
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

func TestProcessRoomDataNoPending(t *testing.T) {
	e := mustCreate(t, "")
	e.StartAutoMapping()
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
	e.StartAutoMapping()
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

func TestHandleGMCPRoomInfoNoPending(t *testing.T) {
	e := mustCreate(t, "")
	e.StartAutoMapping()
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
