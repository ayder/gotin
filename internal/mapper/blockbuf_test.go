package mapper

import (
	"strings"
	"testing"
)

func TestNewRoomBlockBuffer(t *testing.T) {
	p := DefaultT2TMUDProfile()
	cp, err := p.Compile()
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}

	called := false
	b := NewRoomBlockBuffer(cp, func(rb RoomBlock) { called = true })
	if b == nil {
		t.Fatal("NewRoomBlockBuffer returned nil")
	}
	if called {
		t.Errorf("onClose fired on construction")
	}
}

func TestRoomBlockBuffer_OnTag_BlockStartOpens(t *testing.T) {
	p := DefaultT2TMUDProfile()
	cp, _ := p.Compile()
	b := NewRoomBlockBuffer(cp, func(RoomBlock) {})
	b.OnTag("expire", "expire")
	if !b.open {
		t.Errorf("buffer not open after BlockStartTag")
	}
}

func TestRoomBlockBuffer_OnTag_IgnoredWhenClosed(t *testing.T) {
	p := DefaultT2TMUDProfile()
	cp, _ := p.Compile()
	b := NewRoomBlockBuffer(cp, func(RoomBlock) {})
	b.OnTag("x", "x")
	if len(b.tags) != 0 {
		t.Errorf("tag recorded while buffer closed: %+v", b.tags)
	}
}

func TestRoomBlockBuffer_OnTag_ReopenDiscardsPartial(t *testing.T) {
	p := DefaultT2TMUDProfile()
	cp, _ := p.Compile()
	b := NewRoomBlockBuffer(cp, func(RoomBlock) {})
	b.OnTag("expire", "expire")
	b.OnTag("x", "x")
	b.OnTag("expire", "expire")
	if !b.open {
		t.Errorf("buffer should remain open after re-open")
	}
	if len(b.tags) != 0 {
		t.Errorf("tags survived re-open: %+v", b.tags)
	}
	if b.text.Len() != 0 {
		t.Errorf("text survived re-open: %q", b.text.String())
	}
}

func TestRoomBlockBuffer_OnTag_AppendsWhileOpen(t *testing.T) {
	p := DefaultT2TMUDProfile()
	cp, _ := p.Compile()
	b := NewRoomBlockBuffer(cp, func(RoomBlock) {})
	b.OnTag("expire", "expire")
	b.OnTag("x", "x")
	b.OnTag("/x", "/x")
	if len(b.tags) != 2 {
		t.Errorf("len(tags) = %d, want 2", len(b.tags))
	}
}

func TestRoomBlockBuffer_OnText_AccumulatesWhileOpen(t *testing.T) {
	p := DefaultT2TMUDProfile()
	cp, _ := p.Compile()
	b := NewRoomBlockBuffer(cp, func(RoomBlock) {})
	b.OnTag("expire", "expire")
	b.OnText("    A room.")
	b.OnText("\nThe sky is dark blue.")
	if b.text.String() != "    A room.\nThe sky is dark blue." {
		t.Errorf("text not accumulated: %q", b.text.String())
	}
}

func TestRoomBlockBuffer_OnText_IgnoresWhileClosed(t *testing.T) {
	p := DefaultT2TMUDProfile()
	cp, _ := p.Compile()
	b := NewRoomBlockBuffer(cp, func(RoomBlock) {})
	b.OnText("ambient flavor before any block")
	if b.text.Len() != 0 {
		t.Errorf("text accumulated while closed: %q", b.text.String())
	}
}

func TestRoomBlockBuffer_OnText_PromptClosesBlock(t *testing.T) {
	p := DefaultT2TMUDProfile()
	cp, _ := p.Compile()
	var emitted []RoomBlock
	b := NewRoomBlockBuffer(cp, func(rb RoomBlock) { emitted = append(emitted, rb) })
	b.OnTag("expire", "expire")
	b.OnText("    A room.\nThe sky is dark blue.\n    The only obvious exit is north.")
	b.OnText("\nHP:70 EP:70 [WA] > ")
	if len(emitted) != 1 {
		t.Fatalf("expected exactly 1 emission, got %d", len(emitted))
	}
	if b.open {
		t.Errorf("buffer should be closed after emit")
	}
}

