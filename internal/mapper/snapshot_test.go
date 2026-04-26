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
