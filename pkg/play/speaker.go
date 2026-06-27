// Package play is the TTS module's speaker: it owns the audio output device and
// turns reply text into sound. It is the playback counterpart of the synthesis
// facade in the parent tts package — where tts.Service only produces PCM ("a
// dumb speaker"), play.Speaker plays it, with a play-as-you-go timeline,
// look-ahead synthesis, and chunk-granular barge-in.
//
// This is the one cgo-free audio device in the stack: it drives github.com/
// ebitengine/oto/v3 (pure-Go PulseAudio + dlopen'd ALSA at runtime), so the tts
// module stays buildable without PortAudio/cgo. The sibling xcono/asr owns the
// mic (capture) on PortAudio; this owns the speaker (playback) on oto.
//
// The consumer (xcono/voices) drives the Speaker by text and control only —
// Enqueue a reply, Interrupt on barge-in — never by PCM. That keeps all audio
// inside this module.
package play

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/xcono/tts"
	"github.com/xcono/tts/pkg/voiceq"
)

// DefaultChunk is the playback granularity in samples. At the TTS wire rate of
// 24 kHz, 2400 samples ≈ 100 ms — so a barge-in or stop takes effect within
// ~100 ms of the current message.
const DefaultChunk = 2400

// DefaultLookahead is how many messages (current included) to synthesise ahead
// of the play cursor, so the next paragraph is ready when the current ends.
const DefaultLookahead = 2

const sessionID = "main" // single conversation for now

// Synth turns reply text into PCM, delivered as a stream: emit is called with
// each chunk as it is synthesised, so playback can start at first audio instead
// of waiting for the whole clip. *tts.Client, *tts.ElevenLabs and *tts.Fallback
// (the tts.Synthesizer contract) all satisfy it.
type Synth interface {
	SynthesizeStream(ctx context.Context, text string, emit func(chunk []int16) error) error
}

// sink is the playback device the engine writes PCM chunks to. Write blocks
// until the audio is consumed (so the loop is paced by playback), Flush drops
// any not-yet-played audio on barge-in, and Close frees the device. otoSink is
// the real implementation; tests inject a fake.
type sink interface {
	Write(samples []int16) error
	Flush() error
	Close() error
}

// command is a control message into the Speaker's loop: a reply to enqueue, or a
// barge-in interrupt. Both arrive on the same channel so they serialise with
// playback in the single Run goroutine — no locks on the queue.
type command struct {
	enqueue   string
	interrupt bool
}

// synthChunk carries one streamed unit of synthesis from a prefetch goroutine
// back to the loop: either a PCM chunk to append, or (done) the signal that
// synthesis of that message has finished.
type synthChunk struct {
	id    string
	audio []int16
	done  bool
}

// Speaker owns the output device and plays enqueued replies. All queue state
// lives in the run goroutine; callers touch it only through Enqueue/Interrupt.
type Speaker struct {
	synth     Synth
	sink      sink
	chunk     int
	lookahead int

	cmds     chan command
	synthCh  chan synthChunk
	q        voiceq.Queue
	inFlight map[string]bool
	status   atomic.Int32

	ctx       context.Context
	cancel    context.CancelFunc
	done      chan struct{}
	closeOnce sync.Once
}

// NewSpeaker opens the default output device (oto) and starts the playback
// goroutine. It owns the device and frees it on Close. synth supplies the PCM
// for enqueued text.
func NewSpeaker(synth Synth) (*Speaker, error) {
	d, err := newOtoSink(tts.SampleRate, DefaultChunk)
	if err != nil {
		return nil, err
	}
	return newSpeaker(synth, d, DefaultChunk, DefaultLookahead), nil
}

// newSpeaker wires the engine over an injected sink — used by NewSpeaker with
// the oto device, and by tests with a fake sink (no device). chunk<=0 and
// lookahead<=0 fall back to the defaults.
func newSpeaker(synth Synth, snk sink, chunk, lookahead int) *Speaker {
	if chunk <= 0 {
		chunk = DefaultChunk
	}
	if lookahead <= 0 {
		lookahead = DefaultLookahead
	}
	ctx, cancel := context.WithCancel(context.Background())
	s := &Speaker{
		synth:     synth,
		sink:      snk,
		chunk:     chunk,
		lookahead: lookahead,
		cmds:      make(chan command, 16),
		synthCh:   make(chan synthChunk, 64),
		inFlight:  map[string]bool{},
		ctx:       ctx,
		cancel:    cancel,
		done:      make(chan struct{}),
	}
	go s.run()
	return s
}

// Enqueue queues a reply to be spoken. It is split into paragraphs and played in
// order after anything already queued; if the speaker is idle it starts at once.
// Non-blocking once the loop is running (the command buffer absorbs bursts).
func (s *Speaker) Enqueue(text string) {
	if strings.TrimSpace(text) == "" {
		return
	}
	select {
	case s.cmds <- command{enqueue: text}:
	case <-s.ctx.Done():
	}
}

// Interrupt is barge-in: it stops the current utterance within ~one chunk and
// drops any audio already synthesised ahead of the cursor for it. The next
// Enqueue starts fresh.
func (s *Speaker) Interrupt() {
	select {
	case s.cmds <- command{interrupt: true}:
	case <-s.ctx.Done():
	}
}

