package mapper

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"

	"github.com/TyphonHill/go-mermaid/diagrams/flowchart"
	"github.com/google/uuid"
)

// Direction represents a cardinal direction.
type Direction string

const (
	North     Direction = "n"
	South     Direction = "s"
	East      Direction = "e"
	West      Direction = "w"
	NorthEast Direction = "ne"
	SouthWest Direction = "sw"
	NorthWest Direction = "nw"
	SouthEast Direction = "se"
	Up        Direction = "u"
	Down      Direction = "d"
	In        Direction = "in"
	Out       Direction = "out"
)

// MappingOptions configures which loop-detection strategies the mapper uses
// during auto-mapping. Strategies are independent and may be combined.
type MappingOptions struct {
	Vnum bool `json:"vnum"`
	Hash bool `json:"hash"`
}

// DefaultMappingOptions returns the safe default: vnum on, hash off.
func DefaultMappingOptions() MappingOptions {
	return MappingOptions{Vnum: true, Hash: false}
}

// ReverseDirection returns the opposite direction.
func ReverseDirection(dir Direction) Direction {
	switch dir {
	case North:
		return South
	case South:
		return North
	case East:
		return West
	case West:
		return East
	case NorthEast:
		return SouthWest
	case SouthWest:
		return NorthEast
	case NorthWest:
		return SouthEast
	case SouthEast:
		return NorthWest
	case Up:
		return Down
	case Down:
		return Up
	case In:
		return Out
	case Out:
		return In
	default:
		return ""
	}
}

// Room represents a location in the MUD.
type Room struct {
	ID              string               `json:"id"`
	Name            string               `json:"name"`
	Description     string               `json:"description"`
	DescriptionHash string               `json:"description_hash"`
	Exits           map[Direction]string `json:"exits"` // Direction -> RoomID
	X               int                  `json:"x"`
	Y               int                  `json:"y"`
	Z               int                  `json:"z"`
}

// Map holds the world state.
type Map struct {
	Rooms       map[string]*Room `json:"rooms"`
	CurrentRoom string           `json:"current_room"`
}

// deepCopy returns a fully independent copy of the map.
func (m *Map) deepCopy() *Map {
	copy := &Map{
		Rooms:       make(map[string]*Room, len(m.Rooms)),
		CurrentRoom: m.CurrentRoom,
	}
	for id, room := range m.Rooms {
		r := &Room{
			ID:              room.ID,
			Name:            room.Name,
			Description:     room.Description,
			DescriptionHash: room.DescriptionHash,
			X:               room.X,
			Y:               room.Y,
			Z:               room.Z,
		}
		if room.Exits != nil {
			r.Exits = make(map[Direction]string, len(room.Exits))
			for d, exitID := range room.Exits {
				r.Exits[d] = exitID
			}
		}
		copy.Rooms[id] = r
	}
	return copy
}

// DefaultPaths returns the standard set of directions.
func DefaultPaths() []Direction {
	return []Direction{
		North, South, East, West,
		NorthEast, SouthWest, NorthWest, SouthEast,
		Up, Down, In, Out,
	}
}

// Engine manages the map and operations.
type Engine struct {
	data        *Map
	mu          sync.RWMutex
	path        string       // Persistence path
	undo        []mapCommand // Undo stack
	paths       []Direction  // Configured available directions
	autoMapping bool         // Whether auto-mapping mode is enabled

	// Smart auto-mapping state
	hashIndex       map[string]string // Hash -> RoomID for loop detection
	structuralIndex map[string]string // Phase 2 structural hash -> RoomID
	pendingDir      Direction         // Direction we're moving in (for linking after room data arrives)
	pendingFromRoom string            // Room ID we're moving from
	pendingName     string            // One-slot latch for out-of-band room name (e.g. MXP <ROOMNAME>)
	options         MappingOptions
	profile         MUDProfile
	sawMXP          bool
	sawBlockStart   bool
}

// RoomData represents parsed room information from MUD output.
type RoomData struct {
	Description string   // First 50 chars of description (tab-indented line)
	Exits       []string // Parsed exit directions
	RawText     string   // Full raw text for storage
}

