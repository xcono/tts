package play

import (
	"context"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xcono/tts"
)

// fakeSynth maps text to a canned single-chunk clip.
type fakeSynth struct{ audio map[string][]int16 }

func (f fakeSynth) SynthesizeStream(_ context.Context, text string, emit func([]int16) error) error {
	if a := f.audio[text]; len(a) > 0 {
		return emit(a)
	}
	return nil
}

// gatedSynth emits its chunks one at a time, blocking before the chunk at
// gateAfter until release is closed — so a test can prove earlier chunks play
// while synthesis is still in progress.
type gatedSynth struct {
	chunks    [][]int16
	gateAfter int
	release   chan struct{}
}

func (g gatedSynth) SynthesizeStream(ctx context.Context, _ string, emit func([]int16) error) error {
	for i, c := range g.chunks {
		if i == g.gateAfter {
			select {
			case <-g.release:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		if err := emit(c); err != nil {
			return err
		}
	}
	return nil
}

// recordingSynth reports each text it's asked to synthesise on `asked`.
type recordingSynth struct {
	audio map[string][]int16
	asked chan string
}

func (s recordingSynth) SynthesizeStream(_ context.Context, text string, emit func([]int16) error) error {
	s.asked <- text
	if a := s.audio[text]; len(a) > 0 {
		return emit(a)
	}
	return nil
}

// fakeSink records played samples and Flush/Close calls; no device.
type fakeSink struct {
	ch      chan int16
	flushed atomic.Int32
	closed  atomic.Int32
	pending atomic.Int64 // what the device still has to sound; the test sets it
}

func newFakeSink() *fakeSink { return &fakeSink{ch: make(chan int16, 8192)} }

func (s *fakeSink) Write(samples []int16) error {
	for _, x := range samples {
		s.ch <- x
	}
	return nil
}

func (s *fakeSink) Pending() time.Duration { return time.Duration(s.pending.Load()) }
func (s *fakeSink) Flush() error           { s.flushed.Add(1); s.pending.Store(0); return nil }
func (s *fakeSink) Close() error           { s.closed.Add(1); return nil }

func (s *fakeSink) collect(n int, timeout time.Duration) []int16 {
	out := make([]int16, 0, n)
	deadline := time.After(timeout)
	for len(out) < n {
		select {
		case x := <-s.ch:
			out = append(out, x)
		case <-deadline:
			return out
		}
	}
	return out
}

// blockingSink never drains its Write unless the test reads it — so playback
// stalls, exposing whether the next message is prefetched.
type blockingSink struct{ ch chan []int16 }

func (s blockingSink) Write(samples []int16) error { s.ch <- samples; return nil }
func (blockingSink) Pending() time.Duration        { return 0 }
func (blockingSink) Flush() error                  { return nil }
func (blockingSink) Close() error                  { return nil }

func TestEnqueuePlaysReply(t *testing.T) {
	synth := fakeSynth{audio: map[string][]int16{"ответ": {1, 2, 3, 4}}}
	sink := newFakeSink()
	s := newSpeaker(synth, sink, 2, DefaultLookahead)
	defer s.Close()

	s.Enqueue("ответ")

	if got := sink.collect(4, 2*time.Second); !reflect.DeepEqual(got, []int16{1, 2, 3, 4}) {
		t.Fatalf("played %v, want [1 2 3 4]", got)
	}
}

func TestQueuesMultipleInOrder(t *testing.T) {
	synth := fakeSynth{audio: map[string][]int16{"один": {1, 2}, "два": {3, 4}}}
	sink := newFakeSink()
	s := newSpeaker(synth, sink, 2, DefaultLookahead)
	defer s.Close()

	s.Enqueue("один")
	s.Enqueue("два")

	if got := sink.collect(4, 2*time.Second); !reflect.DeepEqual(got, []int16{1, 2, 3, 4}) {
		t.Fatalf("played %v, want [1 2 3 4] (both replies, in order)", got)
	}
}

func TestPrefetchesNextWhilePlaybackBlocked(t *testing.T) {
	asked := make(chan string, 8)
	synth := recordingSynth{audio: map[string][]int16{"p1": {1, 1}, "p2": {2, 2}}, asked: asked}
	sink := blockingSink{ch: make(chan []int16)} // unbuffered → playback of p1 blocks
	s := newSpeaker(synth, sink, 2, DefaultLookahead)

	s.Enqueue("p1\n\np2") // two paragraphs

	// Both paragraphs must be synthesised even though p1's playback is wedged on
	// the sink — proving p2 is prefetched, not synthesised lazily after p1.
	got := map[string]bool{}
	for range 2 {
		select {
		case x := <-asked:
			got[x] = true
		case <-time.After(2 * time.Second):
			t.Fatalf("timed out; synthesised only %v (no prefetch)", got)
		}
	}
	if !got["p1"] || !got["p2"] {
		t.Fatalf("synthesised %v, want both p1 and p2 prefetched", got)
	}

	// Unwedge the sink so Close can drain the in-flight blocking Write.
	stop := make(chan struct{})
	go func() {
		for {
			select {
			case <-sink.ch:
			case <-stop:
				return
			}
		}
	}()
	s.Close()
	close(stop)
}

func TestPlaysFirstChunkBeforeSynthesisCompletes(t *testing.T) {
	// Play-as-you-go: the first synthesised chunk must reach the sink while the
	// synthesiser is still blocked before the second chunk.
	release := make(chan struct{})
	synth := gatedSynth{chunks: [][]int16{{1, 1}, {2, 2}}, gateAfter: 1, release: release}
	sink := newFakeSink()
	s := newSpeaker(synth, sink, 2, DefaultLookahead)
	defer s.Close()

	s.Enqueue("reply")

	if got := sink.collect(2, 2*time.Second); !reflect.DeepEqual(got, []int16{1, 1}) {
		t.Fatalf("played %v before synthesis finished, want [1 1] (not buffered)", got)
	}
	close(release)
	if got := sink.collect(2, 2*time.Second); !reflect.DeepEqual(got, []int16{2, 2}) {
		t.Fatalf("played %v after release, want [2 2]", got)
	}
}

func TestInterruptFlushesThenNextPlays(t *testing.T) {
	// Barge-in flushes the device and the next reply still plays — the interrupt
	// leaves the queue idle, and Enqueue restarts playback at the new content.
	synth := fakeSynth{audio: map[string][]int16{"a": {1, 2}, "b": {3, 4}}}
	sink := newFakeSink()
	s := newSpeaker(synth, sink, 2, DefaultLookahead)
	defer s.Close()

	s.Enqueue("a")
	if got := sink.collect(2, 2*time.Second); !reflect.DeepEqual(got, []int16{1, 2}) {
		t.Fatalf("played %v, want [1 2]", got)
	}

	s.Interrupt()
	s.Enqueue("b")
	if got := sink.collect(2, 2*time.Second); !reflect.DeepEqual(got, []int16{3, 4}) {
		t.Fatalf("played %v after interrupt, want [3 4]", got)
	}
	if sink.flushed.Load() == 0 {
		t.Error("Interrupt did not Flush the sink")
	}
}

func TestBargeInThenEnqueueStillPlays(t *testing.T) {
	// An Interrupt with nothing playing (queue idle) followed by Enqueue must
	// still play — mirrors a barge-in arriving just before the answer.
	synth := fakeSynth{audio: map[string][]int16{"ответ": {5, 6, 7, 8}}}
	sink := newFakeSink()
	s := newSpeaker(synth, sink, 2, DefaultLookahead)
	defer s.Close()

	s.Interrupt()
	s.Enqueue("ответ")

	if got := sink.collect(4, 2*time.Second); !reflect.DeepEqual(got, []int16{5, 6, 7, 8}) {
		t.Fatalf("played %v, want [5 6 7 8]", got)
	}
}

func TestCloseStopsLoopAndClosesSink(t *testing.T) {
	synth := fakeSynth{}
	sink := newFakeSink()
	s := newSpeaker(synth, sink, 2, DefaultLookahead)

	if err := s.Close(); err != nil {
		t.Fatalf("Close() = %v", err)
	}
	if sink.closed.Load() != 1 {
		t.Errorf("sink Close called %d times, want 1", sink.closed.Load())
	}
	s.Close() // idempotent
	if sink.closed.Load() != 1 {
		t.Errorf("second Close re-closed the sink (count=%d)", sink.closed.Load())
	}
}

// scriptSynth is a synthesiser the test drives by hand: every request is
// announced on asked and then blocks until the test releases it (or its context
// is cancelled). It records how many requests ran at once and which contexts
// were cancelled.
type scriptSynth struct {
	asked     chan string
	release   chan struct{}
	cancelled chan string
	running   atomic.Int32
	maxAtOnce atomic.Int32
}

func newScriptSynth() *scriptSynth {
	return &scriptSynth{
		asked:     make(chan string, 16),
		release:   make(chan struct{}, 16),
		cancelled: make(chan string, 16),
	}
}

func (s *scriptSynth) SynthesizeStream(ctx context.Context, text string, emit func([]int16) error) error {
	n := s.running.Add(1)
	defer s.running.Add(-1)
	for {
		m := s.maxAtOnce.Load()
		if n <= m || s.maxAtOnce.CompareAndSwap(m, n) {
			break
		}
	}
	s.asked <- text
	select {
	case <-s.release:
		return emit([]int16{1, 1})
	case <-ctx.Done():
		s.cancelled <- text
		return ctx.Err()
	}
}

func (s *scriptSynth) next(t *testing.T) string {
	t.Helper()
	select {
	case x := <-s.asked:
		return x
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for a synthesis request")
		return ""
	}
}

func (s *scriptSynth) none(t *testing.T, why string) {
	t.Helper()
	select {
	case x := <-s.asked:
		t.Fatalf("unexpected synthesis request %q: %s", x, why)
	case <-time.After(100 * time.Millisecond):
	}
}

// drain keeps a blocking sink readable until the returned stop func is called.
func drain(sink blockingSink) (stop func()) {
	done := make(chan struct{})
	go func() {
		for {
			select {
			case <-sink.ch:
			case <-done:
				return
			}
		}
	}()
	return func() { close(done) }
}

func TestMergesTextQueuedWhileFirstIsSynthesised(t *testing.T) {
	// The first message goes out alone so sound starts at once; what piles up
	// behind it goes out as ONE request, not one per sentence.
	synth := newScriptSynth()
	s := newSpeaker(synth, newFakeSink(), 2, DefaultLookahead)
	defer s.Close()

	s.Enqueue("Первое.")
	if got := synth.next(t); got != "Первое." {
		t.Fatalf("first request = %q, want the first message alone", got)
	}
	s.Enqueue("Второе.")
	s.Enqueue("Третье.")
	s.Enqueue("Четвёртое.")
	synth.none(t, "a request is already in flight")

	synth.release <- struct{}{}
	if got, want := synth.next(t), "Второе. Третье. Четвёртое."; got != want {
		t.Fatalf("second request = %q, want %q", got, want)
	}
	if m := synth.maxAtOnce.Load(); m != 1 {
		t.Errorf("%d requests ran at once, want 1", m)
	}
}

func TestMergeStopsAtTheCap(t *testing.T) {
	synth := newScriptSynth()
	// 15 characters: "Второе. Третье." fits exactly, the next message does not.
	s := newSpeaker(synth, newFakeSink(), 2, DefaultLookahead, WithMaxFragment(15))
	defer s.Close()

	s.Enqueue("Первое.")
	synth.next(t)
	s.Enqueue("Второе.")
	s.Enqueue("Третье.")
	s.Enqueue("Четвёртое.")
	synth.none(t, "a request is already in flight") // also lets the loop take all three in
	synth.release <- struct{}{}
	if got, want := synth.next(t), "Второе. Третье."; got != want {
		t.Fatalf("second request = %q, want %q (cap 15)", got, want)
	}
}

func TestNoMergeWhenCapIsZero(t *testing.T) {
	synth := newScriptSynth()
	sink := blockingSink{ch: make(chan []int16)}
	s := newSpeaker(synth, sink, 2, DefaultLookahead, WithMaxFragment(0))
	defer drain(sink)()
	defer s.Close()

	s.Enqueue("Первое.")
	synth.next(t)
	s.Enqueue("Второе.")
	s.Enqueue("Третье.")
	synth.release <- struct{}{}
	if got := synth.next(t); got != "Второе." {
		t.Fatalf("second request = %q, want %q (no merging)", got, "Второе.")
	}
}

func TestKeepsOneFragmentInReserve(t *testing.T) {
	// Playback of the first fragment is wedged on the sink. The reserve fragment
	// is still synthesised, but the one after it must wait until playback moves on.
	synth := newScriptSynth()
	sink := blockingSink{ch: make(chan []int16)}
	s := newSpeaker(synth, sink, 2, DefaultLookahead, WithMaxFragment(0))

	s.Enqueue("Первое.")
	s.Enqueue("Второе.")
	s.Enqueue("Третье.")
	synth.next(t)
	synth.release <- struct{}{}
	if got := synth.next(t); got != "Второе." {
		t.Fatalf("reserve request = %q, want %q", got, "Второе.")
	}
	synth.release <- struct{}{}
	synth.none(t, "the reserve is full while the first fragment is still playing")

	// Let the first fragment play out: the cursor moves on and the third goes out.
	stop := drain(sink)
	if got := synth.next(t); got != "Третье." {
		t.Fatalf("after playback moved on, request = %q, want %q", got, "Третье.")
	}
	synth.release <- struct{}{}
	s.Close()
	stop()
}

func TestInterruptAbortsRequestAndDropsQueue(t *testing.T) {
	synth := newScriptSynth()
	sink := newFakeSink()
	s := newSpeaker(synth, sink, 2, DefaultLookahead)
	defer s.Close()

	s.Enqueue("Старое первое.")
	s.Enqueue("Старое второе.")
	if got := synth.next(t); got != "Старое первое." {
		t.Fatalf("first request = %q", got)
	}

	s.Interrupt()
	select {
	case got := <-synth.cancelled:
		if got != "Старое первое." {
			t.Fatalf("cancelled %q, want the request in flight", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Interrupt did not cancel the request in flight")
	}

	// The interrupted reply's queued text must never be sent; the new reply is.
	s.Enqueue("Новое.")
	if got := synth.next(t); got != "Новое." {
		t.Fatalf("after interrupt, request = %q, want %q", got, "Новое.")
	}
	synth.release <- struct{}{}
	if got := sink.collect(2, 2*time.Second); !reflect.DeepEqual(got, []int16{1, 1}) {
		t.Fatalf("played %v, want the new reply only", got)
	}
	synth.none(t, "nothing of the interrupted reply may be synthesised")
	if s.Status() != 0 {
		// Ready once the new reply has played out.
		deadline := time.Now().Add(2 * time.Second)
		for s.Status() != 0 && time.Now().Before(deadline) {
			time.Sleep(5 * time.Millisecond)
		}
		if s.Status() != 0 {
			t.Errorf("status = %v after everything played, want ready", s.Status())
		}
	}
}

func waitStatus(t *testing.T, s *Speaker, want tts.Status) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for s.Status() != want {
		if time.Now().After(deadline) {
			t.Fatalf("status = %v, want %v", s.Status(), want)
		}
		time.Sleep(time.Millisecond)
	}
}

func TestSpeakingUntilDeviceFinishedSounding(t *testing.T) {
	// All audio has been handed to the device, but the device still has half a
	// second to sound: the reply is not over, so the status must stay Speaking.
	synth := fakeSynth{audio: map[string][]int16{"ответ": {1, 2, 3, 4}}}
	sink := newFakeSink()
	sink.pending.Store(int64(500 * time.Millisecond))
	s := newSpeaker(synth, sink, 2, DefaultLookahead)
	defer s.Close()

	s.Enqueue("ответ")
	sink.collect(4, 2*time.Second)
	waitStatus(t, s, tts.Speaking)
	time.Sleep(5 * drainPoll)
	if got := s.Status(); got != tts.Speaking {
		t.Fatalf("status = %v while the device is still sounding, want speaking", got)
	}

	sink.pending.Store(0) // the last sample has come out
	waitStatus(t, s, tts.Ready)
}

func TestProcessingUntilFirstSound(t *testing.T) {
	// Text is accepted and being synthesised, but nothing is heard yet.
	synth := newScriptSynth()
	s := newSpeaker(synth, newFakeSink(), 2, DefaultLookahead)
	defer s.Close()

	s.Enqueue("ответ")
	synth.next(t)
	waitStatus(t, s, tts.Processing)
	time.Sleep(5 * drainPoll)
	if got := s.Status(); got != tts.Processing {
		t.Fatalf("status = %v before any audio, want processing", got)
	}

	synth.release <- struct{}{}
	waitStatus(t, s, tts.Ready) // fake device: sounded at once
}

func TestInterruptWhileLastAudioIsSounding(t *testing.T) {
	// Barge-in during the tail: the device is flushed, so nothing is left to
	// sound and the speaker is Ready at once, not after the tail would have ended.
	synth := fakeSynth{audio: map[string][]int16{"ответ": {1, 2}}}
	sink := newFakeSink()
	sink.pending.Store(int64(time.Hour))
	s := newSpeaker(synth, sink, 2, DefaultLookahead)
	defer s.Close()

	s.Enqueue("ответ")
	sink.collect(2, 2*time.Second)
	waitStatus(t, s, tts.Speaking)

	s.Interrupt()
	waitStatus(t, s, tts.Ready)
	if sink.flushed.Load() == 0 {
		t.Error("Interrupt did not flush the device")
	}
}
