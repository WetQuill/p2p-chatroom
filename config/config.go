package config

import (
	"encoding/json"
	"fmt"
	"os"
	"time"
)

type Config struct {
	// Node Configuration
	Port           int    `json:"port"`           // 0 for auto-discovery
	UserName       string `json:"userName"`       // User display name
	Mode           string `json:"mode"`           // "local" or "remote"

	// Network Configuration
	SignalingServer string   `json:"signalingServer"` // e.g., "ws://localhost:9000/ws"
	STUNServers    []string `json:"stunServers"`       // STUN servers for NAT traversal

	// Legacy Support
	EnableLocalMode bool `json:"enableLocalMode"` // Keep localhost connections
}

// Load reads configuration from a JSON file
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}

// Default returns a configuration with sensible defaults
func Default() *Config {
	return &Config{
		Port:           0, // Auto-discovery
		UserName:       fmt.Sprintf("User-%d", time.Now().Unix()%1000),
		Mode:           "local",
		SignalingServer: "",
		STUNServers: []string{
			"stun:stun.l.google.com:19302",
			"stun:stun1.l.google.com:19302",
		},
		EnableLocalMode: true,
	}
}

// Validate checks if the configuration is valid
func (c *Config) Validate() error {
	if c.Mode != "local" && c.Mode != "remote" {
		return fmt.Errorf("invalid mode: %s (must be 'local' or 'remote')", c.Mode)
	}

	if c.Mode == "remote" && c.SignalingServer == "" {
		return fmt.Errorf("signalingServer is required for remote mode")
	}

	return nil
}
