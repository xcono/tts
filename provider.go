package tts

import "context"

// Synthesizer is the streaming TTS contract: emit each PCM chunk as it arrives
// so the consumer can play as-you-go (first audio in hundreds of ms). Both
// *Client (local) and *ElevenLabs (cloud) satisfy it, so they're interchangeable
// — and *Fallback composes from two of them.
type Synthesizer interface {
	SynthesizeStream(ctx context.Context, text string, emit func(chunk []int16) error) error
}

// Provider is the full text-to-speech contract: streaming (Synthesizer) plus
// one-shot synthesis, all in 24 kHz mono int16 LE PCM (SampleRate). The local
// server *Client is one implementation; cloud backends (e.g. *ElevenLabs) sit
// behind the same interface, and a degrade-to-local *Fallback is just another
// Provider. Implementations MUST emit at SampleRate.
//
// Contract note: consumers (../voices) should declare their own narrow interface
// for the subset they need rather than depend on Provider wholesale — Go's
// consumer-side-interface idiom.
type Provider interface {
	Synthesizer // SynthesizeStream — play-as-you-go
	// Synthesize accumulates the stream and returns the whole clip, for
	// one-shot callers.
	Synthesize(ctx context.Context, text string) ([]int16, error)
}

// Every backend and the fallback wrapper satisfy the full Provider contract.
var (
	_ Provider = (*Client)(nil)
	_ Provider = (*ElevenLabs)(nil)
	_ Provider = (*Fallback)(nil)
)