// NewEngine creates a new Mapper Engine.
func NewEngine(path string) *Engine {
	return &Engine{
		data: &Map{
			Rooms: make(map[string]*Room),
		},
		path:            path,
		undo:            make([]mapCommand, 0),
		paths:           DefaultPaths(),
		hashIndex:       make(map[string]string),
		structuralIndex: make(map[string]string),
		options:         DefaultMappingOptions(),
		profile:         DefaultT2TMUDProfile(),
	}
}

// SetPaths configures the available directions for mapping.
func (e *Engine) SetPaths(paths []Direction) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.paths = paths
}

// GetPaths returns the currently configured directions.
func (e *Engine) GetPaths() []Direction {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.paths
}

// SetMappingOptions replaces the loop-detection options. Safe for concurrent use.
func (e *Engine) SetMappingOptions(opts MappingOptions) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.options = opts
}

// GetMappingOptions returns the current loop-detection options.
func (e *Engine) GetMappingOptions() MappingOptions {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.options
}

// SetMUDProfile replaces the MUD profile used by Phase 2 block parsing.
func (e *Engine) SetMUDProfile(p MUDProfile) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.profile = p
}

// GetMUDProfile returns the current MUD profile.
func (e *Engine) GetMUDProfile() MUDProfile {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.profile
}

// Snapshot returns a deep-copied *Map plus the current room ID. Safe for
// rendering off-engine without holding e.mu. Returns a non-nil empty Map
// when the engine has not yet been Created; CurrentRoom may be "".
func (e *Engine) Snapshot() (*Map, string) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	if e.data == nil {
		return &Map{Rooms: make(map[string]*Room)}, ""
	}
	return e.data.deepCopy(), e.data.CurrentRoom
}

// MarkMXPSeen flags that at least one MXP-emitted tag has been observed on the
// current connection.
func (e *Engine) MarkMXPSeen() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.sawMXP = true
}

// HasMXPSeen reports whether any MXP tag has been observed yet.
func (e *Engine) HasMXPSeen() bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.sawMXP
}

// MarkBlockStartSeen flags that the configured block-start tag has been
// observed at least once on this connection.
func (e *Engine) MarkBlockStartSeen() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.sawBlockStart = true
}

// HasBlockStartSeen reports the current block-start sentinel state.
func (e *Engine) HasBlockStartSeen() bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.sawBlockStart
}

// ResetBlockStart clears both MXP-seen and block-start sentinels.
func (e *Engine) ResetBlockStart() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.sawMXP = false
	e.sawBlockStart = false
}

// IsValidDirection checks if a direction is in the configured paths.
func (e *Engine) IsValidDirection(dir Direction) bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	for _, p := range e.paths {
		if p == dir {
			return true
		}
	}
	return false
}

// ParseDirection converts a string to a Direction, handling aliases.
func ParseDirection(s string) (Direction, bool) {
	s = strings.ToLower(strings.TrimSpace(s))
	switch s {
	case "n", "north":
		return North, true
	case "s", "south":
		return South, true
	case "e", "east":
		return East, true
	case "w", "west":
		return West, true
	case "ne", "northeast":
		return NorthEast, true
	case "sw", "southwest":
		return SouthWest, true
	case "nw", "northwest":
		return NorthWest, true
	case "se", "southeast":
		return SouthEast, true
	case "u", "up":
		return Up, true
	case "d", "down":
		return Down, true
	case "in":
		return In, true
	case "out":
		return Out, true
	default:
		return "", false
	}
}

// HashDescription computes the SHA256 hash of description text.
func HashDescription(text string) string {
	hash := sha256.Sum256([]byte(strings.TrimSpace(text)))
	return hex.EncodeToString(hash[:])
}

// ComputeRoomHash creates a hash for loop detection using Desc[0:50] + Exits.
// This avoids time/weather/NPC variations in room descriptions.
func ComputeRoomHash(desc string, exits []string) string {
	// Take first 50 chars of description (or less if shorter)
	descPart := desc
	if len(descPart) > 50 {
		descPart = descPart[:50]
	}

	// Sort exits for consistent ordering
	sortedExits := make([]string, len(exits))
	copy(sortedExits, exits)
	// Simple sort (exits are short strings)
	for i := 0; i < len(sortedExits); i++ {
		for j := i + 1; j < len(sortedExits); j++ {
			if sortedExits[i] > sortedExits[j] {
				sortedExits[i], sortedExits[j] = sortedExits[j], sortedExits[i]
			}
		}
	}

	combined := descPart + "|" + strings.Join(sortedExits, ",")
	hash := sha256.Sum256([]byte(combined))
	return hex.EncodeToString(hash[:])
}

