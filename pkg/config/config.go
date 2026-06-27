// Package config is the TTS service's configuration schema and loader. Mirrors
// the stt repo's pkg/config shape so the two services configure the same way.
package config

import (
	"encoding/json"
	"fmt"
	"os"
)

// Config is the top-level configuration for the tts service.
type Config struct {
	TTS TTSConfig `json:"tts"`
}

// TTSConfig selects and configures the synthesis backend.
type TTSConfig struct {
	Provider   string           `json:"provider"` // "qwen3" (local) | "elevenlabs" | "fallback"
	Qwen3      Qwen3Config      `json:"qwen3"`
	ElevenLabs ElevenLabsConfig `json:"elevenlabs"`
}

// Qwen3Config configures the local Qwen3-TTS server (OpenAI-compatible).
type Qwen3Config struct {
	BaseURL string `json:"base_url"` // e.g. "http://localhost:8007/v1"
	Voice   string `json:"voice"`    // optional; "" => server default
	Model   string `json:"model"`    // optional
}

// ElevenLabsConfig configures the remote ElevenLabs backend.
type ElevenLabsConfig struct {
	APIKey  string `json:"api_key"`
	VoiceID string `json:"voice_id"`
	Model   string `json:"model"`    // "" => eleven_flash_v2_5
	BaseURL string `json:"base_url"` // "" => https://api.elevenlabs.io
}

// Load reads config.json from path (defaults to "config.json" when empty) and
// fills zero values with defaults.
func Load(path string) (*Config, error) {
	if path == "" {
		path = "config.json"
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config %s: %w", path, err)
	}
	cfg.setDefaults()
	return &cfg, nil
}

// setDefaults fills zero values. Default provider is the local Qwen3 server, so
// a minimal config with no cloud credentials still runs.
func (c *Config) setDefaults() {
	if c.TTS.Provider == "" {
		c.TTS.Provider = "qwen3"
	}
	if c.TTS.Qwen3.BaseURL == "" {
		c.TTS.Qwen3.BaseURL = "http://localhost:8007/v1"
	}
}
