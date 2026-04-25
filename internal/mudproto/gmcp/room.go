package gmcp

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// RoomInfo holds parsed GMCP Room.Info data.
// MUDs use different field names; this struct normalises the common variants.
type RoomInfo struct {
	Vnum        string   // Stable room identifier (num/id/vnum)
	Name        string   // Room name
	Description string   // Room description
	Area        string   // Optional area/zone name
	Exits       []string // Available exit directions (lowercased)
}

// ParseRoomInfo parses a GMCP Room.Info JSON payload.
// It is intentionally forgiving with field-name variations.
func ParseRoomInfo(payload []byte) (*RoomInfo, error) {
	// Fast-path: many MUDs send a flat object.
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(payload, &raw); err != nil {
		return nil, fmt.Errorf("json unmarshal: %w", err)
	}

	r := &RoomInfo{}

	// Vnum / num / id
	if v, ok := raw["num"]; ok {
		r.Vnum = string(trimQuotes(v))
	} else if v, ok := raw["id"]; ok {
		r.Vnum = string(trimQuotes(v))
	} else if v, ok := raw["vnum"]; ok {
		r.Vnum = string(trimQuotes(v))
	}

	// Name / title
	if v, ok := raw["name"]; ok {
		r.Name = string(trimQuotes(v))
	} else if v, ok := raw["title"]; ok {
		r.Name = string(trimQuotes(v))
	}

	// Description / desc
	if v, ok := raw["desc"]; ok {
		r.Description = string(trimQuotes(v))
	} else if v, ok := raw["description"]; ok {
		r.Description = string(trimQuotes(v))
	}

	// Area / zone
	if v, ok := raw["area"]; ok {
		r.Area = string(trimQuotes(v))
	} else if v, ok := raw["zone"]; ok {
		r.Area = string(trimQuotes(v))
	}

	// Exits — can be a map (direction -> target) or an array of strings.
	if v, ok := raw["exits"]; ok {
		r.Exits = parseExits(v)
	}

	return r, nil
}

// trimQuotes removes surrounding JSON quotes from a RawMessage when it is a
// plain string literal. Non-string values (numbers) are left as-is.
func trimQuotes(rm json.RawMessage) []byte {
	if len(rm) >= 2 && rm[0] == '"' && rm[len(rm)-1] == '"' {
		return rm[1 : len(rm)-1]
	}
	return rm
}

// parseExits handles both {"n":"123","e":"456"} and ["n","e","s"].
func parseExits(raw json.RawMessage) []string {
	// Try map first.
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err == nil {
		out := make([]string, 0, len(m))
		for k := range m {
			out = append(out, jsonDirToDir(k))
		}
		return out
	}

	// Try array of strings.
	var arr []string
	if err := json.Unmarshal(raw, &arr); err == nil {
		out := make([]string, 0, len(arr))
		for _, s := range arr {
			out = append(out, jsonDirToDir(s))
		}
		return out
	}

	// Try array of numbers (rare, but possible for direction codes).
	var nums []int
	if err := json.Unmarshal(raw, &nums); err == nil {
		out := make([]string, 0, len(nums))
		for _, n := range nums {
			out = append(out, strconv.Itoa(n))
		}
		return out
	}

	return nil
}

// jsonDirToDir normalises common direction strings to the mapper's short form.
func jsonDirToDir(s string) string {
	switch strings.ToLower(s) {
	case "north", "n":
		return "n"
	case "south", "s":
		return "s"
	case "east", "e":
		return "e"
	case "west", "w":
		return "w"
	case "northeast", "ne":
		return "ne"
	case "southwest", "sw":
		return "sw"
	case "northwest", "nw":
		return "nw"
	case "southeast", "se":
		return "se"
	case "up", "u":
		return "u"
	case "down", "d":
		return "d"
	case "in":
		return "in"
	case "out":
		return "out"
	default:
		return s
	}
}
