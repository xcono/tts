package play

import (
	"context"
	"reflect"
	"sync/atomic"
	"testing"
	"time"
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
}

func newFakeSink() *fakeSink { return &fakeSink{ch: make(chan int16, 8192)} }

func (s *fakeSink) Write(samples []int16) error {
	for _, x := range samples {
		s.ch <- x
	}
	return nil
}

func (s *fakeSink) Flush() error { s.flushed.Add(1); return nil }
func (s *fakeSink) Close() error { s.closed.Add(1); return nil }

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
