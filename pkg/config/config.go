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

// Defaults for the local server: the voice baked into the llm/vllm-omni image and
// the sampler settings chosen by ear (docs/tts-engine.md). DefaultRefText is the
// exact transcript of DefaultRefAudio (llm/vllm-omni/ref_stas.txt) — change the
// two together, a mismatch degrades the clone.
const (
	DefaultBaseURL  = "http://localhost:8007/v1"
	DefaultLanguage = "ru"
	DefaultRefAudio = "file:///ref/ref_stas.wav"
	DefaultRefText  = "Это нелегко. Ты думаешь, меня окружают прекрасные люди. Лучшей командой желать нельзя. Но всё равно что-то может пройти не так. Этот мир не прощает ошибок. Он жесток."
	DefaultSteps    = 32
	DefaultGuidance = 2.0
)

// Qwen3Config configures the local OpenAI-compatible server (vLLM-Omni serving
// OmniVoice). The name is historical (the first local engine was Qwen3-TTS); the
// consumer reads this section too, so it stays. Zero fields take the defaults
// above — see WithDefaults.
type Qwen3Config struct {
	BaseURL string `json:"base_url"` // e.g. "http://localhost:8007/v1"
	Voice   string `json:"voice"`    // optional; "" => server default
	Model   string `json:"model"`    // optional

	Language string  `json:"language"`  // e.g. "ru"
	RefAudio string  `json:"ref_audio"` // reference clip as the server sees it
	RefText  string  `json:"ref_text"`  // exact transcript of ref_audio
	Steps    int     `json:"steps"`     // diffusion steps
	Guidance float64 `json:"guidance"`  // guidance scale
}

// ElevenLabsConfig configures the remote ElevenLabs backend.
type ElevenLabsConfig struct {
	APIKey  string `json:"api_key"`
	VoiceID string `json:"voice_id"`
	Model   string `json:"model"`    // "" => eleven_flash_v2_5
	BaseURL string `json:"base_url"` // "" => https://api.elevenlabs.io
}

// WithDefaults returns c with every zero field filled in, so an empty section
// speaks with the project's voice against the stock server. The reference clip
// and its transcript are filled only as a pair, and not at all when a named
// Voice is set: that selects a voice stored on the server instead.
func (c Qwen3Config) WithDefaults() Qwen3Config {
	if c.BaseURL == "" {
		c.BaseURL = DefaultBaseURL
	}
	if c.Language == "" {
		c.Language = DefaultLanguage
	}
	if c.Steps == 0 {
		c.Steps = DefaultSteps
	}
	if c.Guidance == 0 {
		c.Guidance = DefaultGuidance
	}
	if c.Voice == "" && c.RefAudio == "" && c.RefText == "" {
		c.RefAudio, c.RefText = DefaultRefAudio, DefaultRefText
	}
	return c
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

// setDefaults fills zero values. Default provider is the local server, so a
// minimal config with no cloud credentials still runs.
func (c *Config) setDefaults() {
	if c.TTS.Provider == "" {
		c.TTS.Provider = "qwen3"
	}
	c.TTS.Qwen3 = c.TTS.Qwen3.WithDefaults()
}