// normaliseDescription trims surrounding whitespace and collapses any run of
// internal whitespace within a line down to a single space. Newlines are
// preserved.
func normaliseDescription(s string) string {
	lines := strings.Split(s, "\n")
	var out []string
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		out = append(out, strings.Join(fields, " "))
	}
	return strings.Join(out, "\n")
}

// ComputeStructuralHash builds a Phase 2 room-identity hash from a cleaned
// description and a sorted exit set. The returned value is prefixed "v2:" so
// it is externally distinguishable from legacy ComputeRoomHash output.
func ComputeStructuralHash(desc string, exits []string) string {
	norm := normaliseDescription(desc)
	sortedExits := append([]string(nil), exits...)
	sort.Strings(sortedExits)
	payload := "v2|" + norm + "|" + strings.Join(sortedExits, ",")
	h := sha256.Sum256([]byte(payload))
	return "v2:" + hex.EncodeToString(h[:])
}

// ParseRoomData extracts room description and exits from MUD output.
// Looks for tab-indented lines for description and standard exit patterns.
func ParseRoomData(text string) *RoomData {
	lines := strings.Split(text, "\n")
	var desc string
	var exits []string

	for _, line := range lines {
		// Description: lines starting with tab
		if strings.HasPrefix(line, "\t") && desc == "" {
			desc = strings.TrimSpace(line)
		}

		// Exit patterns:
		// "The only obvious exits are north, south, east and west."
		// "The only obvious exit is north."
		// Or tab-indented exit line
		lineLower := strings.ToLower(line)
		if strings.Contains(lineLower, "obvious exit") {
			exits = parseExitsFromLine(lineLower)
		}
	}

	if desc == "" && len(lines) > 0 {
		// Fallback: use first non-empty line as description
		for _, line := range lines {
			trimmed := strings.TrimSpace(line)
			if trimmed != "" {
				desc = trimmed
				break
			}
		}
	}

	return &RoomData{
		Description: desc,
		Exits:       exits,
		RawText:     text,
	}
}

// parseExitsFromLine extracts direction names from an exit description line.
func parseExitsFromLine(line string) []string {
	var exits []string

	// Common direction words to look for
	directions := []string{"north", "south", "east", "west", "northeast", "northwest", "southeast", "southwest", "up", "down", "in", "out"}

	for _, dir := range directions {
		if strings.Contains(line, dir) {
			exits = append(exits, dir)
		}
	}

	return exits
}

// Create initializes a new map, optionally loading from a file.
func (e *Engine) Create(filename string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.path = filename
	if filename != "" {
		// Attempt to load
		data, err := os.ReadFile(filename)
		if err == nil {
			if err := json.Unmarshal(data, e.data); err != nil {
				return err
			}
			// Rebuild hash index from loaded rooms
			e.rebuildHashIndex()
			return nil
		} else if !os.IsNotExist(err) {
			return err
		}
	}

	// Init fresh if load failed or not requested
	e.data = &Map{
		Rooms: make(map[string]*Room),
	}
	e.hashIndex = make(map[string]string)
	e.structuralIndex = make(map[string]string)
	e.undo = e.undo[:0]

	// Create Start Room
	startRoom := &Room{
		ID:    uuid.New().String(),
		Name:  "Start",
		Exits: make(map[Direction]string),
	}
	e.data.Rooms[startRoom.ID] = startRoom
	e.data.CurrentRoom = startRoom.ID
	return nil
}

// rebuildHashIndex rebuilds both legacy and structural hash indices from
// existing rooms.
func (e *Engine) rebuildHashIndex() {
	e.hashIndex = make(map[string]string)
	e.structuralIndex = make(map[string]string)
	for id, room := range e.data.Rooms {
		if room.DescriptionHash != "" {
			e.hashIndex[room.DescriptionHash] = id
			e.structuralIndex[room.DescriptionHash] = id
		}
	}
}

