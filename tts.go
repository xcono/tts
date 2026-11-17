package tts

import (
	"context"
	"fmt"
	"sync/atomic"

	"github.com/xcono/tts/pkg/config"
)

// Status is the facade's coarse playback state, the TTS counterpart of stt's
// IsSpeaking. It lets the LLM loop know whether a turn is being spoken — useful
// for "don't start a new utterance while one is playing".
type Status int32

const (
	Ready      Status = iota // idle — no synthesis in flight
	Processing               // request sent, awaiting the first audio chunk
	Speaking                 // audio is streaming to the sink
)

func (s Status) String() string {
	switch s {
	case Ready:
		return "ready"
	case Processing:
		return "processing"
	case Speaking:
		return "speaking"
	default:
		return "status(?)"
	}
}

// Service is the importable TTS facade: it wraps a Provider and exposes Stream /
// Batch plus a Status signal. Build it with New (provider from config) or NewWith
// (inject any Provider — Client, ElevenLabs, or Fallback).
//
// One utterance at a time: Status is a single shared value, so Stream/Batch are
// not meant to be called concurrently on the same Service (you don't speak two
// things at once). Serialise calls in the consumer.
type Service struct {
	provider Provider
	status   atomic.Int32
}

// New builds the facade with the provider selected by config.
func New(cfg *config.Config) (*Service, error) {
	p, err := provider(cfg.TTS)
	if err != nil {
		return nil, err
	}
	return NewWith(p), nil
}

// NewWith wraps a caller-supplied Provider. Use it to inject a custom backend or
// a hand-built Fallback, and in tests.
func NewWith(p Provider) *Service {
	return &Service{provider: p}
}

// Stream synthesises text and calls emit with each PCM chunk as it arrives
// (play-as-you-go). Status moves Processing → Speaking (on the first chunk) →
// Ready (on return, success or error).
func (s *Service) Stream(ctx context.Context, text string, emit func(chunk []int16) error) error {
	s.status.Store(int32(Processing))
	defer s.status.Store(int32(Ready))

	first := true
	return s.provider.SynthesizeStream(ctx, text, func(chunk []int16) error {
		if first {
			first = false
			s.status.Store(int32(Speaking))
		}
		return emit(chunk)
	})
}

// Batch synthesises text and returns the whole 24 kHz mono PCM clip. It routes
// through Stream, so Status tracking is identical to the streaming path.
func (s *Service) Batch(ctx context.Context, text string) ([]int16, error) {
	out := make([]int16, 0)
	err := s.Stream(ctx, text, func(chunk []int16) error {
		out = append(out, chunk...)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// Status reports the current playback state; safe to poll from any goroutine.
func (s *Service) Status() Status { return Status(s.status.Load()) }

// provider builds the configured backend. "fallback" wires cloud ElevenLabs as
// primary and local Qwen3 as secondary (degrade-to-local), matching Fallback's
// design.
func provider(c config.TTSConfig) (Provider, error) {
	switch c.Provider {
	case "qwen3":
		return NewClient(c.Qwen3), nil
	case "elevenlabs":
		return eleven(c.ElevenLabs), nil
	case "fallback":
		return &Fallback{Primary: eleven(c.ElevenLabs), Secondary: NewClient(c.Qwen3)}, nil
	default:
		return nil, fmt.Errorf("tts: unknown provider %q", c.Provider)
	}
}

func eleven(c config.ElevenLabsConfig) *ElevenLabs {
	return &ElevenLabs{APIKey: c.APIKey, VoiceID: c.VoiceID, Model: c.Model, BaseURL: c.BaseURL}
}
