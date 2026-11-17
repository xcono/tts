// Package play is the TTS module's speaker: it owns the audio output device and
// turns reply text into sound. It is the playback counterpart of the synthesis
// facade in the parent tts package — where tts.Service only produces PCM ("a
// dumb speaker"), play.Speaker plays it, with a play-as-you-go timeline,
// look-ahead synthesis, merging of queued text into larger fragments, and
// chunk-granular barge-in.
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
	"time"

	"github.com/xcono/tts"
	"github.com/xcono/tts/pkg/voiceq"
)

// DefaultChunk is the playback granularity in samples. At the TTS wire rate of
// 24 kHz, 2400 samples ≈ 100 ms — so a barge-in or stop takes effect within
// ~100 ms of the current message.
const DefaultChunk = 2400

// DefaultLookahead is how many fragments (current included) to synthesise ahead
// of the play cursor: the one playing and one in reserve. Synthesis is several
// times faster than speech, so one fragment in reserve is always ready in time;
// a larger reserve would not bring sound any sooner. It would cost two things:
// text already sent can no longer be merged with what arrives later, and the
// server cannot cancel a request, so on barge-in everything sent is still
// rendered before the next reply gets the GPU.
const DefaultLookahead = 2

// DefaultMaxFragment is the most characters sent to the synthesiser in one
// request when queued messages are merged — about 18 s of speech. The model
// intones one request as one passage, so merging removes the seams between
// separately rendered sentences; the cap bounds how long one request takes
// (and so how long the GPU stays busy after a barge-in). 0 disables merging:
// every enqueued message is its own request.
const DefaultMaxFragment = 250

const sessionID = "main" // single conversation for now

// Synth turns reply text into PCM, delivered as a stream: emit is called with
// each chunk as it is synthesised, so playback can start at first audio instead
// of waiting for the whole clip. *tts.Client, *tts.ElevenLabs and *tts.Fallback
// (the tts.Synthesizer contract) all satisfy it.
type Synth interface {
	SynthesizeStream(ctx context.Context, text string, emit func(chunk []int16) error) error
}

// sink is the playback device the engine writes PCM chunks to. Write blocks
// until the device has taken the audio (so the loop is paced by playback),
// Pending reports how much of it has not sounded yet, Flush drops any
// not-yet-played audio on barge-in, and Close frees the device. otoSink is the
// real implementation; tests inject a fake.
type sink interface {
	Write(samples []int16) error
	Pending() time.Duration
	Flush() error
	Close() error
}

// drainPoll is how often the loop re-checks the device while the last audio of
// a reply is still sounding — the resolution of the Speaking → Ready edge.
const drainPoll = 20 * time.Millisecond

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

// Option adjusts a Speaker at construction.
type Option func(*Speaker)

// WithMaxFragment sets the merge cap in characters (see DefaultMaxFragment);
// 0 sends every enqueued message as its own request.
func WithMaxFragment(chars int) Option {
	return func(s *Speaker) { s.maxFragment = max(chars, 0) }
}

// Speaker owns the output device and plays enqueued replies. All queue state
// lives in the run goroutine; callers touch it only through Enqueue/Interrupt.
type Speaker struct {
	synth       Synth
	sink        sink
	chunk       int
	lookahead   int
	maxFragment int

	cmds    chan command
	synthCh chan synthChunk
	q       voiceq.Queue
	status  atomic.Int32

	// At most one synthesis request is out at a time: inFlight is the ID of the
	// message being synthesised ("" when none) and cancelSynth aborts it.
	inFlight    string
	cancelSynth context.CancelFunc

	// sounding is true from the first chunk written to the device until the
	// device reports that everything written has come out of the speaker.
	sounding bool

	ctx       context.Context
	cancel    context.CancelFunc
	done      chan struct{}
	closeOnce sync.Once
}

// NewSpeaker opens the default output device (oto) and starts the playback
// goroutine. It owns the device and frees it on Close. synth supplies the PCM
// for enqueued text.
func NewSpeaker(synth Synth, opts ...Option) (*Speaker, error) {
	d, err := newOtoSink(tts.SampleRate, DefaultChunk)
	if err != nil {
		return nil, err
	}
	return newSpeaker(synth, d, DefaultChunk, DefaultLookahead, opts...), nil
}

// newSpeaker wires the engine over an injected sink — used by NewSpeaker with
// the oto device, and by tests with a fake sink (no device). chunk<=0 and
// lookahead<=0 fall back to the defaults.
func newSpeaker(synth Synth, snk sink, chunk, lookahead int, opts ...Option) *Speaker {
	if chunk <= 0 {
		chunk = DefaultChunk
	}
	if lookahead <= 0 {
		lookahead = DefaultLookahead
	}
	ctx, cancel := context.WithCancel(context.Background())
	s := &Speaker{
		synth:       synth,
		sink:        snk,
		chunk:       chunk,
		lookahead:   lookahead,
		maxFragment: DefaultMaxFragment,
		cmds:        make(chan command, 16),
		synthCh:     make(chan synthChunk, 64),
		ctx:         ctx,
		cancel:      cancel,
		done:        make(chan struct{}),
	}
	for _, o := range opts {
		o(s)
	}
	go s.run()
	return s
}