// saveState pushes a snapshot of the current map state onto the undo stack.
// Used by auto-mapping operations.
func (e *Engine) saveState() {
	e.undo = append(e.undo, &snapshotCmd{
		snapshot:        e.data.deepCopy(),
		pendingDir:      e.pendingDir,
		pendingFromRoom: e.pendingFromRoom,
		pendingName:     e.pendingName,
	})
	if len(e.undo) > 50 {
		e.undo = e.undo[1:]
	}
}

// Dig creates a new room in the specified direction.
func (e *Engine) Dig(dir Direction, name string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	cmd := &digCmd{dir: dir, name: name}
	if err := cmd.execute(e); err != nil {
		return err
	}
	e.undo = append(e.undo, cmd)
	if len(e.undo) > 50 {
		e.undo = e.undo[1:]
	}
	return nil
}

func (e *Engine) Undo() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if len(e.undo) == 0 {
		return fmt.Errorf("nothing to undo")
	}

	last := e.undo[len(e.undo)-1]
	e.undo = e.undo[:len(e.undo)-1]

	return last.undo(e)
}

func (e *Engine) DeleteRoom(id string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	cmd := &deleteCmd{query: id}
	if err := cmd.execute(e); err != nil {
		return err
	}
	e.undo = append(e.undo, cmd)
	if len(e.undo) > 50 {
		e.undo = e.undo[1:]
	}
	return nil
}

func (e *Engine) SetName(name string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if curr, ok := e.data.Rooms[e.data.CurrentRoom]; ok {
		curr.Name = name
	}
}

func (e *Engine) SetDescription(desc string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if curr, ok := e.data.Rooms[e.data.CurrentRoom]; ok {
		curr.Description = desc
		curr.DescriptionHash = HashDescription(desc)
	}
}

func (e *Engine) Search(query string) []string {
	e.mu.RLock()
	defer e.mu.RUnlock()
	var results []string
	q := strings.ToLower(query)
	for _, r := range e.data.Rooms {
		if strings.Contains(strings.ToLower(r.Name), q) ||
			strings.Contains(strings.ToLower(r.Description), q) {
			results = append(results, fmt.Sprintf("[%s] %s", r.ID[:8], r.Name))
		}
	}
	return results
}

func (e *Engine) Show(radius int) string {
	e.mu.RLock()
	defer e.mu.RUnlock()

	currID := e.data.CurrentRoom
	if _, ok := e.data.Rooms[currID]; !ok {
		return "No map."
	}

	visibleRooms := make(map[string]*Room)

	if radius < 0 {
		for id, r := range e.data.Rooms {
			visibleRooms[id] = r
		}
	} else {
		// BFS to find rooms within radius
		queue := []string{currID}
		distances := make(map[string]int)
		distances[currID] = 0

		for len(queue) > 0 {
			id := queue[0]
			queue = queue[1:]

			r, ok := e.data.Rooms[id]
			if !ok {
				continue
			}
			visibleRooms[id] = r

			dist := distances[id]
			if dist < radius {
				// We also need stable iteration over exits to keep traversal predictable
				var sortedDirs []Direction
				for dir := range r.Exits {
					sortedDirs = append(sortedDirs, dir)
				}
				// Sort directions by string representation
				sort.Slice(sortedDirs, func(i, j int) bool {
					return string(sortedDirs[i]) < string(sortedDirs[j])
				})

				for _, dir := range sortedDirs {
					nextID := r.Exits[dir]
					if _, seen := distances[nextID]; !seen {
						distances[nextID] = dist + 1
						queue = append(queue, nextID)
					}
				}
			}
		}
	}

	fc := flowchart.NewFlowchart()
	fc.EnableMarkdownFence()
	fc.SetTitle("MUD Map")

	nodeMap := make(map[string]*flowchart.Node)

	// Sort room IDs for deterministic output
	var sortedIDs []string
	for id := range visibleRooms {
		sortedIDs = append(sortedIDs, id)
	}
	sort.Strings(sortedIDs)

	// Pass 1: Create Nodes
	for _, id := range sortedIDs {
		r := visibleRooms[id]
		cleanName := strings.ReplaceAll(r.Name, "\"", "'")
		if cleanName == "" {
			cleanName = id
		}

		node := fc.AddNode(cleanName)
		node.SetShape(flowchart.NodeShapeProcess)

		if id == currID {
			style := flowchart.NewNodeStyle()
			style.Fill = "#8f8" // Green
			style.Stroke = "#050"
			style.StrokeWidth = 2
			node.SetStyle(style)
		}

		nodeMap[id] = node
	}

	// Pass 2: Create Links
	for _, id := range sortedIDs {
		r := visibleRooms[id]
		fromNode := nodeMap[id]

		var sortedDirs []Direction
		for dir := range r.Exits {
			sortedDirs = append(sortedDirs, dir)
		}
		sort.Slice(sortedDirs, func(i, j int) bool {
			return string(sortedDirs[i]) < string(sortedDirs[j])
		})

		for _, dir := range sortedDirs {
			nextID := r.Exits[dir]
			if toNode, exists := nodeMap[nextID]; exists {
				link := fc.AddLink(fromNode, toNode)
				link.SetText(string(dir))
			}
		}
	}

	return fc.String()
}

