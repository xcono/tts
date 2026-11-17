package voiceq

import (
	"reflect"
	"testing"
)

func TestAppendAudioAccumulatesChunks(t *testing.T) {
	var q Queue
	q.Append(Message{ID: "m"})

	if !q.AppendAudio("m", []int16{1, 2}) {
		t.Fatal("AppendAudio(m) = false, want true")
	}
	q.AppendAudio("m", []int16{3, 4})

	if got := q.Current().Audio; !reflect.DeepEqual(got, []int16{1, 2, 3, 4}) {
		t.Errorf("Audio = %v, want [1 2 3 4]", got)
	}
	if q.AppendAudio("nope", []int16{0}) {
		t.Error("AppendAudio(unknown) = true, want false")
	}
}

func TestAdvanceWaitsAtEndOfPartialStream(t *testing.T) {
	// A message that is streaming (not yet Done) must NOT auto-advance when the
	// play cursor reaches the end of the audio synthesised so far — it has to wait
	// for the next chunk.
	var q Queue
	q.Append(Message{ID: "m0"})
	q.Append(Message{ID: "m1"})
	q.AppendAudio("m0", []int16{1, 2})

	q.Advance(2) // played everything available, but m0 isn't Done

	if q.cursor != 0 {
		t.Errorf("cursor = %d, want 0 (stay on the streaming message)", q.cursor)
	}
	if !q.playing {
		t.Error("want still playing (waiting for the next chunk)")
	}
}

func TestAdvanceFinishesWhenDone(t *testing.T) {
	var q Queue
	q.Append(Message{ID: "m0"})
	q.Append(Message{ID: "m1"})
	q.AppendAudio("m0", []int16{1, 2})
	q.MarkDone("m0")

	q.Advance(2) // played everything and m0 is Done → advance to m1

	if q.cursor != 1 {
		t.Errorf("cursor = %d, want 1 (advanced past the finished message)", q.cursor)
	}
}

func TestDoneEmptyMessageFinishes(t *testing.T) {
	// A message that completes synthesis with no audio (e.g. a synth error) must
	// not stall the queue: it should finish and let the next message play.
	var q Queue
	q.Append(Message{ID: "m0"})
	q.Append(Message{ID: "m1"})
	q.MarkDone("m0")

	q.Advance(0) // nothing to play, but m0 is Done at pos 0 → finish

	if q.cursor != 1 {
		t.Errorf("cursor = %d, want 1 (empty finished message skipped)", q.cursor)
	}
}

func TestAppendToEmptyStartsPlaying(t *testing.T) {
	var q Queue
	q.Append(Message{ID: "m0"})

	if !q.playing {
		t.Error("want playing=true after first append")
	}
	if q.cursor != 0 {
		t.Errorf("cursor = %d, want 0", q.cursor)
	}
	cur := q.Current()
	if cur == nil || cur.ID != "m0" {
		t.Errorf("current = %v, want message m0", cur)
	}
}

func TestAppendToStoppedQueuePlaysNewContent(t *testing.T) {
	// After a barge-in stops playback, the queue is non-empty but idle. A new
	// answer must start playing at the new content (not stay silent, not replay
	// the old message).
	var q Queue
	q.Append(Message{ID: "old", Audio: []int16{1, 2}})
	q.Stop()

	q.Append(Message{ID: "new"})

	if !q.playing {
		t.Error("want playing=true: a new message on an idle queue should start")
	}
	cur := q.Current()
	if cur == nil || cur.ID != "new" {
		t.Errorf("current = %v, want new (cursor jumped to the new content)", cur)
	}
}

func TestAppendWhilePlayingDoesNotInterrupt(t *testing.T) {
	var q Queue
	q.Append(Message{ID: "m0"})
	q.pos = 1000
	q.Append(Message{ID: "m1"})

	if q.cursor != 0 {
		t.Errorf("cursor = %d, want 0 (still on m0)", q.cursor)
	}
	if q.pos != 1000 {
		t.Errorf("pos = %d, want 1000 (uninterrupted)", q.pos)
	}
	if !q.playing {
		t.Error("want still playing")
	}
}

func TestPauseKeepsPositionResumeContinues(t *testing.T) {
	var q Queue
	q.Append(Message{ID: "m0"})
	q.pos = 500

	q.Pause()
	if q.playing {
		t.Error("want playing=false after pause")
	}
	if q.pos != 500 {
		t.Errorf("pos = %d, want 500 (pause keeps position)", q.pos)
	}

	q.Resume()
	if !q.playing {
		t.Error("want playing=true after resume")
	}
	if q.pos != 500 {
		t.Errorf("pos = %d, want 500 (resume continues)", q.pos)
	}
}

func TestStopResetsPositionKeepsCursor(t *testing.T) {
	var q Queue
	q.Append(Message{ID: "m0"})
	q.Append(Message{ID: "m1"})
	q.Next()
	q.pos = 500

	q.Stop()
	if q.playing {
		t.Error("want playing=false after stop")
	}
	if q.pos != 0 {
		t.Errorf("pos = %d, want 0 (stop rewinds current)", q.pos)
	}
	if q.cursor != 1 {
		t.Errorf("cursor = %d, want 1 (stop keeps cursor)", q.cursor)
	}
}

