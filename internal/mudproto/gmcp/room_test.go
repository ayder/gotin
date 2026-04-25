package gmcp

import (
	"testing"
)

func TestParseRoomInfo_FlatObject(t *testing.T) {
	payload := []byte(`{"num":123,"name":"Foyer","desc":"A small room.","area":"Town","exits":{"n":"456","e":"789"}}`)
	r, err := ParseRoomInfo(payload)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if r.Vnum != "123" {
		t.Fatalf("vnum: want 123 got %q", r.Vnum)
	}
	if r.Name != "Foyer" {
		t.Fatalf("name: want Foyer got %q", r.Name)
	}
	if r.Description != "A small room." {
		t.Fatalf("desc: want 'A small room.' got %q", r.Description)
	}
	if r.Area != "Town" {
		t.Fatalf("area: want Town got %q", r.Area)
	}
	if len(r.Exits) != 2 {
		t.Fatalf("exits: want 2 got %v", r.Exits)
	}
}

func TestParseRoomInfo_ArrayExits(t *testing.T) {
	payload := []byte(`{"name":"Forest","description":"Dark woods.","exits":["n","s","e","w"]}`)
	r, err := ParseRoomInfo(payload)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(r.Exits) != 4 {
		t.Fatalf("exits: want 4 got %v", r.Exits)
	}
	if r.Exits[0] != "n" {
		t.Fatalf("exit[0]: want n got %q", r.Exits[0])
	}
}

func TestParseRoomInfo_NoExits(t *testing.T) {
	payload := []byte(`{"name":"Void"}`)
	r, err := ParseRoomInfo(payload)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if r.Name != "Void" {
		t.Fatalf("name: want Void got %q", r.Name)
	}
	if len(r.Exits) != 0 {
		t.Fatalf("exits: want 0 got %v", r.Exits)
	}
}

func TestParseRoomInfo_NormalisesDirections(t *testing.T) {
	payload := []byte(`{"exits":{"north":"1","southwest":"2","Up":"3"}}`)
	r, err := ParseRoomInfo(payload)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	want := map[string]bool{"n": true, "sw": true, "u": true}
	if len(r.Exits) != len(want) {
		t.Fatalf("exits: want %v got %v", want, r.Exits)
	}
	for _, ex := range r.Exits {
		if !want[ex] {
			t.Fatalf("unexpected exit %q", ex)
		}
	}
}
