package play

import (
	"io"
	"sync"
	"time"

	"github.com/ebitengine/oto/v3"

	ttsaudio "github.com/xcono/tts/pkg/audio"
)

// deviceBuffer is the oto hardware buffer target. Small keeps barge-in latency
// low (audio already handed to the device can't be flushed); too small risks
// glitches. ~50 ms is the usual voice trade-off.
const deviceBuffer = 50 * time.Millisecond

// deviceTail is how long sound keeps coming out after the player's own buffer
// has emptied: what is already in the hardware/sound-server buffer. oto cannot
// report that part, so Pending holds on for this long after the buffer drains.
const deviceTail = 2 * deviceBuffer

// otoSink plays 16-bit mono PCM through an oto player. Write pushes samples
// through an io.Pipe the player drains at real time, so Write blocks until the
// audio is consumed — pacing the play loop exactly like a blocking device write.
// Flush drops not-yet-played audio for barge-in by closing the current
// pipe+player and starting a fresh pair (oto's Close is a deprecated no-op in
// v3, so a swap is how you discard buffered audio).
//
// All methods are serialised by the Speaker's single run goroutine except Close,
// which may race a blocked Write; mu guards the pipe/player swap so Close can
// unblock that Write cleanly.
type otoSink struct {
	ctx *oto.Context

	mu     sync.Mutex
	pr     *io.PipeReader
	pw     *io.PipeWriter
	player *oto.Player
	closed bool

	rate    int       // samples per second, to turn buffered bytes into time
	wrote   bool      // audio was written since the last drain or flush
	drained time.Time // when the player's buffer was first seen empty
}

// newOtoSink creates the process audio context (oto allows one) at the TTS wire
// rate — 24 kHz mono int16 LE — waits for the hardware to be ready, and opens
// the first player.
func newOtoSink(rate, _ int) (*otoSink, error) {
	ctx, ready, err := oto.NewContext(&oto.NewContextOptions{
		SampleRate:   rate,
		ChannelCount: 1,
		Format:       oto.FormatSignedInt16LE,
		BufferSize:   deviceBuffer,
	})
	if err != nil {
		return nil, err
	}
	<-ready // hardware init can lag; block until the device is up

	s := &otoSink{ctx: ctx, rate: rate}
	s.mu.Lock()
	s.swapPlayerLocked()
	s.mu.Unlock()
	return s, nil
}

// swapPlayerLocked tears down the current pipe+player (dropping any buffered
// audio and unblocking a pending Write with ErrClosedPipe) and starts a fresh
// pair, playing. Caller holds mu.
func (s *otoSink) swapPlayerLocked() {
	if s.player != nil {
		s.player.Pause()
	}
	if s.pw != nil {
		s.pw.Close()
	}
	if s.pr != nil {
		s.pr.Close() // unblocks a Write blocked on the old pipe
	}
	pr, pw := io.Pipe()
	p := s.ctx.NewPlayer(pr)
	p.Play()
	s.pr, s.pw, s.player = pr, pw, p
	s.wrote, s.drained = false, time.Time{} // the old player's audio is gone
}

// Write feeds one PCM chunk to the player and blocks until it is consumed. A
// concurrent Flush/Close swaps the pipe out from under it, surfacing as
// ErrClosedPipe — deliberate, not a device error, so it is swallowed.
func (s *otoSink) Write(samples []int16) error {
	s.mu.Lock()
	pw, closed := s.pw, s.closed
	s.mu.Unlock()
	if closed || pw == nil {
		return nil
	}
	s.mu.Lock()
	s.wrote, s.drained = true, time.Time{}
	s.mu.Unlock()
	if _, err := pw.Write(ttsaudio.PCM16LE(samples)); err != nil {
		if err == io.ErrClosedPipe {
			return nil
		}
		return err
	}
	return nil
}

// Pending reports how much written audio has not come out of the speaker yet.
// Write returns once the player has taken the samples into its buffer — about
// half a second ahead of the sound — so "everything written" is not "everything
// heard". Zero means the reply has actually finished sounding.
func (s *otoSink) Pending() time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.player == nil || !s.wrote {
		return 0
	}
	if n := s.player.BufferedSize(); n > 0 {
		s.drained = time.Time{}
		return time.Duration(n/2)*time.Second/time.Duration(s.rate) + deviceTail
	}
	if s.drained.IsZero() {
		s.drained = time.Now()
	}
	if left := deviceTail - time.Since(s.drained); left > 0 {
		return left
	}
	s.wrote = false
	return 0
}

// Flush drops audio not yet played (barge-in) by swapping to a fresh player.
func (s *otoSink) Flush() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.swapPlayerLocked()
	return nil
}

// Close pauses the player and closes the pipe, unblocking any in-flight Write.
// The oto context itself is process-global and is left in place.
func (s *otoSink) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	if s.player != nil {
		s.player.Pause()
	}
	if s.pw != nil {
		s.pw.Close()
	}
	if s.pr != nil {
		s.pr.Close()
	}
	return nil
}
