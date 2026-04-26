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