// Status reports the coarse playback state (Ready / Processing / Speaking); safe
// to poll from any goroutine. Mirrors tts.Service.Status for the playback path.
func (s *Speaker) Status() tts.Status { return tts.Status(s.status.Load()) }

// Close stops the playback loop, waits for it to drain, then frees the device.
// Idempotent.
func (s *Speaker) Close() error {
	var err error
	s.closeOnce.Do(func() {
		s.cancel()
		// A blocking-Write sink (the device paces playback by blocking Write)
		// leaves run() outside its select when cancelled; Flush unblocks that
		// Write so run() can observe ctx.Done and exit instead of wedging Close.
		_ = s.sink.Flush()
		<-s.done
		err = s.sink.Close()
	})
	return err
}

// run is the single-goroutine playback loop. Each pass it kicks off synthesis
// for upcoming messages, then either plays a chunk of the (ready) current
// message, handles a command (interrupt preempts between chunks), or folds in a
// finished synthesis. It blocks when there's nothing to play and nothing
// synthesised yet. Returns when ctx is cancelled (Close).
func (s *Speaker) run() {
	defer close(s.done)
	for {
		s.prefetch()
		s.updateStatus()

		cur := s.q.Current()

		// A finished message played to its end (including an empty one from a
		// synth error) must advance the queue even though there's nothing to
		// write — Advance(0) does that via the Done check.
		if s.q.Playing() && cur != nil && cur.Done && s.q.Pos() >= len(cur.Audio) {
			s.q.Advance(0)
			continue
		}

		// Playable = there are synthesised-but-unplayed samples right now.
		playable := s.q.Playing() && cur != nil && s.q.Pos() < len(cur.Audio)
		if playable {
			select {
			case <-s.ctx.Done():
				return
			case c := <-s.cmds:
				s.handle(c)
			case r := <-s.synthCh:
				s.onSynth(r)
			default:
				s.playChunk()
			}
			continue
		}

		// Idle, or waiting for the current message's next chunk.
		select {
		case <-s.ctx.Done():
			return
		case c := <-s.cmds:
			s.handle(c)
		case r := <-s.synthCh:
			s.onSynth(r)
		}
	}
}

// handle applies a control command. Interrupt pauses the queue and flushes the
// device so buffered audio stops immediately; Enqueue appends a reply (paragraph
// per message). On an idle queue, Append starts playback at the new content.
func (s *Speaker) handle(c command) {
	switch {
	case c.interrupt:
		s.q.Pause()
		_ = s.sink.Flush()
	case c.enqueue != "":
		s.q.AppendAnswer(sessionID, c.enqueue)
	}
}

// updateStatus derives the coarse status from queue/synth state: Speaking while
// the play cursor is live, Processing while synthesis is in flight with nothing
// to play yet, Ready otherwise.
func (s *Speaker) updateStatus() {
	switch {
	case s.q.Playing():
		s.status.Store(int32(tts.Speaking))
	case len(s.inFlight) > 0:
		s.status.Store(int32(tts.Processing))
	default:
		s.status.Store(int32(tts.Ready))
	}
}

// prefetch starts synthesis for any upcoming message (current + lookahead) that
// isn't done and isn't already in flight. Each goroutine streams PCM chunks back
// on synthCh, followed by a done marker; both are folded in by onSynth (from the
// loop), so the queue is only mutated in the single run goroutine.
func (s *Speaker) prefetch() {
	for _, m := range s.q.Upcoming(s.lookahead) {
		if s.inFlight[m.ID] || m.Done {
			continue
		}
		s.inFlight[m.ID] = true
		id, text := m.ID, m.Text
		go func() {
			// SynthesizeStream hands us a freshly-allocated slice per chunk, so it
			// is safe to send on the channel without copying. A synth error just
			// ends the stream early; the done marker still finalises the message
			// so the queue advances instead of stalling.
			_ = s.synth.SynthesizeStream(s.ctx, text, func(chunk []int16) error {
				select {
				case s.synthCh <- synthChunk{id: id, audio: chunk}:
					return nil
				case <-s.ctx.Done():
					return s.ctx.Err()
				}
			})
			select {
			case s.synthCh <- synthChunk{id: id, done: true}:
			case <-s.ctx.Done():
			}
		}()
	}
}

func (s *Speaker) onSynth(r synthChunk) {
	if r.done {
		delete(s.inFlight, r.id)
		s.q.MarkDone(r.id)
		return
	}
	s.q.AppendAudio(r.id, r.audio)
}

// playChunk writes the next chunk of the current (already-synthesised) message
// to the sink and advances; voiceq auto-advances or stops at the end. The sink
// Write blocks until the device consumes the chunk, so this paces the loop.
func (s *Speaker) playChunk() {
	cur := s.q.Current()
	if cur == nil {
		return
	}
	pos := s.q.Pos()
	end := min(pos+s.chunk, len(cur.Audio))
	if end <= pos {
		return // nothing synthesised past the cursor yet
	}
	if err := s.sink.Write(cur.Audio[pos:end]); err != nil {
		s.q.Stop()
		return
	}
	s.q.Advance(end - pos)
}
