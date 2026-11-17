// Package voiceq is the voice-message playback timeline: a pure, navigable,
// session-grouped queue of synthesised replies. It is a library, not a service —
// no audio device, no network, no goroutine. The play.Speaker imports it,
// mutates it directly from its own loop, and reads its state to decide what to
// play next.
//
// The speaker's playback loop is woven into its single select-loop, not a
// blocking call, so a barge-in stays responsive:
//
//	for {
//		select {
//		case cmd := <-cmds:         // Enqueue / Interrupt
//			handle(cmd)             // e.g. q.Pause() on barge-in
//		default:
//			if !q.Playing() {       // nothing to play: block on commands instead
//				cmd := <-cmds; handle(cmd); continue
//			}
//			cur := q.Current()
//			n := min(chunk, len(cur.Audio)-q.Pos())            // ~100 ms slice
//			sink.Write(cur.Audio[q.Pos() : q.Pos()+n])
//			q.Advance(n)            // moves pos; auto-advances / stops at the end
//		}
//	}
//
// Because the speaker writes one ~100 ms chunk per iteration and re-checks
// commands between chunks, pause/stop/next/barge-in land within one chunk — the
// sink stays dumb, the responsiveness comes from chunking.
package voiceq

import (
	"fmt"
	"strings"
	"time"
)

// Message is one voice message on the timeline — typically one paragraph of a
// reply. Audio is the synthesised PCM, filled lazily by the speaker when the
// message becomes current; it is nil until then. Messages are grouped by
// SessionID so a reply's paragraphs (and, later, whole conversations) can be
// navigated as a unit.
type Message struct {
	ID        string
	SessionID string
	Seq       int // paragraph index within the reply it came from
	Text      string
	Audio     []int16
	// Done reports that synthesis for this message has finished, so Audio is
	// complete. While false, Audio may still be growing (streamed in chunk by
	// chunk by the speaker), and the play cursor must wait at the end of Audio
	// rather than advance to the next message. Set via MarkDone.
	Done    bool
	Created time.Time
}

// Queue is the playback state machine: an ordered, persistent list of messages
// with a cursor, a play/pause flag, and a sample offset into the current
// message. Played messages stay in the list so the cursor can navigate back
// (Prev) and forward (Next). The zero value is an empty, idle queue.
type Queue struct {
	msgs    []Message
	cursor  int
	playing bool
	pos     int
	ids     int // monotonic counter for unique message IDs
}

// Current returns the message under the cursor, or nil if the queue is empty.
func (q *Queue) Current() *Message {
	if len(q.msgs) == 0 {
		return nil
	}
	return &q.msgs[q.cursor]
}

// Playing reports whether the speaker should be writing audio.
func (q *Queue) Playing() bool { return q.playing }

// Pos is the sample offset into the current message.
func (q *Queue) Pos() int { return q.pos }

// Append adds a message to the end of the timeline. If the queue is idle (empty,
// or stopped/finished — e.g. after a barge-in), playback starts at the new
// message; if a message is already playing, the new one queues after it without
// interrupting. Played messages stay in the list, so the cursor can still
// navigate back to them.
func (q *Queue) Append(m Message) {
	start := len(q.msgs)
	q.msgs = append(q.msgs, m)
	if !q.playing {
		q.cursor = start
		q.pos = 0
		q.playing = true
	}
}

// Upcoming returns copies of up to n messages from the cursor forward (current
// included), clamped to the end. The speaker uses it to synthesise ahead of
// playback.
func (q *Queue) Upcoming(n int) []Message {
	if n <= 0 || len(q.msgs) == 0 {
		return nil
	}
	end := min(q.cursor+n, len(q.msgs))
	out := make([]Message, end-q.cursor)
	copy(out, q.msgs[q.cursor:end])
	return out
}

// SetAudio attaches synthesised audio to the message with the given ID, reporting
// whether it was found. Used by the speaker to store prefetched audio.
func (q *Queue) SetAudio(id string, audio []int16) bool {
	for i := range q.msgs {
		if q.msgs[i].ID == id {
			q.msgs[i].Audio = audio
			return true
		}
	}
	return false
}

// AppendAudio appends a freshly-synthesised PCM chunk to the message with the
// given ID, reporting whether it was found. The speaker calls it from its loop
// as TTS chunks stream in, so playback of a message can start before its
// synthesis is complete.
func (q *Queue) AppendAudio(id string, chunk []int16) bool {
	for i := range q.msgs {
		if q.msgs[i].ID == id {
			q.msgs[i].Audio = append(q.msgs[i].Audio, chunk...)
			return true
		}
	}
	return false
}

// MarkDone flags the message with the given ID as fully synthesised, reporting
// whether it was found. Until then the play cursor waits at the end of the
// streamed audio; once done, reaching the end auto-advances to the next message.
func (q *Queue) MarkDone(id string) bool {
	for i := range q.msgs {
		if q.msgs[i].ID == id {
			q.msgs[i].Done = true
			return true
		}
	}
	return false
}

// Pause halts playback but keeps the position, so Resume continues mid-message.
func (q *Queue) Pause() { q.playing = false }

// Resume continues playback from the current position.
func (q *Queue) Resume() { q.playing = true }

// Stop halts playback and rewinds the current message to its start; the cursor
// is unchanged, so the timeline is not discarded.
func (q *Queue) Stop() {
	q.playing = false
	q.pos = 0
}

// Next moves the cursor to the following message (clamped at the last) and
// rewinds it. The play/pause flag is left as-is.
func (q *Queue) Next() {
	if q.cursor < len(q.msgs)-1 {
		q.cursor++
	}
	q.pos = 0
}

// Prev moves the cursor to the preceding message (clamped at the first) and
// rewinds it.
func (q *Queue) Prev() {
	if q.cursor > 0 {
		q.cursor--
	}
	q.pos = 0
}

// AppendAnswer splits a reply into paragraphs (on blank lines) and appends each
// as a Message tagged with sessionID and its paragraph index, so a reply's parts
// play in order and can be navigated and grouped by conversation. Blank
// paragraphs are dropped. It returns the number of messages appended.
func (q *Queue) AppendAnswer(sessionID, text string) int {
	paras := splitParagraphs(text)
	for i, p := range paras {
		q.ids++
		q.Append(Message{
			ID:        fmt.Sprintf("%s-%d", sessionID, q.ids),
			SessionID: sessionID,
			Seq:       i,
			Text:      p,
			Created:   time.Now(),
		})
	}
	return len(paras)
}

// splitParagraphs breaks text on blank lines, trimming each and dropping empties.
func splitParagraphs(text string) []string {
	var out []string
	for p := range strings.SplitSeq(text, "\n\n") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// Advance progresses playback of the current message by n samples. When a
// finished message (Done) is played to its end it auto-advances to the next, or
// stops at the end of the timeline. A message still being streamed (not Done)
// holds at the end of its audio so far — the speaker blocks for the next chunk
// rather than skipping ahead. It is the one call the speaker's playback loop
// makes after writing a chunk to the sink.
func (q *Queue) Advance(n int) {
	cur := q.Current()
	if cur == nil {
		return
	}
	q.pos += n
	if cur.Done && q.pos >= len(cur.Audio) {
		q.finishCurrent()
	}
}

// finishCurrent moves to the next message if there is one, otherwise stops at
// the end of the timeline.
func (q *Queue) finishCurrent() {
	if q.cursor < len(q.msgs)-1 {
		q.cursor++
		q.pos = 0
		return
	}
	q.playing = false
}
