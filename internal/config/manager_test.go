package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestManager_SaveAndLoad(t *testing.T) {
	// Create a temp directory for testing
	tmpDir, err := os.MkdirTemp("", "termud-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	configPath := filepath.Join(tmpDir, "config.json")
	mgr := NewManagerWithPath(configPath)

	// Create test config
	cfg := Config{
		Aliases: map[string]string{
			"k": "kill",
			"l": "look",
		},
		Triggers: []TriggerConfig{
			{Pattern: "hello", Response: "world"},
		},
		LastHost: "test.mud.org",
		LastPort: 4000,
	}

	// Save config
	if err := mgr.Save(cfg); err != nil {
		t.Fatalf("Failed to save config: %v", err)
	}

	// Verify file exists
	if !mgr.Exists() {
		t.Error("Expected config file to exist after save")
	}

	// Load config
	loaded, err := mgr.Load()
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	// Verify loaded data
	if loaded.LastHost != cfg.LastHost {
		t.Errorf("LastHost = %q, want %q", loaded.LastHost, cfg.LastHost)
	}
	if loaded.LastPort != cfg.LastPort {
		t.Errorf("LastPort = %d, want %d", loaded.LastPort, cfg.LastPort)
	}
	if len(loaded.Aliases) != len(cfg.Aliases) {
		t.Errorf("Aliases count = %d, want %d", len(loaded.Aliases), len(cfg.Aliases))
	}
	if loaded.Aliases["k"] != "kill" {
		t.Errorf("Aliases[k] = %q, want 'kill'", loaded.Aliases["k"])
	}
	if len(loaded.Triggers) != len(cfg.Triggers) {
		t.Errorf("Triggers count = %d, want %d", len(loaded.Triggers), len(cfg.Triggers))
	}
}

func TestManager_LoadNonExistent(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "termud-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	configPath := filepath.Join(tmpDir, "nonexistent.json")
	mgr := NewManagerWithPath(configPath)

	// Load should return empty config, not error
	cfg, err := mgr.Load()
	if err != nil {
		t.Fatalf("Load of non-existent file should not error: %v", err)
	}

	// Should have initialized maps
	if cfg.Aliases == nil {
		t.Error("Expected Aliases to be initialized")
	}
	if cfg.Triggers == nil {
		t.Error("Expected Triggers to be initialized")
	}
}

func TestManager_SaveCreatesDirectory(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "termud-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Use a nested path that doesn't exist
	configPath := filepath.Join(tmpDir, "nested", "dir", "config.json")
	mgr := NewManagerWithPath(configPath)

	cfg := Config{
		Aliases: map[string]string{"test": "value"},
	}

	// Save should create the directory
	if err := mgr.Save(cfg); err != nil {
		t.Fatalf("Failed to save with nested directory: %v", err)
	}

	// Verify file exists
	if !mgr.Exists() {
		t.Error("Expected config file to exist")
	}
}

func TestManager_Path(t *testing.T) {
	customPath := "/custom/path/config.json"
	mgr := NewManagerWithPath(customPath)

	if mgr.Path() != customPath {
		t.Errorf("Path() = %q, want %q", mgr.Path(), customPath)
	}
}

func TestDefaultConfigPath(t *testing.T) {
	path, err := DefaultConfigPath()
	if err != nil {
		t.Fatalf("DefaultConfigPath failed: %v", err)
	}

	// Should end with .termud/config.json
	if filepath.Base(path) != "config.json" {
		t.Errorf("Expected path to end with config.json, got %s", filepath.Base(path))
	}
	if filepath.Base(filepath.Dir(path)) != ".termud" {
		t.Errorf("Expected parent dir to be .termud, got %s", filepath.Base(filepath.Dir(path)))
	}
}
