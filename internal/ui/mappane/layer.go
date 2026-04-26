package mappane

import (
	"sort"

	"github.com/ayder/gotin/internal/mapper"
)

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
