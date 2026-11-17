// Command say is the standalone TTS debug tool: it synthesises text through the
// configured provider and writes a 24 kHz mono WAV, logging the facade's Status
// transitions (ready → processing → speaking) and the time-to-first-audio. It is
// pure-Go (no audio device), so it runs headless — play the result with any
// player, e.g. `aplay say.wav`.
//
//	go run ./cmd/say -text "Привет, мир" -o out.wav      # local Qwen3-TTS (:8007)
//	go run ./cmd/say -config config.json -text "..."     # provider from config
//	go run ./cmd/say -provider elevenlabs -config config.json
package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/xcono/tts"
	"github.com/xcono/tts/pkg/audio"
	"github.com/xcono/tts/pkg/config"
)

const defaultText = `Привет! Это проверка локального синтеза речи.`

func main() {
	cfgPath := flag.String("config", "", "path to config.json (overrides -tts/-voice)")
	provider := flag.String("provider", "qwen3", "provider when no -config: qwen3 | elevenlabs | fallback")
	ttsURL := flag.String("tts", "http://localhost:8007/v1", "Qwen3-TTS base URL (no -config)")
	voice := flag.String("voice", "", "voice name; empty = server default")
	text := flag.String("text", defaultText, "text to synthesise")
	out := flag.String("o", "say.wav", "output WAV path")
	// Voice-clone settings for a vLLM-Omni server (no -config). Unset ones are not sent.
	var local config.Qwen3Config
	flag.StringVar(&local.Language, "lang", "", "language, e.g. ru; empty = server detects")
	flag.StringVar(&local.RefAudio, "ref-audio", "", "reference clip as the server sees it, e.g. file:///ref/ref_stas.wav")
	flag.StringVar(&local.RefText, "ref-text", "", "exact transcript of -ref-audio")
	flag.IntVar(&local.Steps, "steps", 0, "diffusion steps; 0 = server default")
	flag.Float64Var(&local.Guidance, "guidance", 0, "guidance scale; 0 = server default")
	flag.Parse()
	local.BaseURL, local.Voice = *ttsURL, *voice

	if err := run(*cfgPath, *provider, local, *text, *out); err != nil {
		log.Fatal(err)
	}
}

func run(cfgPath, provider string, local config.Qwen3Config, text, out string) error {
	svc, mode, err := build(cfgPath, provider, local)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	log.Printf("tts: %s | status=%s | synthesising %q", mode, svc.Status(), text)
	t0 := time.Now()
	var ttfa time.Duration
	var n int
	buf := make([]int16, 0, tts.SampleRate)
	err = svc.Stream(ctx, text, func(chunk []int16) error {
		if n == 0 {
			ttfa = time.Since(t0)
			log.Printf("tts: first audio in %s (status=%s)", ttfa.Round(time.Millisecond), svc.Status())
		}
		n += len(chunk)
		buf = append(buf, chunk...)
		return nil
	})
	if err != nil {
		return fmt.Errorf("synthesize: %w", err)
	}
	log.Printf("tts: %d samples (%.2fs audio), ttfa %s, total %s, status=%s",
		n, float64(n)/float64(tts.SampleRate), ttfa.Round(time.Millisecond),
		time.Since(t0).Round(time.Millisecond), svc.Status())

	if err := writeWAV(out, buf, tts.SampleRate); err != nil {
		return fmt.Errorf("write %s: %w", out, err)
	}
	log.Printf("tts: wrote %s", out)
	return nil
}

// build returns a facade Service either from config.json or, lacking one, from
// the flags (the common local-Qwen3 debug case). Mirrors tts.New's provider
// switch so -provider qwen3|elevenlabs|fallback works without a config file.
func build(cfgPath, provider string, local config.Qwen3Config) (*tts.Service, string, error) {
	if cfgPath != "" {
		cfg, err := config.Load(cfgPath)
		if err != nil {
			return nil, "", err
		}
		svc, err := tts.New(cfg)
		return svc, "config " + cfgPath + " (" + cfg.TTS.Provider + ")", err
	}
	switch provider {
	case "qwen3", "":
		return tts.NewWith(tts.NewClient(local)), "local " + local.BaseURL, nil
	default:
		return nil, "", fmt.Errorf("provider %q needs -config (credentials)", provider)
	}
}

// writeWAV serialises 24 kHz mono int16 PCM as a canonical 44-byte-header WAV,
// reusing the module's PCM16LE encoder for the sample data.
func writeWAV(path string, samples []int16, rate int) error {
	data := audio.PCM16LE(samples)
	var b bytes.Buffer
	b.WriteString("RIFF")
	binary.Write(&b, binary.LittleEndian, uint32(36+len(data)))
	b.WriteString("WAVE")
	b.WriteString("fmt ")
	binary.Write(&b, binary.LittleEndian, uint32(16)) // PCM fmt chunk size
	binary.Write(&b, binary.LittleEndian, uint16(1))  // audio format: PCM
	binary.Write(&b, binary.LittleEndian, uint16(1))  // channels: mono
	binary.Write(&b, binary.LittleEndian, uint32(rate))
	binary.Write(&b, binary.LittleEndian, uint32(rate*2)) // byte rate (mono, 2 bytes/sample)
	binary.Write(&b, binary.LittleEndian, uint16(2))      // block align
	binary.Write(&b, binary.LittleEndian, uint16(16))     // bits per sample
	b.WriteString("data")
	binary.Write(&b, binary.LittleEndian, uint32(len(data)))
	b.Write(data)
	return os.WriteFile(path, b.Bytes(), 0o644)
}