func (e *Engine) Save() error {
	e.mu.RLock()
	defer e.mu.RUnlock()
	if e.path == "" {
		return fmt.Errorf("no filename set")
	}
	data, err := json.MarshalIndent(e.data, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(e.path, data, 0644)
}

func (e *Engine) GetCurrent() *Room {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.data.Rooms[e.data.CurrentRoom]
}

// Goto teleports the mapper to a room by ID or name.
func (e *Engine) Goto(query string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	// First try exact ID match
	if _, ok := e.data.Rooms[query]; ok {
		e.data.CurrentRoom = query
		return nil
	}

	// Try partial ID match (first 8 chars)
	for id := range e.data.Rooms {
		if strings.HasPrefix(id, query) {
			e.data.CurrentRoom = id
			return nil
		}
	}

	// Try name match (case-insensitive)
	queryLower := strings.ToLower(query)
	for id, room := range e.data.Rooms {
		if strings.ToLower(room.Name) == queryLower {
			e.data.CurrentRoom = id
			return nil
		}
	}

	// Try partial name match
	for id, room := range e.data.Rooms {
		if strings.Contains(strings.ToLower(room.Name), queryLower) {
			e.data.CurrentRoom = id
			return nil
		}
	}

	return fmt.Errorf("room not found: %s", query)
}

// Link creates a one-way exit from the current room to a target room.
func (e *Engine) Link(dir Direction, targetQuery string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	cmd := &linkCmd{dir: dir, targetQuery: targetQuery}
	if err := cmd.execute(e); err != nil {
		return err
	}
	e.undo = append(e.undo, cmd)
	if len(e.undo) > 50 {
		e.undo = e.undo[1:]
	}
	return nil
}

// DeleteByNameOrID deletes a room by ID or name.
func (e *Engine) DeleteByNameOrID(query string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	cmd := &deleteCmd{query: query}
	if err := cmd.execute(e); err != nil {
		return err
	}
	e.undo = append(e.undo, cmd)
	if len(e.undo) > 50 {
		e.undo = e.undo[1:]
	}
	return nil
}

// IsCreated reports whether a map has been initialized via Create. Used to
// gate auto-mapping start so callers must explicitly create or load a map
// before rooms begin accumulating.
func (e *Engine) IsCreated() bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.path != ""
}

// StartAutoMapping enables auto-mapping mode.
func (e *Engine) StartAutoMapping() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.autoMapping = true
}

// StopAutoMapping disables auto-mapping mode.
func (e *Engine) StopAutoMapping() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.autoMapping = false
}

// IsAutoMapping returns whether auto-mapping is enabled.
func (e *Engine) IsAutoMapping() bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.autoMapping
}

