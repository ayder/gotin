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
