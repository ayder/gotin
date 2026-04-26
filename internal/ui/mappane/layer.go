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
