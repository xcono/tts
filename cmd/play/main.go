// Command play is the standalone speaker debug tool: it synthesises text with
// the local Qwen3-TTS provider and plays it on the default output device through
// play.Speaker (oto), exercising the real playback path — look-ahead synthesis,
// play-as-you-go, and the queue. Unlike cmd/say (which writes a WAV and is
// headless), this opens an audio device, so it needs a working speaker (and,
// like the rest of the stack, the TTS server at -tts).
//
//	go run ./cmd/play -text "Привет, мир"                 # local Qwen3-TTS (:8007)
//	go run ./cmd/play -tts http://localhost:8007/v1 -voice ...
package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/xcono/tts"
	"github.com/xcono/tts/pkg/play"
)

const defaultText = `Привет! Это проверка локального синтеза и воспроизведения речи.`

func main() {
	ttsURL := flag.String("tts", "http://localhost:8007/v1", "Qwen3-TTS base URL")
	voice := flag.String("voice", "", "voice name; empty = server default")
	text := flag.String("text", defaultText, "text to speak")
	flag.Parse()

	if err := run(*ttsURL, *voice, *text); err != nil {
		log.Fatal(err)
	}
}

func run(ttsURL, voice, text string) error {
	synth := &tts.Client{BaseURL: ttsURL, Voice: voice}

	sp, err := play.NewSpeaker(synth)
	if err != nil {
		return err
	}
	defer sp.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	log.Printf("play: speaking %q via %s (Ctrl-C to stop)", text, ttsURL)
	sp.Enqueue(text)

	waitIdle(ctx, sp)
	return nil
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