// ProcessMovement handles a movement command during auto-mapping.
// Returns true if the movement was processed, the new room name, and any error.
// For smart auto-mapping, this sets up pending state - call ProcessRoomData when room text arrives.
func (e *Engine) ProcessMovement(input string) (processed bool, roomName string, err error) {
	if !e.IsAutoMapping() {
		return false, "", nil
	}
	if !e.HasMXPSeen() || !e.HasBlockStartSeen() {
		return false, "", nil
	}

	// Check if input is a valid direction
	dir, ok := ParseDirection(input)
	if !ok {
		return false, "", nil
	}

	// Check if direction is in configured paths
	if !e.IsValidDirection(dir) {
		return false, "", nil
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	curr, ok := e.data.Rooms[e.data.CurrentRoom]
	if !ok {
		return false, "", fmt.Errorf("current room not found")
	}

	// If exit exists, just move to it
	if existingID, exists := curr.Exits[dir]; exists {
		e.data.CurrentRoom = existingID
		e.pendingDir = ""
		e.pendingFromRoom = ""
		if room, ok := e.data.Rooms[existingID]; ok {
			return true, room.Name, nil
		}
		return true, "", nil
	}

	// Set up pending state for smart auto-mapping
	// When room data arrives, we'll check for loop detection
	e.pendingDir = dir
	e.pendingFromRoom = e.data.CurrentRoom

	return true, "[pending room data]", nil
}

// HandleRoomBlock processes a fully-buffered room render produced by the Phase
// 2 RoomBlockBuffer.
func (e *Engine) HandleRoomBlock(b RoomBlock) (processed bool, roomName string, loopDetected bool, err error) {
	if !e.IsAutoMapping() {
		return false, "", false, nil
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	if !e.sawMXP || !e.sawBlockStart {
		return false, "", false, nil
	}

	if e.pendingDir == "" {
		curr, ok := e.data.Rooms[e.data.CurrentRoom]
		if ok {
			if b.Description != "" {
				curr.Description = b.Description
				if e.options.Hash {
					h := ComputeStructuralHash(b.Description, b.Exits)
					curr.DescriptionHash = h
					e.structuralIndex[h] = curr.ID
				}
			}
			if b.Name != "" {
				curr.Name = b.Name
			}
		}
		return false, "", false, nil
	}

	fromRoom, ok := e.data.Rooms[e.pendingFromRoom]
	if !ok {
		e.pendingDir = ""
		e.pendingFromRoom = ""
		e.pendingName = ""
		return false, "", false, fmt.Errorf("source room not found")
	}

	if e.options.Vnum && b.Vnum != "" {
		if existing, found := e.data.Rooms[b.Vnum]; found {
			e.saveState()
			e.completePendingMovement(fromRoom, e.pendingDir, b.Vnum, existing)
			return true, existing.Name, true, nil
		}
	}
	if e.options.Hash && b.Description != "" {
		h := ComputeStructuralHash(b.Description, b.Exits)
		if existingID, found := e.structuralIndex[h]; found {
			if existing, ok := e.data.Rooms[existingID]; ok {
				e.saveState()
				e.completePendingMovement(fromRoom, e.pendingDir, existingID, existing)
				return true, existing.Name, true, nil
			}
		}
	}

	var newID string
	if e.options.Vnum && b.Vnum != "" {
		newID = b.Vnum
	} else {
		newID = uuid.New().String()
	}
	e.saveState()
	var descHash string
	if e.options.Hash {
		descHash = ComputeStructuralHash(b.Description, b.Exits)
	}
	newRoom := e.createRoomV2(fromRoom, e.pendingDir, newID, b.Name, b.Description, descHash)
	return true, newRoom.Name, false, nil
}

// completePendingMovement links fromRoom to an existing room in the given direction,
// adds the reverse link if missing, sets current room, and clears pending state.
// The caller must have already called saveState() and must hold e.mu.
func (e *Engine) completePendingMovement(fromRoom *Room, dir Direction, existingID string, existingRoom *Room) {
	fromRoom.Exits[dir] = existingID
	reverse := ReverseDirection(dir)
	if reverse != "" {
		if _, hasReverse := existingRoom.Exits[reverse]; !hasReverse {
			existingRoom.Exits[reverse] = fromRoom.ID
		}
	}
	e.data.CurrentRoom = existingID
	e.pendingDir = ""
	e.pendingFromRoom = ""
	e.pendingName = ""
}

// createRoom creates a new room at the coordinates offset from fromRoom in the
// given direction, links it bidirectionally, and clears pending state.
// The caller must have already called saveState() and must hold e.mu.
func (e *Engine) createRoom(fromRoom *Room, dir Direction, id, name, description, descHash string) *Room {
	nx, ny, nz := fromRoom.X, fromRoom.Y, fromRoom.Z
	switch dir {
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

	effectiveName := name
	if effectiveName == "" || effectiveName == "New Room" {
		if e.pendingName != "" {
			effectiveName = e.pendingName
		}
	}
	e.pendingName = ""

	room := &Room{
		ID:              id,
		Name:            effectiveName,
		Description:     description,
		DescriptionHash: descHash,
		Exits:           make(map[Direction]string),
		X:               nx,
		Y:               ny,
		Z:               nz,
	}

	fromRoom.Exits[dir] = id
	reverse := ReverseDirection(dir)
	if reverse != "" {
		room.Exits[reverse] = fromRoom.ID
	}

	e.data.Rooms[id] = room
	if e.options.Hash && descHash != "" {
		e.hashIndex[descHash] = id
	}
	e.data.CurrentRoom = id

	e.pendingDir = ""
	e.pendingFromRoom = ""

	return room
}

// createRoomV2 mirrors createRoom but writes into structuralIndex instead of
// the legacy hashIndex. Caller must hold e.mu.
func (e *Engine) createRoomV2(fromRoom *Room, dir Direction, id, name, description, descHash string) *Room {
	nx, ny, nz := fromRoom.X, fromRoom.Y, fromRoom.Z
	switch dir {
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

	effectiveName := name
	if effectiveName == "" || effectiveName == "New Room" {
		if e.pendingName != "" {
			effectiveName = e.pendingName
		}
	}
	e.pendingName = ""

	room := &Room{
		ID:              id,
		Name:            effectiveName,
		Description:     description,
		DescriptionHash: descHash,
		Exits:           make(map[Direction]string),
		X:               nx,
		Y:               ny,
		Z:               nz,
	}

	fromRoom.Exits[dir] = id
	reverse := ReverseDirection(dir)
	if reverse != "" {
		room.Exits[reverse] = fromRoom.ID
	}

	e.data.Rooms[id] = room
	if e.options.Hash && descHash != "" {
		e.structuralIndex[descHash] = id
	}
	e.data.CurrentRoom = id

	e.pendingDir = ""
	e.pendingFromRoom = ""

	return room
}

// ProcessRoomData processes incoming room data for smart auto-mapping.
// This should be called when MUD room description text is received.
// Returns: (wasProcessed, roomName, isLoopDetected, error)
func (e *Engine) ProcessRoomData(text string) (processed bool, roomName string, loopDetected bool, err error) {
	if !e.IsAutoMapping() {
		return false, "", false, nil
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	if e.pendingDir == "" {
		if curr, ok := e.data.Rooms[e.data.CurrentRoom]; ok {
			roomData := ParseRoomData(text)
			if roomData.Description != "" {
				curr.Description = roomData.RawText
				if e.options.Hash {
					hash := ComputeRoomHash(roomData.Description, roomData.Exits)
					curr.DescriptionHash = hash
					e.hashIndex[hash] = curr.ID
				}
			}
		}
		return false, "", false, nil
	}

	roomData := ParseRoomData(text)

	fromRoom, ok := e.data.Rooms[e.pendingFromRoom]
	if !ok {
		e.pendingDir = ""
		e.pendingFromRoom = ""
		e.pendingName = ""
		return false, "", false, fmt.Errorf("source room not found")
	}

	if e.options.Hash {
		hash := ComputeRoomHash(roomData.Description, roomData.Exits)
		if existingID, found := e.hashIndex[hash]; found {
			if existingRoom, ok := e.data.Rooms[existingID]; ok {
				e.saveState()
				e.completePendingMovement(fromRoom, e.pendingDir, existingID, existingRoom)
				return true, existingRoom.Name, true, nil
			}
		}
	}

	e.saveState()
	var descHash string
	if e.options.Hash {
		descHash = ComputeRoomHash(roomData.Description, roomData.Exits)
	}
	newRoom := e.createRoom(fromRoom, e.pendingDir, uuid.New().String(), "New Room", roomData.RawText, descHash)
	return true, newRoom.Name, false, nil
}

// GMCPRoom holds structured room data from a GMCP Room.Info message.
type GMCPRoom struct {
	Vnum        string   // Stable room ID from MUD (num/id/vnum)
	Name        string   // Room name
	Description string   // Room description
	Area        string   // Optional area/zone name
	Exits       []string // Available exit directions
}

// HandleGMCPRoomInfo processes a GMCP Room.Info message for auto-mapping.
// If auto-mapping is off, it returns immediately.
// If there is a pending movement, it completes the link/create logic using the
// GMCP data (vnum for loop detection when available, otherwise description hash).
// Returns: (wasProcessed, roomName, isLoopDetected, error)
func (e *Engine) HandleGMCPRoomInfo(r GMCPRoom) (processed bool, roomName string, loopDetected bool, err error) {
	if !e.IsAutoMapping() {
		return false, "", false, nil
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	// Normalise exit directions to the mapper's short form.
	var exits []string
	for _, ex := range r.Exits {
		if d, ok := ParseDirection(ex); ok {
			exits = append(exits, string(d))
		} else {
			exits = append(exits, ex)
		}
	}

	// No pending movement — just update current room info.
	if e.pendingDir == "" {
		if curr, ok := e.data.Rooms[e.data.CurrentRoom]; ok {
			if r.Description != "" {
				curr.Description = r.Description
				if e.options.Hash {
					curr.DescriptionHash = ComputeRoomHash(r.Description, exits)
					e.hashIndex[curr.DescriptionHash] = curr.ID
				}
			}
			if r.Name != "" {
				curr.Name = r.Name
			}
		}
		return false, "", false, nil
	}

	fromRoom, ok := e.data.Rooms[e.pendingFromRoom]
	if !ok {
		e.pendingDir = ""
		e.pendingFromRoom = ""
		e.pendingName = ""
		return false, "", false, fmt.Errorf("source room not found")
	}

	if e.options.Vnum && r.Vnum != "" {
		if existingRoom, found := e.data.Rooms[r.Vnum]; found {
			e.saveState()
			e.completePendingMovement(fromRoom, e.pendingDir, r.Vnum, existingRoom)
			return true, existingRoom.Name, true, nil
		}
	}
	if e.options.Hash && r.Description != "" {
		hash := ComputeRoomHash(r.Description, exits)
		if existingID, found := e.hashIndex[hash]; found {
			if existingRoom, ok := e.data.Rooms[existingID]; ok {
				e.saveState()
				e.completePendingMovement(fromRoom, e.pendingDir, existingID, existingRoom)
				return true, existingRoom.Name, true, nil
			}
		}
	}

	var newID string
	if e.options.Vnum && r.Vnum != "" {
		newID = r.Vnum
	} else {
		newID = uuid.New().String()
	}
	e.saveState()
	var descHash string
	if e.options.Hash {
		descHash = ComputeRoomHash(r.Description, exits)
	}
	newRoom := e.createRoom(fromRoom, e.pendingDir, newID, r.Name, r.Description, descHash)
	return true, newRoom.Name, false, nil
}

// SetIncomingRoomName attaches a name supplied out-of-band to the room that
// the next room-data handler will create. If no movement is pending, it renames
// the current room immediately. Repeated calls before consumption: last wins.
func (e *Engine) SetIncomingRoomName(name string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if name == "" {
		return
	}
	if e.pendingDir != "" {
		e.pendingName = name
		return
	}
	if curr, ok := e.data.Rooms[e.data.CurrentRoom]; ok {
		curr.Name = name
	}
}

// HasPendingMovement returns true if there's a pending movement waiting for room data.
func (e *Engine) HasPendingMovement() bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.pendingDir != ""
}

// CancelPendingMovement clears any pending movement state.
func (e *Engine) CancelPendingMovement() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.pendingDir = ""
	e.pendingFromRoom = ""
	e.pendingName = ""
}

// Move moves to an adjacent room in the specified direction (without creating new rooms).
func (e *Engine) Move(dir Direction) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	curr, ok := e.data.Rooms[e.data.CurrentRoom]
	if !ok {
		return fmt.Errorf("current room not found")
	}

	targetID, exists := curr.Exits[dir]
	if !exists {
		return fmt.Errorf("no exit in direction %s", dir)
	}

	if _, ok := e.data.Rooms[targetID]; !ok {
		return fmt.Errorf("target room not found")
	}

	e.data.CurrentRoom = targetID
	return nil
}
