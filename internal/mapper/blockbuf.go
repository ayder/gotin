package mapper

import (
	"regexp"
	"strings"
)

// RoomBlock holds the parsed output of one MXP-driven room render.
type RoomBlock struct {
	Description string   // Cleaned static description (post-strip, ANSI removed).
	Exits       []string // Normalised short-form exit directions, in tag order, deduplicated.
	Presence    []string // Trimmed text of any presence-tag-marked lines.
	Vnum        string   // Empty unless a future profile populates it.
	Name        string   // From <ROOMNAME> if observed; otherwise empty.
}

type taggedFragment struct {
	name    string
	body    string
	textPos int
}

// RoomBlockBuffer accumulates MXP tag callbacks and stripped text between
// a profile.BlockStartTag and a match against profile.BlockEndPattern.
// It is single-goroutine-safe: callers must not concurrently invoke OnTag,
// OnText, or Reset.
type RoomBlockBuffer struct {
	cp      *CompiledProfile
	open    bool
	text    strings.Builder
	tags    []taggedFragment
	onClose func(RoomBlock)
}

// NewRoomBlockBuffer constructs a buffer that calls onClose with a parsed
// RoomBlock each time a complete block is observed.
func NewRoomBlockBuffer(cp *CompiledProfile, onClose func(RoomBlock)) *RoomBlockBuffer {
	return &RoomBlockBuffer{cp: cp, onClose: onClose}
}

// Reset clears any in-flight block (called on disconnect or manual abort).
func (b *RoomBlockBuffer) Reset() {
	b.open = false
	b.text.Reset()
	b.tags = b.tags[:0]
}

// OnTag records an MXP tag observation. The block-start tag opens (or
// re-opens) a fresh block; other tags are appended only while the buffer
// is open.
func (b *RoomBlockBuffer) OnTag(name, body string) {
	if name == b.cp.Profile.BlockStartTag {
		b.text.Reset()
		b.tags = b.tags[:0]
		b.open = true
		return
	}
	if !b.open {
		return
	}
	b.tags = append(b.tags, taggedFragment{name: name, body: body, textPos: b.text.Len()})
}

// OnText appends post-MXP-strip text to the in-flight block. When the
// accumulated text contains a match for profile.BlockEndPattern, the block is
// parsed and emitted via the onClose callback.
func (b *RoomBlockBuffer) OnText(s string) {
	if !b.open {
		return
	}
	b.text.WriteString(s)
	full := b.text.String()
	if loc := b.cp.EndRe.FindStringIndex(full); loc != nil {
		body := full[:loc[0]]
		rb := b.parse(body)
		b.open = false
		b.text.Reset()
		b.tags = b.tags[:0]
		if b.onClose != nil {
			b.onClose(rb)
		}
	}
}

// ansiRe matches CSI sequences of the form ESC [ <params> <final-byte>.
var ansiRe = regexp.MustCompile(`\x1b\[[0-9;]*[A-Za-z]`)

// stripANSI removes CSI / mode escape sequences from s.
func stripANSI(s string) string {
	return ansiRe.ReplaceAllString(s, "")
}

func (b *RoomBlockBuffer) parse(body string) RoomBlock {
	prof := b.cp.Profile

	exitSet := make(map[string]struct{}, len(prof.ExitTags))
	for _, t := range prof.ExitTags {
		exitSet[t] = struct{}{}
	}
	presenceSet := make(map[string]struct{}, len(prof.PresenceTags))
	for _, t := range prof.PresenceTags {
		presenceSet[t] = struct{}{}
	}

	var exits []string
	seenExits := make(map[string]struct{})
	for i, t := range b.tags {
		if _, isExit := exitSet[t.name]; !isExit {
			continue
		}
		closePos := len(body)
		for j := i + 1; j < len(b.tags); j++ {
			if b.tags[j].name == "/"+t.name {
				closePos = b.tags[j].textPos
				break
			}
		}
		if t.textPos > len(body) {
			continue
		}
		raw := body[t.textPos:min(closePos, len(body))]
		raw = strings.TrimSpace(stripANSI(raw))
		if raw == "" {
			continue
		}
		dir, ok := ParseDirection(raw)
		if !ok {
			continue
		}
		if _, dup := seenExits[string(dir)]; dup {
			continue
		}
		seenExits[string(dir)] = struct{}{}
		exits = append(exits, string(dir))
	}

	lineStarts := []int{0}
	for i := 0; i < len(body); i++ {
		if body[i] == '\n' {
			lineStarts = append(lineStarts, i+1)
		}
	}
	lineForOffset := func(off int) int {
		lo, hi := 0, len(lineStarts)-1
		for lo < hi {
			mid := (lo + hi + 1) / 2
			if lineStarts[mid] <= off {
				lo = mid
			} else {
				hi = mid - 1
			}
		}
		return lo
	}

	dropLines := make(map[int]bool)
	var presence []string
	rawLines := strings.Split(body, "\n")
	for _, t := range b.tags {
		if _, isPres := presenceSet[t.name]; !isPres {
			continue
		}
		idx := lineForOffset(t.textPos)
		if idx < 0 || idx >= len(rawLines) {
			continue
		}
		if !dropLines[idx] {
			plain := strings.TrimSpace(stripANSI(rawLines[idx]))
			if plain != "" {
				presence = append(presence, plain)
			}
		}
		dropLines[idx] = true
	}

	var keep []string
	for i, line := range rawLines {
		if dropLines[i] {
			continue
		}
		plain := strings.TrimSpace(stripANSI(line))
		if plain == "" {
			continue
		}
		if matchesAny(plain, b.cp.WeatherR) {
			continue
		}
		if b.cp.DoorR != nil && b.cp.DoorR.MatchString(plain) {
			continue
		}
		if strings.HasPrefix(plain, "The only obvious exit") {
			continue
		}
		if strings.HasPrefix(plain, "There is water to the ") {
			continue
		}
		keep = append(keep, plain)
	}

	return RoomBlock{
		Description: strings.Join(keep, "\n"),
		Exits:       exits,
		Presence:    presence,
	}
}

func matchesAny(s string, rs []*regexp.Regexp) bool {
	for _, r := range rs {
		if r.MatchString(s) {
			return true
		}
	}
	return false
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
