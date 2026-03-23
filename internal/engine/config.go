//go:build windows

// Package engine provides the shared wallpaper rendering engine used by both
// the CLI (cmd/livewallpaper) and the GUI (cmd/gowallpaper-gui).
package engine

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Config holds all wallpaper engine settings.
type Config struct {
	// VideoPath is the absolute path to the video file.
	VideoPath string `json:"videoPath"`
	// Mode controls frame scaling: "cover", "contain", or "stretch".
	Mode string `json:"mode"`
	// Loop restarts playback at end-of-stream when true.
	Loop bool `json:"loop"`
	// FPSLimit caps the render rate (0 means unlimited).
	FPSLimit int `json:"fpsLimit"`
	// LastDir is the last folder the user browsed (GUI preference).
	LastDir string `json:"lastDir,omitempty"`
}

// DefaultAppDataConfigPath returns the path
// %APPDATA%\GoWallpaper\config.json (or the OS equivalent).
func DefaultAppDataConfigPath() string {
	dir := os.Getenv("APPDATA")
	if dir == "" {
		var err error
		dir, err = os.UserConfigDir()
		if err != nil {
			dir = "."
		}
	}
	return filepath.Join(dir, "GoWallpaper", "config.json")
}

// LoadConfig reads and parses the JSON configuration file at path.
// Missing or zero fields receive sensible defaults.
func LoadConfig(path string) (*Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var cfg Config
	if err := json.NewDecoder(f).Decode(&cfg); err != nil {
		return nil, err
	}
	applyDefaults(&cfg)
	return &cfg, nil
}

// SaveConfig writes cfg to path as pretty-printed JSON,
// creating any parent directories that do not yet exist.
func SaveConfig(cfg *Config, path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	return enc.Encode(cfg)
}

func applyDefaults(cfg *Config) {
	if cfg.FPSLimit <= 0 {
		cfg.FPSLimit = 30
	}
	if cfg.Mode == "" {
		cfg.Mode = "cover"
	}
}
