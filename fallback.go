package tts

import (
	"context"
	"fmt"
)

// Fallback runs Primary and, only if it fails BEFORE emitting any audio, degrades
// to Secondary — e.g. cloud ElevenLabs primary, local server secondary, so a cloud
// outage or offline box still speaks.
//
// Two rules keep it honest:
//   - Once a chunk has played we can't restart cleanly, so a primary failure
//     *mid-stream* is returned as-is (no half-clip-then-restart stutter).
//   - An emit (sink) error is the consumer's problem, never a reason to fall
//     back; it's surfaced unchanged.
type Fallback struct {
	Primary    Synthesizer
	Secondary  Synthesizer
	OnFallback func(err error) // optional; called with the primary error when degrading
}

// SynthesizeStream satisfies Synthesizer.
func (f *Fallback) SynthesizeStream(ctx context.Context, text string, emit func(chunk []int16) error) error {
	emitted := false
	var emitErr error
	err := f.Primary.SynthesizeStream(ctx, text, func(chunk []int16) error {
		emitted = true
		if e := emit(chunk); e != nil {
			emitErr = e
			return e
		}
		return nil
	})
	if err == nil {
		return nil
	}
	if emitErr != nil {
		return emitErr // sink failure — surface, don't retry the other provider
	}
	if emitted {
		return fmt.Errorf("tts: primary failed mid-stream (already playing, cannot fall back): %w", err)
	}
	if f.OnFallback != nil {
		f.OnFallback(err)
	}
	return f.Secondary.SynthesizeStream(ctx, text, emit)
}

// Synthesize accumulates the streamed chunks into the full clip.
func (f *Fallback) Synthesize(ctx context.Context, text string) ([]int16, error) {
	return accumulate(ctx, f, text)
}
