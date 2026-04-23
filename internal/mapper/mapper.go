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

	"github.com/google/uuid"
	"github.com/TyphonHill/go-mermaid/diagrams/flowchart"
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
	path        string      // Persistence path
	undo        []string    // Stack of serialized states (simple but effective for small maps)
	paths       []Direction // Configured available directions
	autoMapping bool        // Whether auto-mapping mode is enabled

	// Smart auto-mapping state
	hashIndex       map[string]string // Hash -> RoomID for loop detection
	pendingDir      Direction         // Direction we're moving in (for linking after room data arrives)
	pendingFromRoom string            // Room ID we're moving from
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
		path:      path,
		undo:      make([]string, 0),
		paths:     DefaultPaths(),
		hashIndex: make(map[string]string),
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

// rebuildHashIndex rebuilds the hash index from existing rooms.
func (e *Engine) rebuildHashIndex() {
	e.hashIndex = make(map[string]string)
	for id, room := range e.data.Rooms {
		if room.DescriptionHash != "" {
			e.hashIndex[room.DescriptionHash] = id
		}
	}
}

// saveState pushes current state to undo stack
func (e *Engine) saveState() {
	if data, err := json.Marshal(e.data); err == nil {
		e.undo = append(e.undo, string(data))
		// Limit stack size
		if len(e.undo) > 50 {
			e.undo = e.undo[1:]
		}
	}
}

// Dig creates a new room in the specified direction.
func (e *Engine) Dig(dir Direction, name string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	// Validate direction is in configured paths
	validDir := false
	for _, p := range e.paths {
		if p == dir {
			validDir = true
			break
		}
	}
	if !validDir {
		return fmt.Errorf("direction %s is not configured in paths", dir)
	}

	e.saveState()

	currID := e.data.CurrentRoom
	curr, ok := e.data.Rooms[currID]
	if !ok {
		return fmt.Errorf("current room not found")
	}

	// Check if exit already exists
	if _, exists := curr.Exits[dir]; exists {
		return fmt.Errorf("exit %s already exists", dir)
	}

	// Calculate new coords
	nx, ny, nz := curr.X, curr.Y, curr.Z
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
	// In/Out don't affect spatial coordinates
	}

	newRoom := &Room{
		ID:    uuid.New().String(),
		Name:  name,
		Exits: make(map[Direction]string),
		X:     nx,
		Y:     ny,
		Z:     nz,
	}

	// Link
	curr.Exits[dir] = newRoom.ID

	reverse := ReverseDirection(dir)
	if reverse != "" {
		newRoom.Exits[reverse] = curr.ID
	}

	e.data.Rooms[newRoom.ID] = newRoom
	e.data.CurrentRoom = newRoom.ID
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

	var oldMap Map
	if err := json.Unmarshal([]byte(last), &oldMap); err != nil {
		return err
	}
	e.data = &oldMap
	return nil
}

