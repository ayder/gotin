package mapper

import (
	"reflect"
	"testing"
)

func TestDefaultT2TMUDProfile(t *testing.T) {
	p := DefaultT2TMUDProfile()
	if p.Host != "t2tmud.org" {
		t.Errorf("Host = %q, want t2tmud.org", p.Host)
	}
	if p.BlockStartTag != "expire" {
		t.Errorf("BlockStartTag = %q, want expire", p.BlockStartTag)
	}
	wantExitTags := []string{"x", "xx", "t", "w", "ww", "l", "ll", "y", "yy", "z"}
	if !reflect.DeepEqual(p.ExitTags, wantExitTags) {
		t.Errorf("ExitTags = %v, want %v", p.ExitTags, wantExitTags)
	}
	wantPresence := []string{"i30", "i9"}
	if !reflect.DeepEqual(p.PresenceTags, wantPresence) {
		t.Errorf("PresenceTags = %v, want %v", p.PresenceTags, wantPresence)
	}
	if len(p.WeatherPatterns) < 2 {
		t.Errorf("WeatherPatterns: want at least 2, got %d", len(p.WeatherPatterns))
	}
	if p.DoorStatePattern == "" {
		t.Errorf("DoorStatePattern is empty")
	}
	if p.BlockEndPattern == "" {
		t.Errorf("BlockEndPattern is empty")
	}
}

func TestMergeProfile_EmptyOverrideReturnsBase(t *testing.T) {
	base := DefaultT2TMUDProfile()
	merged := MergeProfile(base, MUDProfile{})
	if !reflect.DeepEqual(merged, base) {
		t.Errorf("empty override mutated base: got %+v", merged)
	}
}

func TestMergeProfile_PartialOverride(t *testing.T) {
	base := DefaultT2TMUDProfile()
	override := MUDProfile{
		BlockStartTag: "myroom",
		ExitTags:      []string{"go"},
	}
	merged := MergeProfile(base, override)
	if merged.BlockStartTag != "myroom" {
		t.Errorf("BlockStartTag not overridden: %q", merged.BlockStartTag)
	}
	if !reflect.DeepEqual(merged.ExitTags, []string{"go"}) {
		t.Errorf("ExitTags not overridden: %v", merged.ExitTags)
	}
	if merged.BlockEndPattern != base.BlockEndPattern {
		t.Errorf("BlockEndPattern leaked: %q", merged.BlockEndPattern)
	}
	if !reflect.DeepEqual(merged.PresenceTags, base.PresenceTags) {
		t.Errorf("PresenceTags leaked: %v", merged.PresenceTags)
	}
}

func TestMergeProfile_HostFromOverride(t *testing.T) {
	base := DefaultT2TMUDProfile()
	override := MUDProfile{Host: "example.org"}
	merged := MergeProfile(base, override)
	if merged.Host != "example.org" {
		t.Errorf("Host = %q, want example.org", merged.Host)
	}
}

func TestMUDProfile_Compile(t *testing.T) {
	p := DefaultT2TMUDProfile()
	c, err := p.Compile()
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if c.EndRe == nil {
		t.Fatal("EndRe nil")
	}
	if !c.EndRe.MatchString("\nHP:70 EP:70 [WA] > ") {
		t.Errorf("EndRe should match captured prompt (no ANSI)")
	}
	if !c.EndRe.MatchString("\nHP:70 EP:70 [\x1b[1;32mWA\x1b[0m] > ") {
		t.Errorf("EndRe should match prompt with ANSI inside brackets")
	}
	// Real t2tmud prompt: \r\n line ending plus ANSI resets between newline
	// and "HP:". Must match — otherwise room blocks never close.
	if !c.EndRe.MatchString("south.\x1b[0m\r\n\x1b[0m\x1b[0mHP:70 EP:70 [\x1b[1;32mWA\x1b[0m] > \x1b[0m") {
		t.Errorf("EndRe should match real t2tmud prompt with \\r\\n and leading ANSI resets")
	}
	if len(c.WeatherR) != len(p.WeatherPatterns) {
		t.Fatalf("WeatherR len = %d, want %d", len(c.WeatherR), len(p.WeatherPatterns))
	}
	if !c.WeatherR[0].MatchString("The sky is dark blue.") {
		t.Errorf("WeatherR[0] should match weather sentence")
	}
	if c.DoorR == nil || !c.DoorR.MatchString("The south door is closed.") {
		t.Errorf("DoorR should match door-state sentence")
	}
}

func TestMUDProfile_Compile_BadRegex(t *testing.T) {
	p := DefaultT2TMUDProfile()
	p.WeatherPatterns = []string{`(unbalanced`}
	if _, err := p.Compile(); err == nil {
		t.Errorf("Compile should fail on bad regex")
	}
}