// Enqueue queues a reply to be spoken, after anything already queued; if the
// speaker is idle it is synthesised at once, as is, so sound starts as early as
// possible. Text enqueued while something is being synthesised or played waits,
// and goes out merged into one request (see DefaultMaxFragment).
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

// Interrupt is barge-in: it stops the current utterance within ~one chunk,
// aborts the synthesis request in flight, and drops everything still queued —
// audio synthesised ahead and text not yet sent. The next Enqueue starts fresh.
func (s *Speaker) Interrupt() {
	select {
	case s.cmds <- command{interrupt: true}:
	case <-s.ctx.Done():
	}
}

// Status reports the playback state by what is actually heard; safe to poll from
// any goroutine. Processing: text is accepted but no sound is coming out (being
// synthesised). Speaking: sound is coming out. Ready: nothing queued and the last
// audio has finished sounding — not merely been handed to the device.
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
		s.updateStatus(playable)
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

		// Idle, or waiting for the current message's next chunk. While the last
		// audio written is still sounding, wake up to notice when it ends.
		var drain <-chan time.Time
		if s.sounding {
			drain = time.After(drainPoll)
		}
		select {
		case <-s.ctx.Done():
			return
		case c := <-s.cmds:
			s.handle(c)
		case r := <-s.synthCh:
			s.onSynth(r)
		case <-drain:
		}
	}
}

// handle applies a control command. Interrupt aborts the request in flight,
// empties the queue and flushes the device so buffered audio stops immediately;
// Enqueue appends a reply (paragraph per message). On an idle queue, Append
// starts playback at the new content.
func (s *Speaker) handle(c command) {
	switch {
	case c.interrupt:
		if s.cancelSynth != nil {
			// Closes the HTTP request. A late chunk or done marker from it is
			// ignored by onSynth, because inFlight no longer names it.
			s.cancelSynth()
			s.cancelSynth = nil
		}
		s.inFlight = ""
		s.q.Abandon()
		_ = s.sink.Flush()
		s.sounding = false // the flush dropped whatever was still to sound
	case c.enqueue != "":
		s.q.AppendAnswer(sessionID, c.enqueue)
	}
}

// updateStatus derives the status from what is heard, not from the queue:
// Speaking while audio is being written or the device is still sounding what was
// written, Processing while text is queued or in synthesis with no sound coming
// out, Ready otherwise. playable says the loop is about to write a chunk.
func (s *Speaker) updateStatus(playable bool) {
	if s.sounding && !playable && s.sink.Pending() == 0 {
		s.sounding = false
	}
	switch {
	case s.sounding || playable:
		s.status.Store(int32(tts.Speaking))
	case s.q.Playing() || s.inFlight != "":
		s.status.Store(int32(tts.Processing))
	default:
		s.status.Store(int32(tts.Ready))
	}
}

// prefetch sends the next fragment to the synthesiser, one request at a time:
// nothing goes out while a request is in flight, and the next one goes out as
// soon as it returns — playback of earlier audio does not hold it back. Only the
// current message and the reserve after it (lookahead) are eligible.
//
// The message under the cursor is sent as is. A reserve message first absorbs
// the messages queued behind it, up to maxFragment characters. The goroutine
// streams PCM chunks back on synthCh, followed by a done marker; both are folded
// in by onSynth (from the loop), so the queue is only mutated in the run goroutine.
func (s *Speaker) prefetch() {
	if s.inFlight != "" {
		return
	}
	for i, m := range s.q.Upcoming(s.lookahead) {
		if m.Done {
			continue
		}
		text := m.Text
		if i > 0 && s.maxFragment > 0 {
			text = s.q.MergePending(m.ID, s.maxFragment)
		}
		s.synthesise(m.ID, text)
		return
	}
}

// synthesise starts the request for one fragment under its own context, so a
// barge-in can abort this request without stopping the Speaker.
func (s *Speaker) synthesise(id, text string) {
	ctx, cancel := context.WithCancel(s.ctx)
	s.inFlight, s.cancelSynth = id, cancel
	go func() {
		defer cancel()
		// SynthesizeStream hands us a freshly-allocated slice per chunk, so it is
		// safe to send on the channel without copying. A synth error just ends
		// the stream early; the done marker still finalises the message so the
		// queue advances instead of stalling.
		_ = s.synth.SynthesizeStream(ctx, text, func(chunk []int16) error {
			select {
			case s.synthCh <- synthChunk{id: id, audio: chunk}:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
		select {
		case s.synthCh <- synthChunk{id: id, done: true}:
		case <-s.ctx.Done():
		}
	}()
}

// onSynth folds one unit of synthesis into the queue. Anything from a request
// that is no longer the one in flight (aborted by a barge-in) is dropped.
func (s *Speaker) onSynth(r synthChunk) {
	if r.id != s.inFlight {
		return
	}
	if r.done {
		s.inFlight, s.cancelSynth = "", nil
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
	s.sounding = true
	s.q.Advance(end - pos)
}
