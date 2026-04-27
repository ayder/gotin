package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/ayder/gotin/internal/input"
	"github.com/ayder/gotin/internal/mapper"
)

func TestGotinDataMappingOptions_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })

	opts := mapper.MappingOptions{Vnum: false, Hash: true}
	td := gotinData{MappingOptions: &opts}
	blob, err := json.MarshalIndent(td, "", "  ")
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, gotinDataFilename), blob, 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	var got gotinData
	raw, err := os.ReadFile(filepath.Join(dir, gotinDataFilename))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if got.MappingOptions == nil {
		t.Fatalf("MappingOptions is nil after round trip")
	}
	if *got.MappingOptions != opts {
		t.Errorf("got %+v, want %+v", *got.MappingOptions, opts)
	}
}

// TestGotinDataMappingOptions_ExplicitNone covers the {Vnum:false,Hash:false}
// case: omitempty must NOT drop the inner fields, so a deliberate "none"
// setting survives a round trip.
func TestGotinDataMappingOptions_ExplicitNone(t *testing.T) {
	opts := mapper.MappingOptions{Vnum: false, Hash: false}
	td := gotinData{MappingOptions: &opts}
	blob, err := json.Marshal(td)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var got gotinData
	if err := json.Unmarshal(blob, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if got.MappingOptions == nil {
		t.Fatalf("MappingOptions is nil after round trip; explicit-none was dropped")
	}
	if *got.MappingOptions != opts {
		t.Errorf("got %+v, want %+v", *got.MappingOptions, opts)
	}
}

// TestGotinDataMappingOptions_AbsentStaysNil confirms a config file without
// any mapping_options field unmarshals to nil (caller falls back to default).
func TestGotinDataMappingOptions_AbsentStaysNil(t *testing.T) {
	blob := []byte(`{"aliases":{"l":"look"}}`)
	var got gotinData
	if err := json.Unmarshal(blob, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if got.MappingOptions != nil {
		t.Errorf("MappingOptions = %+v, want nil for absent field", *got.MappingOptions)
	}
}

// TestGotinDataMappingOptions_PerConnection verifies per-alias mapping_options
// round-trips alongside the rest of the connection alias.
func TestGotinDataMappingOptions_PerConnection(t *testing.T) {
	opts := mapper.MappingOptions{Vnum: true, Hash: true}
	td := gotinData{
		Connections: map[string]input.ConnectionAlias{
			"t2t": {
				Host:           "t2tmud.org",
				Port:           9999,
				Auto:           true,
				MappingOptions: &opts,
			},
		},
	}
	blob, err := json.Marshal(td)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var got gotinData
	if err := json.Unmarshal(blob, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	ca, ok := got.Connections["t2t"]
	if !ok {
		t.Fatalf("connection alias t2t missing after round trip")
	}
	if ca.MappingOptions == nil {
		t.Fatalf("alias MappingOptions is nil after round trip")
	}
	if *ca.MappingOptions != opts {
		t.Errorf("got %+v, want %+v", *ca.MappingOptions, opts)
	}
}
