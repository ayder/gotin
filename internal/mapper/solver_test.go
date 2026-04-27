package mapper

import "testing"

func graphFromEdges(ids []string, edges []struct {
	From string
	Dir  Direction
	To   string
}) *Map {
	m := &Map{Rooms: make(map[string]*Room, len(ids))}
	for _, id := range ids {
		m.Rooms[id] = &Room{ID: id, Exits: make(map[Direction]string)}
	}
	for _, e := range edges {
		m.Rooms[e.From].Exits[e.Dir] = e.To
		if rev := ReverseDirection(e.Dir); rev != "" {
			m.Rooms[e.To].Exits[rev] = e.From
		}
	}
	m.CurrentRoom = ids[0]
	return m
}

func TestClosingTriangle(t *testing.T) {
	m := graphFromEdges(
		[]string{"A", "B", "C"},
		[]struct {
			From string
			Dir  Direction
			To   string
		}{
			{"A", South, "B"},
			{"B", NorthWest, "C"},
			{"C", East, "A"},
		},
	)

	NewSolver(SolverConfig{RestLength: 1, MaxIterations: 500}).Solve(m)

	a, b, c := m.Rooms["A"], m.Rooms["B"], m.Rooms["C"]
	coords := map[[2]int]bool{
		{a.X, a.Y}: true,
		{b.X, b.Y}: true,
		{c.X, c.Y}: true,
	}
	if len(coords) != 3 {
		t.Fatalf("expected 3 distinct cells, got %d: A=%v B=%v C=%v",
			len(coords), [2]int{a.X, a.Y}, [2]int{b.X, b.Y}, [2]int{c.X, c.Y})
	}
	maxDist := 0
	for _, p := range [][2]*Room{{a, b}, {b, c}, {a, c}} {
		d := absInt(p[0].X-p[1].X) + absInt(p[0].Y-p[1].Y)
		if d > maxDist {
			maxDist = d
		}
	}
	if maxDist > 4 {
		t.Fatalf("triangle too stretched (Manhattan max %d > 4): A=%v B=%v C=%v",
			maxDist, [2]int{a.X, a.Y}, [2]int{b.X, b.Y}, [2]int{c.X, c.Y})
	}
}

func TestFourLoop(t *testing.T) {
	m := graphFromEdges(
		[]string{"A", "B", "C", "D"},
		[]struct {
			From string
			Dir  Direction
			To   string
		}{
			{"A", North, "B"},
			{"B", East, "C"},
			{"C", South, "D"},
			{"D", West, "A"},
		},
	)
	NewSolver(SolverConfig{}).Solve(m)
	cells := map[[2]int]string{}
	for id, r := range m.Rooms {
		cells[[2]int{r.X, r.Y}] = id
	}
	if len(cells) != 4 {
		t.Fatalf("expected 4 distinct cells, got %d", len(cells))
	}
	for ida, a := range m.Rooms {
		for idb, b := range m.Rooms {
			if ida == idb {
				continue
			}
			if d := absInt(a.X-b.X) + absInt(a.Y-b.Y); d > 4 {
				t.Errorf("rooms %s and %s too far apart: %d", ida, idb, d)
			}
		}
	}
}

func TestSingleRoomIdempotent(t *testing.T) {
	m := &Map{
		Rooms:       map[string]*Room{"only": {ID: "only", X: 7, Y: 11, Exits: map[Direction]string{}}},
		CurrentRoom: "only",
	}
	NewSolver(SolverConfig{}).Solve(m)
	r := m.Rooms["only"]
	if r.X != 7 || r.Y != 11 {
		t.Errorf("single-room solve mutated coords to (%d,%d); want (7,11)", r.X, r.Y)
	}
}

func TestNoOverlap(t *testing.T) {
	m := graphFromEdges(
		[]string{"A", "B", "C"},
		[]struct {
			From string
			Dir  Direction
			To   string
		}{
			{"A", North, "B"},
			{"A", NorthEast, "C"},
		},
	)
	for _, r := range m.Rooms {
		r.X, r.Y = 0, 0
	}
	NewSolver(SolverConfig{}).Solve(m)
	cells := map[[2]int]bool{}
	for _, r := range m.Rooms {
		key := [2]int{r.X, r.Y}
		if cells[key] {
			t.Fatalf("overlap at (%d,%d)", r.X, r.Y)
		}
		cells[key] = true
	}
}

func TestDeterminism(t *testing.T) {
	build := func() *Map {
		return graphFromEdges(
			[]string{"r1", "r2", "r3", "r4", "r5"},
			[]struct {
				From string
				Dir  Direction
				To   string
			}{
				{"r1", North, "r2"},
				{"r2", East, "r3"},
				{"r3", South, "r4"},
				{"r4", West, "r1"},
				{"r1", NorthEast, "r5"},
			},
		)
	}
	s := NewSolver(SolverConfig{})
	base := build()
	s.Solve(base)
	baseCoords := map[string][2]int{}
	for id, r := range base.Rooms {
		baseCoords[id] = [2]int{r.X, r.Y}
	}
	for run := 0; run < 100; run++ {
		m := build()
		s.Solve(m)
		for id, want := range baseCoords {
			r := m.Rooms[id]
			if got := [2]int{r.X, r.Y}; got != want {
				t.Fatalf("run %d: room %s = %v; want %v", run, id, got, want)
			}
		}
	}
}
