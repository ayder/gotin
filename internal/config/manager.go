package config

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// TriggerConfig represents a trigger's serializable form.
type TriggerConfig struct {
	Pattern  string `json:"pattern"`
	Response string `json:"response"`
}

// Config represents the application configuration.
type Config struct {
	// Aliases maps alias keys to their expanded values.
	Aliases map[string]string `json:"aliases,omitempty"`
	// Triggers contains pattern-response pairs.
	Triggers []TriggerConfig `json:"triggers,omitempty"`
	// LastHost is the last connected server host.
	LastHost string `json:"last_host,omitempty"`
	// LastPort is the last connected server port.
	LastPort int `json:"last_port,omitempty"`
}

// DefaultConfigDir returns the default configuration directory (~/.gotin).
func DefaultConfigDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".gotin"), nil
}

// DefaultConfigPath returns the default configuration file path.
func DefaultConfigPath() (string, error) {
	dir, err := DefaultConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.json"), nil
}

// Manager handles loading and saving configuration.
type Manager struct {
	configPath string
}

// NewManager creates a new config manager with the default path.
func NewManager() (*Manager, error) {
	path, err := DefaultConfigPath()
	if err != nil {
		return nil, err
	}
	return &Manager{configPath: path}, nil
}

// NewManagerWithPath creates a new config manager with a custom path.
func NewManagerWithPath(path string) *Manager {
	return &Manager{configPath: path}
}

// Load reads the configuration from disk.
// Returns an empty Config if the file doesn't exist.
func (m *Manager) Load() (Config, error) {
	cfg := Config{
		Aliases:  make(map[string]string),
		Triggers: make([]TriggerConfig, 0),
	}

	data, err := os.ReadFile(m.configPath)
	if err != nil {
		if os.IsNotExist(err) {
			// File doesn't exist, return empty config
			return cfg, nil
		}
		return cfg, err
	}

	if err := json.Unmarshal(data, &cfg); err != nil {
		return cfg, err
	}

	// Ensure maps are initialized
	if cfg.Aliases == nil {
		cfg.Aliases = make(map[string]string)
	}
	if cfg.Triggers == nil {
		cfg.Triggers = make([]TriggerConfig, 0)
	}

	return cfg, nil
}

// Save writes the configuration to disk.
// Creates the directory if it doesn't exist.
func (m *Manager) Save(cfg Config) error {
	// Ensure directory exists
	dir := filepath.Dir(m.configPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(m.configPath, data, 0644)
}

// Exists checks if the configuration file exists.
func (m *Manager) Exists() bool {
	_, err := os.Stat(m.configPath)
	return err == nil
}

// Path returns the configuration file path.
func (m *Manager) Path() string {
	return m.configPath
}
