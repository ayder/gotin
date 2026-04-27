package mapper

import "testing"

func TestDirectionVector(t *testing.T) {
	cases := []struct {
		dir    Direction
		wantDx int
		wantDy int
		wantDz int
		wantOk bool
	}{
		{North, 0, 1, 0, true},
		{South, 0, -1, 0, true},
		{East, 1, 0, 0, true},
		{West, -1, 0, 0, true},
		{NorthEast, 1, 1, 0, true},
		{NorthWest, -1, 1, 0, true},
		{SouthEast, 1, -1, 0, true},
		{SouthWest, -1, -1, 0, true},
		{Up, 0, 0, 1, true},
		{Down, 0, 0, -1, true},
		{In, 0, 0, 0, false},
		{Out, 0, 0, 0, false},
		{Direction("zzz"), 0, 0, 0, false},
	}
	for _, c := range cases {
		gotDx, gotDy, gotDz, gotOk := DirectionVector(c.dir)
		if gotDx != c.wantDx || gotDy != c.wantDy || gotDz != c.wantDz || gotOk != c.wantOk {
			t.Errorf("DirectionVector(%q) = (%d,%d,%d,%v); want (%d,%d,%d,%v)",
				c.dir, gotDx, gotDy, gotDz, gotOk, c.wantDx, c.wantDy, c.wantDz, c.wantOk)
		}
	}
}
