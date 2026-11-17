// Command play is the standalone speaker debug tool: it synthesises text with
// the local server and plays it on the default output device through
// play.Speaker (oto), exercising the real playback path — one request at a time,
// merging of queued text, look-ahead synthesis, and barge-in. Unlike cmd/say
// (which writes a WAV and is headless), this opens an audio device, so it needs
// a working speaker (and the TTS server at -tts).
//
// The text is fed the way the voice agent feeds it: one Enqueue per sentence,
// -gap apart, as if an LLM were streaming the reply. Every synthesis request is
// logged with its text and timing, so the cutting is visible.
//
//	go run ./cmd/play -text "Привет, мир"
//	go run ./cmd/play -config config.json -text "..."         # voice settings from config
//	go run ./cmd/play -max-fragment 0 -text "..."             # no merging: a request per sentence
//	go run ./cmd/play -interrupt-after 3s -then "Хорошо." ... # barge-in, then a new reply
package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/xcono/tts"
	"github.com/xcono/tts/pkg/config"
	"github.com/xcono/tts/pkg/play"
)

const defaultText = `Привет! Это проверка локального синтеза и воспроизведения речи.`

func main() {
	cfgPath := flag.String("config", "", "path to config.json; its local-server section replaces the flags below")
	var local config.Qwen3Config
	flag.StringVar(&local.BaseURL, "tts", "http://localhost:8007/v1", "local TTS server base URL")
	flag.StringVar(&local.Voice, "voice", "", "voice name; empty = server default")
	flag.StringVar(&local.Language, "lang", "", "language, e.g. ru; empty = server detects")
	flag.StringVar(&local.RefAudio, "ref-audio", "", "reference clip as the server sees it, e.g. file:///ref/ref_stas.wav")
	flag.StringVar(&local.RefText, "ref-text", "", "exact transcript of -ref-audio")
	flag.IntVar(&local.Steps, "steps", 0, "diffusion steps; 0 = server default")
	flag.Float64Var(&local.Guidance, "guidance", 0, "guidance scale; 0 = server default")

	text := flag.String("text", defaultText, "text to speak")
	gap := flag.Duration("gap", 300*time.Millisecond, "pause between sentences as they are enqueued (an LLM's pace); 0 = all at once")
	maxFragment := flag.Int("max-fragment", play.DefaultMaxFragment, "most characters merged into one request; 0 = a request per sentence")
	interruptAfter := flag.Duration("interrupt-after", 0, "barge in this long after the first sound; 0 = never")
	then := flag.String("then", "", "reply to speak right after the barge-in")
	flag.Parse()

	if *cfgPath != "" {
		cfg, err := config.Load(*cfgPath)
		if err != nil {
			log.Fatal(err)
		}
		local = cfg.TTS.Qwen3
	}
	if err := run(local, *text, *gap, *maxFragment, *interruptAfter, *then); err != nil {
		log.Fatal(err)
	}
}

func run(local config.Qwen3Config, text string, gap time.Duration, maxFragment int, interruptAfter time.Duration, then string) error {
	t0 := time.Now()
	sp, err := play.NewSpeaker(logSynth{tts.NewClient(local), t0}, play.WithMaxFragment(maxFragment))
	if err != nil {
		return err
	}
	defer sp.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	sentences := splitSentences(text)
	log.Printf("play: %d sentence(s) via %s, gap %s, max fragment %d (Ctrl-C to stop)",
		len(sentences), local.BaseURL, gap, maxFragment)

	go func() {
		for i, s := range sentences {
			if i > 0 && gap > 0 {
				select {
				case <-time.After(gap):
				case <-ctx.Done():
					return
				}
			}
			sp.Enqueue(s)
		}
	}()

	if interruptAfter > 0 {
		go func() {
			if !waitFor(ctx, sp, tts.Speaking) {
				return
			}
			select {
			case <-time.After(interruptAfter):
			case <-ctx.Done():
				return
			}
			log.Printf("play: +%s barge-in", since(t0))
			stop() // stops the feeder: nothing more of this reply is enqueued
			sp.Interrupt()
			if then != "" {
				sp.Enqueue(then)
			}
		}()
	}

	waitIdle(ctx, sp)
	if interruptAfter > 0 && then != "" {
		// ctx was cancelled by the barge-in; wait for the follow-up reply on a fresh one.
		ctx2, stop2 := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop2()
		waitIdle(ctx2, sp)
	}
	log.Printf("play: +%s done", since(t0))
	return nil
}

// logSynth logs each synthesis request: when it was sent, when its audio came
// back, how much speech it holds. The times are relative to program start, so
// the log reads as a timeline of the reply.
type logSynth struct {
	s  play.Synth
	t0 time.Time
}

func (l logSynth) SynthesizeStream(ctx context.Context, text string, emit func([]int16) error) error {
	sent := time.Now()
	log.Printf("play: +%s → %d chars %q", since(l.t0), len([]rune(text)), text)
	var n int
	err := l.s.SynthesizeStream(ctx, text, func(chunk []int16) error {
		n += len(chunk)
		return emit(chunk)
	})
	log.Printf("play: +%s ← %.2fs audio in %s (err=%v)", since(l.t0),
		float64(n)/float64(tts.SampleRate), time.Since(sent).Round(time.Millisecond), err)
	return err
}

func since(t time.Time) time.Duration { return time.Since(t).Round(10 * time.Millisecond) }

// splitSentences cuts text after . ! ? … followed by whitespace — the same
// simple rule the voice agent applies to an LLM stream before it calls Enqueue.
func splitSentences(text string) []string {
	var out []string
	var b strings.Builder
	runes := []rune(strings.TrimSpace(text))
	for i, r := range runes {
		b.WriteRune(r)
		end := strings.ContainsRune(".!?…", r) && (i+1 == len(runes) || runes[i+1] == ' ' || runes[i+1] == '\n')
		if end || i+1 == len(runes) {
			if s := strings.TrimSpace(b.String()); s != "" {
				out = append(out, s)
			}
			b.Reset()
		}
	}
	return out
}

// waitFor blocks until the speaker reaches status want, or ctx is cancelled.
func waitFor(ctx context.Context, sp *play.Speaker, want tts.Status) bool {
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return false
		case <-tick.C:
			if sp.Status() == want {
				return true
			}
		}
	}
}

// waitIdle blocks until the utterance has started and finished playing (status
// returns to Ready), or the context is cancelled (Ctrl-C). It first waits for
// playback to begin so it doesn't return on the brief initial Ready window
// before synthesis is in flight.
func waitIdle(ctx context.Context, sp *play.Speaker) {
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()

	started := false
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			switch s := sp.Status(); {
			case s != tts.Ready:
				started = true
			case started: // was speaking/processing, now idle → done
				return
			}
		}
	}
}