func TestNextAdvancesAndClamps(t *testing.T) {
	var q Queue
	q.Append(Message{ID: "m0"})
	q.Append(Message{ID: "m1"})
	q.pos = 500

	q.Next()
	if q.cursor != 1 {
		t.Errorf("cursor = %d, want 1", q.cursor)
	}
	if q.pos != 0 {
		t.Errorf("pos = %d, want 0 after next", q.pos)
	}

	q.Next() // already last — clamp
	if q.cursor != 1 {
		t.Errorf("cursor = %d, want 1 (clamped at last)", q.cursor)
	}
}

func TestPrevRetreatsAndClamps(t *testing.T) {
	var q Queue
	q.Append(Message{ID: "m0"})
	q.Append(Message{ID: "m1"})
	q.Next()
	q.pos = 500

	q.Prev()
	if q.cursor != 0 {
		t.Errorf("cursor = %d, want 0", q.cursor)
	}
	if q.pos != 0 {
		t.Errorf("pos = %d, want 0 after prev", q.pos)
	}

	q.Prev() // already first — clamp
	if q.cursor != 0 {
		t.Errorf("cursor = %d, want 0 (clamped at first)", q.cursor)
	}
}

func TestUpcomingReturnsFromCursorClamped(t *testing.T) {
	var q Queue
	q.Append(Message{ID: "a"})
	q.Append(Message{ID: "b"})
	q.Append(Message{ID: "c"})

	up := q.Upcoming(2)
	if len(up) != 2 || up[0].ID != "a" || up[1].ID != "b" {
		t.Fatalf("Upcoming(2) from cursor 0 = %+v, want [a b]", up)
	}
	q.Next()           // cursor 1
	up = q.Upcoming(9) // clamp to end
	if len(up) != 2 || up[0].ID != "b" || up[1].ID != "c" {
		t.Fatalf("Upcoming(9) from cursor 1 = %+v, want [b c]", up)
	}
	if got := (&Queue{}).Upcoming(3); got != nil {
		t.Errorf("Upcoming on empty queue = %v, want nil", got)
	}
}

func TestSetAudioByID(t *testing.T) {
	var q Queue
	q.Append(Message{ID: "a"})
	q.Append(Message{ID: "b"})

	if !q.SetAudio("b", []int16{1, 2, 3}) {
		t.Fatal("SetAudio(b) = false, want true")
	}
	if got := q.Upcoming(2)[1].Audio; len(got) != 3 {
		t.Errorf("b.Audio = %v, want 3 samples", got)
	}
	if q.SetAudio("nope", []int16{0}) {
		t.Error("SetAudio(unknown) = true, want false")
	}
}

func TestMergePendingJoinsFollowingUpToCap(t *testing.T) {
	var q Queue
	for _, s := range []string{"Один.", "Два.", "Три.", "Четыре."} {
		q.AppendAnswer("s", s)
	}
	second := q.Upcoming(2)[1].ID

	// "Два. Три." is 9 characters; "Четыре." would make it 17.
	if got := q.MergePending(second, 10); got != "Два. Три." {
		t.Fatalf("merged = %q, want %q", got, "Два. Три.")
	}
	up := q.Upcoming(10)
	if len(up) != 3 || up[1].Text != "Два. Три." || up[2].Text != "Четыре." {
		t.Fatalf("queue after merge = %+v", up)
	}
}

func TestMergePendingLeavesSynthesisedMessagesAlone(t *testing.T) {
	var q Queue
	q.AppendAnswer("s", "Один.")
	q.AppendAnswer("s", "Два.")
	q.AppendAnswer("s", "Три.")
	up := q.Upcoming(3)
	q.AppendAudio(up[2].ID, []int16{1})

	if got := q.MergePending(up[1].ID, 100); got != "Два." {
		t.Fatalf("merged = %q, want %q (next already has audio)", got, "Два.")
	}
}

func TestAbandonDropsTheRestAndGoesIdle(t *testing.T) {
	var q Queue
	q.AppendAnswer("s", "Один.")
	q.AppendAnswer("s", "Два.")
	q.AppendAudio(q.Current().ID, []int16{1, 2, 3})
	q.Advance(1)

	q.Abandon()
	if q.Playing() {
		t.Fatal("still playing after Abandon")
	}
	if up := q.Upcoming(10); len(up) != 1 || !up[0].Done || up[0].Audio != nil {
		t.Fatalf("after Abandon = %+v, want only the closed current message", up)
	}

	q.AppendAnswer("s", "Новое.")
	if !q.Playing() || q.Current().Text != "Новое." || q.Pos() != 0 {
		t.Fatalf("new content did not start: %+v pos=%d", q.Current(), q.Pos())
	}
}

func TestFinishedMessageReleasesAudio(t *testing.T) {
	var q Queue
	q.AppendAnswer("s", "Один.")
	q.AppendAnswer("s", "Два.")
	first := q.Current().ID
	q.AppendAudio(first, []int16{1, 2})
	q.MarkDone(first)
	q.Advance(2)

	if q.Current().Text != "Два." {
		t.Fatalf("cursor on %q, want the second message", q.Current().Text)
	}
	q.Prev()
	if q.Current().Audio != nil {
		t.Fatal("played message still holds its audio")
	}
}
