package mapper

import (
	"fmt"
	"strings"

	"github.com/google/uuid"
)

// mapCommand is the internal undo interface. Every mutating operation pushes
// one implementation onto the undo stack.
type mapCommand interface {
	execute(e *Engine) error
	undo(e *Engine) error
}

// ─── Explicit user commands ────────────────────────────────────────────────

// digCmd creates a new room in a direction from the current room.
type digCmd struct {
	dir     Direction
	name    string
	roomID  string
	fromID  string
	reverse Direction
}

func (c *digCmd) execute(e *Engine) error {
	validDir := false
	for _, p := range e.paths {
		if p == c.dir {
			validDir = true
			break
		}
	}
	if !validDir {
		return fmt.Errorf("direction %s is not configured in paths", c.dir)
	}

	currID := e.data.CurrentRoom
	curr, ok := e.data.Rooms[currID]
	if !ok {
		return fmt.Errorf("current room not found")
	}
	if _, exists := curr.Exits[c.dir]; exists {
		return fmt.Errorf("exit %s already exists", c.dir)
	}

	nx, ny, nz := curr.X, curr.Y, curr.Z
	switch c.dir {
	case North:
		ny++
	case South:
		ny--
	case East:
		nx++
	case West:
		nx--
	case NorthEast:
		nx++
		ny++
	case SouthWest:
		nx--
		ny--
	case NorthWest:
		nx--
		ny++
	case SouthEast:
		nx++
		ny--
	case Up:
		nz++
	case Down:
		nz--
	}

	newRoom := &Room{
		ID:    uuid.New().String(),
		Name:  c.name,
		Exits: make(map[Direction]string),
		X:     nx,
		Y:     ny,
		Z:     nz,
	}

	curr.Exits[c.dir] = newRoom.ID
	c.fromID = curr.ID
	c.reverse = ReverseDirection(c.dir)
	if c.reverse != "" {
		newRoom.Exits[c.reverse] = curr.ID
	}

	e.data.Rooms[newRoom.ID] = newRoom
	e.data.CurrentRoom = newRoom.ID
	c.roomID = newRoom.ID
	return nil
}

func (c *digCmd) undo(e *Engine) error {
	delete(e.data.Rooms, c.roomID)
	if from, ok := e.data.Rooms[c.fromID]; ok {
		delete(from.Exits, c.dir)
	}
	e.data.CurrentRoom = c.fromID
	return nil
}

// linkCmd adds a one-way exit from the current room to a target room.
type linkCmd struct {
	dir         Direction
	targetQuery string
	targetID    string
	fromID      string
}

func (c *linkCmd) execute(e *Engine) error {
	validDir := false
	for _, p := range e.paths {
		if p == c.dir {
			validDir = true
			break
		}
	}
	if !validDir {
		return fmt.Errorf("direction %s is not configured in paths", c.dir)
	}

	curr, ok := e.data.Rooms[e.data.CurrentRoom]
	if !ok {
		return fmt.Errorf("current room not found")
	}

	// Resolve target by ID, partial ID, or name
	var targetID string
	if _, ok := e.data.Rooms[c.targetQuery]; ok {
		targetID = c.targetQuery
	}
	if targetID == "" {
		for id := range e.data.Rooms {
			if strings.HasPrefix(id, c.targetQuery) {
				targetID = id
				break
			}
		}
	}
	if targetID == "" {
		queryLower := strings.ToLower(c.targetQuery)
		for id, room := range e.data.Rooms {
			if strings.ToLower(room.Name) == queryLower {
				targetID = id
				break
			}
		}
	}
	if targetID == "" {
		return fmt.Errorf("target room not found: %s", c.targetQuery)
	}
	if targetID == e.data.CurrentRoom {
		return fmt.Errorf("cannot link room to itself")
	}

	curr.Exits[c.dir] = targetID
	c.fromID = e.data.CurrentRoom
	c.targetID = targetID
	return nil
}

func (c *linkCmd) undo(e *Engine) error {
	if from, ok := e.data.Rooms[c.fromID]; ok {
		delete(from.Exits, c.dir)
	}
	return nil
}

// deleteCmd removes a room and all exits pointing to it.
type deleteCmd struct {
	query        string
	deletedRoom  *Room
	backlinks    map[string][]Direction
	oldCurrentID string
}

func (c *deleteCmd) execute(e *Engine) error {
	var targetID string
	if _, ok := e.data.Rooms[c.query]; ok {
		targetID = c.query
	}
	if targetID == "" {
		for id := range e.data.Rooms {
			if strings.HasPrefix(id, c.query) {
				targetID = id
				break
			}
		}
	}
	if targetID == "" {
		queryLower := strings.ToLower(c.query)
		for id, room := range e.data.Rooms {
			if strings.ToLower(room.Name) == queryLower {
				targetID = id
				break
			}
		}
	}
	if targetID == "" {
		return fmt.Errorf("room not found: %s", c.query)
	}

	room := e.data.Rooms[targetID]
	c.deletedRoom = &Room{
		ID:              room.ID,
		Name:            room.Name,
		Description:     room.Description,
		DescriptionHash: room.DescriptionHash,
		Exits:           make(map[Direction]string, len(room.Exits)),
		X:               room.X,
		Y:               room.Y,
		Z:               room.Z,
	}
	for d, id := range room.Exits {
		c.deletedRoom.Exits[d] = id
	}

	c.backlinks = make(map[string][]Direction)
	for _, r := range e.data.Rooms {
		for d, exitID := range r.Exits {
			if exitID == targetID {
				c.backlinks[r.ID] = append(c.backlinks[r.ID], d)
			}
		}
	}

	for _, r := range e.data.Rooms {
		for d, exitID := range r.Exits {
			if exitID == targetID {
				delete(r.Exits, d)
			}
		}
	}

	c.oldCurrentID = e.data.CurrentRoom
	delete(e.data.Rooms, targetID)
	if e.data.CurrentRoom == targetID {
		if len(e.data.Rooms) > 0 {
			for k := range e.data.Rooms {
				e.data.CurrentRoom = k
				break
			}
		}
	}
	return nil
}

func (c *deleteCmd) undo(e *Engine) error {
	e.data.Rooms[c.deletedRoom.ID] = c.deletedRoom
	for roomID, directions := range c.backlinks {
		if r, ok := e.data.Rooms[roomID]; ok {
			for _, d := range directions {
				r.Exits[d] = c.deletedRoom.ID
			}
		}
	}
	if _, ok := e.data.Rooms[c.oldCurrentID]; ok {
		e.data.CurrentRoom = c.oldCurrentID
	}
	return nil
}

// ─── Snapshot command for auto-mapping ─────────────────────────────────────

// snapshotCmd captures a deep copy of the entire map state. It is used by
// auto-mapping (ProcessRoomData / HandleGMCPRoomInfo) where a full-state
// restore is the safest undo semantics.
type snapshotCmd struct {
	snapshot        *Map
	pendingDir      Direction
	pendingFromRoom string
}

func (c *snapshotCmd) execute(e *Engine) error { return nil }

func (c *snapshotCmd) undo(e *Engine) error {
	e.data = c.snapshot
	e.pendingDir = c.pendingDir
	e.pendingFromRoom = c.pendingFromRoom
	return nil
}
