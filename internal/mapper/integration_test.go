package mapper

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ayder/gotin/internal/mudproto/mxp"
)

func loadFixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(b)
}

func drive(t *testing.T, fixtures []string) []RoomBlock {
	t.Helper()
	cp, err := DefaultT2TMUDProfile().Compile()
	if err != nil {
		t.Fatalf("compile profile: %v", err)
	}
	var emitted []RoomBlock
	buf := NewRoomBlockBuffer(cp, func(rb RoomBlock) { emitted = append(emitted, rb) })

	p := mxp.New()
	p.SetSink(buf)
	for _, name := range fixtures {
		raw := loadFixture(t, name)
		_ = p.Filter(raw)
	}
	return emitted
}

func TestIntegration_PlainsFiveTilesProduceFiveDistinctRooms(t *testing.T) {
	blocks := drive(t, []string{
		"t2tmud_plains_north_ne.txt",
		"t2tmud_plains_nw_n.txt",
		"t2tmud_plains_nw.txt",
		"t2tmud_plains_landlocked.txt",
		"t2tmud_plains_white_towers_se.txt",
	})
	if len(blocks) != 5 {
		t.Fatalf("expected 5 RoomBlocks, got %d", len(blocks))
	}

	hashes := make(map[string]bool)
	for i, b := range blocks {
		h := ComputeStructuralHash(b.Description, b.Exits)
		if hashes[h] {
			t.Errorf("tile %d hash collision with earlier tile: %s", i, h)
		}
		hashes[h] = true
	}
	if len(hashes) != 5 {
		t.Errorf("expected 5 distinct hashes, got %d", len(hashes))
	}
}

func TestIntegration_DescriptionDropsWeatherKeepsSights(t *testing.T) {
	blocks := drive(t, []string{"t2tmud_plains_north_ne.txt"})
	if len(blocks) != 1 {
		t.Fatalf("len(blocks) = %d, want 1", len(blocks))
	}
	d := blocks[0].Description
	if !strings.Contains(d, "Gently rolling plains") {
		t.Errorf("lost static description: %q", d)
	}
	if !strings.Contains(d, "Lune River") {
		t.Errorf("lost nearby-sight: %q", d)
	}
	if strings.Contains(d, "The sky is") {
		t.Errorf("kept weather: %q", d)
	}
	if strings.Contains(d, "There is water to") {
		t.Errorf("kept water-exits sentence: %q", d)
	}
	if strings.Contains(d, "obvious exits") {
		t.Errorf("kept exits sentence: %q", d)
	}
}

func TestIntegration_WhiteTowersDistinctFromPlains(t *testing.T) {
	plains := drive(t, []string{"t2tmud_plains_white_towers_se.txt"})
	heath := drive(t, []string{"t2tmud_white_towers_room.txt"})
	if len(plains) != 1 || len(heath) != 1 {
		t.Fatalf("len plains=%d heath=%d", len(plains), len(heath))
	}
	hp := ComputeStructuralHash(plains[0].Description, plains[0].Exits)
	hh := ComputeStructuralHash(heath[0].Description, heath[0].Exits)
	if hp == hh {
		t.Errorf("plains and heath collided: %s", hp)
	}
}

func TestIntegration_PresenceDropped(t *testing.T) {
	blocks := drive(t, []string{"t2tmud_paved_street.txt"})
	if len(blocks) != 1 {
		t.Fatalf("len(blocks) = %d, want 1", len(blocks))
	}
	if strings.Contains(blocks[0].Description, "An injured seagull") {
		t.Errorf("presence leaked into description: %q", blocks[0].Description)
	}
	if len(blocks[0].Presence) == 0 {
		t.Errorf("presence was not captured")
	}
}

func TestT2tmudTriangle(t *testing.T) {
	e := NewEngine("")
	if err := e.Create(""); err != nil {
		t.Fatalf("Create: %v", err)
	}
	e.SetMappingOptions(MappingOptions{Vnum: false, Hash: false})
	e.MarkMXPSeen()
	e.MarkBlockStartSeen()
	e.StartAutoMapping()

	raw, err := os.ReadFile(filepath.Join("testdata", "triangle_blocks.json"))
	if err != nil {
		t.Fatalf("fixture: %v", err)
	}
	var blocks []RoomBlock
	if err := json.Unmarshal(raw, &blocks); err != nil {
		t.Fatalf("decode fixture: %v", err)
	}
	if len(blocks) != 4 {
		t.Fatalf("fixture blocks = %d; want 4", len(blocks))
	}

	start := e.GetCurrent()
	start.Name = blocks[0].Name
	start.Description = blocks[0].Description
	startID := start.ID

	walk := func(input string, b RoomBlock) {
		t.Helper()
		e.MarkMXPSeen()
		e.MarkBlockStartSeen()
		if _, _, err := e.ProcessMovement(input); err != nil {
			t.Fatalf("ProcessMovement(%q): %v", input, err)
		}
		if _, _, _, err := e.HandleRoomBlock(b); err != nil {
			t.Fatalf("HandleRoomBlock(%q): %v", input, err)
		}
	}

	walk("s", blocks[1])
	southID := e.GetCurrent().ID
	walk("nw", blocks[2])
	nwID := e.GetCurrent().ID
	walk("e", blocks[3])

	if got := len(e.data.Rooms); got != 3 {
		t.Fatalf("rooms = %d; want 3", got)
	}
	if got := e.GetCurrent().ID; got != startID {
		t.Errorf("current = %q; want start %q", got, startID)
	}
	if got := e.data.Rooms[startID].Exits[South]; got != southID {
		t.Errorf("start south exit = %q; want %q", got, southID)
	}
	if got := e.data.Rooms[southID].Exits[NorthWest]; got != nwID {
		t.Errorf("south northwest exit = %q; want %q", got, nwID)
	}
	if got := e.data.Rooms[nwID].Exits[East]; got != startID {
		t.Errorf("nw east exit = %q; want %q", got, startID)
	}
}