func TestRoomBlockBuffer_OnText_PromptWithANSIClosesBlock(t *testing.T) {
	p := DefaultT2TMUDProfile()
	cp, _ := p.Compile()
	var emitted []RoomBlock
	b := NewRoomBlockBuffer(cp, func(rb RoomBlock) { emitted = append(emitted, rb) })
	b.OnTag("expire", "expire")
	b.OnText("    A room.\n    The only obvious exit is north.")
	b.OnText("\nHP:70 EP:70 [\x1b[1;32mWA\x1b[0m] > ")
	if len(emitted) != 1 {
		t.Fatalf("expected exactly 1 emission with ANSI prompt, got %d", len(emitted))
	}
	if b.open {
		t.Errorf("buffer should be closed after ANSI prompt")
	}
}

func TestStripANSI(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"plain", "plain"},
		{"\x1b[0;32mgreen\x1b[0m", "green"},
		{"a\x1b[1;37mword\x1b[0m b", "aword b"},
		{"\x1b[4z<x>east</x>", "<x>east</x>"},
		{"", ""},
	}
	for _, c := range cases {
		got := stripANSI(c.in)
		if got != c.want {
			t.Errorf("stripANSI(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestRoomBlockBuffer_ParsesPlainsTile(t *testing.T) {
	p := DefaultT2TMUDProfile()
	cp, _ := p.Compile()
	var got RoomBlock
	b := NewRoomBlockBuffer(cp, func(rb RoomBlock) { got = rb })

	b.OnTag("expire", "expire")
	b.OnText(
		"    Gently rolling plains extend into the distance.\n" +
			"This tranquil plain manifests a sense of peace.\n" +
			"The Lune River lies north and northeast.\n" +
			"The sky is dark blue and a yellow glow comes from the west.\n" +
			"A crystal clear sky hovers over the landscape.\n" +
			"    There is water to the ",
	)
	b.OnTag("w", "w")
	b.OnText("north")
	b.OnTag("/w", "/w")
	b.OnText(" and ")
	b.OnTag("w", "w")
	b.OnText("northeast")
	b.OnTag("/w", "/w")
	b.OnText(".\n    The only obvious exits are ")
	b.OnTag("x", "x")
	b.OnText("east")
	b.OnTag("/x", "/x")
	b.OnText(", ")
	b.OnTag("x", "x")
	b.OnText("south")
	b.OnTag("/x", "/x")
	b.OnText(".\n")
	b.OnTag("i30", `i30 "orc 1"`)
	b.OnText("An ugly orc")
	b.OnTag("/i30", "/i30")
	b.OnText("\nHP:70 EP:70 [WA] > ")

	if !sliceEq(got.Exits, []string{"n", "ne", "e", "s"}) {
		t.Errorf("Exits = %v, want [n ne e s]", got.Exits)
	}
	if !strings.Contains(got.Description, "Gently rolling plains") {
		t.Errorf("description missing static prose: %q", got.Description)
	}
	if !strings.Contains(got.Description, "Lune River") {
		t.Errorf("description must keep nearby-sight: %q", got.Description)
	}
	if strings.Contains(got.Description, "The sky is") {
		t.Errorf("description should drop weather: %q", got.Description)
	}
	if strings.Contains(got.Description, "ugly orc") {
		t.Errorf("description should drop presence line: %q", got.Description)
	}
	if strings.Contains(got.Description, "obvious exits") {
		t.Errorf("description should drop exits sentence: %q", got.Description)
	}
	if strings.Contains(got.Description, "There is water") {
		t.Errorf("description should drop water sentence: %q", got.Description)
	}
	if len(got.Presence) != 1 || !strings.Contains(got.Presence[0], "ugly orc") {
		t.Errorf("Presence = %v, want one orc line", got.Presence)
	}
}

func sliceEq(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