func (e *Engine) DeleteRoom(id string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.saveState()

	if _, ok := e.data.Rooms[id]; !ok {
		return fmt.Errorf("room %s not found", id)
	}

	// Remove links from other rooms
	for _, r := range e.data.Rooms {
		for d, exitID := range r.Exits {
			if exitID == id {
				delete(r.Exits, d)
			}
		}
	}

	delete(e.data.Rooms, id)
	if e.data.CurrentRoom == id {
		// Reset to random or empty?
		// Just pick one if available, or create new start
		if len(e.data.Rooms) > 0 {
			for k := range e.data.Rooms {
				e.data.CurrentRoom = k
				break
			}
		} else {
			// Re-init? handled by Create ideally but here we just leave empty
		}
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

	// Validate direction is in configured paths
	validDir := false
	for _, p := range e.paths {
		if p == dir {
			validDir = true
			break
		}
	}
	if !validDir {
		return fmt.Errorf("direction %s is not configured in paths", dir)
	}

	curr, ok := e.data.Rooms[e.data.CurrentRoom]
	if !ok {
		return fmt.Errorf("current room not found")
	}

	// Find target room by ID or name
	var targetID string

	// Try exact ID match
	if _, ok := e.data.Rooms[targetQuery]; ok {
		targetID = targetQuery
	}

	// Try partial ID match
	if targetID == "" {
		for id := range e.data.Rooms {
			if strings.HasPrefix(id, targetQuery) {
				targetID = id
				break
			}
		}
	}

	// Try name match (case-insensitive)
	if targetID == "" {
		queryLower := strings.ToLower(targetQuery)
		for id, room := range e.data.Rooms {
			if strings.ToLower(room.Name) == queryLower {
				targetID = id
				break
			}
		}
	}

	if targetID == "" {
		return fmt.Errorf("target room not found: %s", targetQuery)
	}

	if targetID == e.data.CurrentRoom {
		return fmt.Errorf("cannot link room to itself")
	}

	e.saveState()

	// Create one-way link (no reverse link)
	curr.Exits[dir] = targetID
	return nil
}

// DeleteByNameOrID deletes a room by ID or name.
func (e *Engine) DeleteByNameOrID(query string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	var targetID string

	// Try exact ID match
	if _, ok := e.data.Rooms[query]; ok {
		targetID = query
	}

	// Try partial ID match
	if targetID == "" {
		for id := range e.data.Rooms {
			if strings.HasPrefix(id, query) {
				targetID = id
				break
			}
		}
	}

	// Try name match (case-insensitive)
	if targetID == "" {
		queryLower := strings.ToLower(query)
		for id, room := range e.data.Rooms {
			if strings.ToLower(room.Name) == queryLower {
				targetID = id
				break
			}
		}
	}

	if targetID == "" {
		return fmt.Errorf("room not found: %s", query)
	}

	e.saveState()

	// Remove links from other rooms
	for _, r := range e.data.Rooms {
		for d, exitID := range r.Exits {
			if exitID == targetID {
				delete(r.Exits, d)
			}
		}
	}

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

// ProcessRoomData processes incoming room data for smart auto-mapping.
// This should be called when MUD room description text is received.
// Returns: (wasProcessed, roomName, isLoopDetected, error)
func (e *Engine) ProcessRoomData(text string) (processed bool, roomName string, loopDetected bool, err error) {
	if !e.IsAutoMapping() {
		return false, "", false, nil
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	// No pending movement to complete
	if e.pendingDir == "" {
		// Still update current room's description if we have one
		if curr, ok := e.data.Rooms[e.data.CurrentRoom]; ok {
			roomData := ParseRoomData(text)
			if roomData.Description != "" {
				curr.Description = roomData.RawText
				hash := ComputeRoomHash(roomData.Description, roomData.Exits)
				curr.DescriptionHash = hash
				e.hashIndex[hash] = curr.ID
			}
		}
		return false, "", false, nil
	}

	// Parse the room data
	roomData := ParseRoomData(text)
	hash := ComputeRoomHash(roomData.Description, roomData.Exits)

	fromRoom, ok := e.data.Rooms[e.pendingFromRoom]
	if !ok {
		e.pendingDir = ""
		e.pendingFromRoom = ""
		return false, "", false, fmt.Errorf("source room not found")
	}

	// Check if we've seen this room before (loop detection)
	if existingID, found := e.hashIndex[hash]; found {
		if existingRoom, ok := e.data.Rooms[existingID]; ok {
			// Loop detected! Link to existing room instead of creating new one
			e.saveState()

			fromRoom.Exits[e.pendingDir] = existingID

			// Add reverse link if it doesn't exist
			reverse := ReverseDirection(e.pendingDir)
			if reverse != "" {
				if _, hasReverse := existingRoom.Exits[reverse]; !hasReverse {
					existingRoom.Exits[reverse] = e.pendingFromRoom
				}
			}

			e.data.CurrentRoom = existingID
			e.pendingDir = ""
			e.pendingFromRoom = ""

			return true, existingRoom.Name, true, nil
		}
	}

	// New room - create it
	e.saveState()

	nx, ny, nz := fromRoom.X, fromRoom.Y, fromRoom.Z
	switch e.pendingDir {
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
		ID:              uuid.New().String(),
		Name:            "New Room",
		Description:     roomData.RawText,
		DescriptionHash: hash,
		Exits:           make(map[Direction]string),
		X:               nx,
		Y:               ny,
		Z:               nz,
	}

	// Link rooms
	fromRoom.Exits[e.pendingDir] = newRoom.ID
	reverse := ReverseDirection(e.pendingDir)
	if reverse != "" {
		newRoom.Exits[reverse] = e.pendingFromRoom
	}

	e.data.Rooms[newRoom.ID] = newRoom
	e.hashIndex[hash] = newRoom.ID
	e.data.CurrentRoom = newRoom.ID

	e.pendingDir = ""
	e.pendingFromRoom = ""

	return true, newRoom.Name, false, nil
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
