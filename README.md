# tts

Decoupled **text-to-speech "mouth"** for the realtime voice agent. A cgo-free
client library over an OpenAI-compatible TTS server (local faster-qwen3-tts by
default, cloud ElevenLabs optionally), a synthesis facade (`Service`), and a
device-owning playback facade (`play.Speaker`) — consumed in-process by
[`xcono/voices`](../voices). No NATS — synthesis streams over Go channels, and
the speaker owns the audio output, so voices does no audio.

## Architecture

```
LLM loop ─► tts.Service ─┬─ Stream(text, emit) ── play-as-you-go (first audio in ~hundreds of ms)
(../voices)              ├─ Batch(text) ──────── whole 24 kHz mono int16 clip
                         └─ Status() ─────────── ready | processing | speaking
                              │
                              └─ Provider: Client (Qwen3-TTS) | ElevenLabs | Fallback
```

All backends emit **24 kHz mono int16 LE PCM** (`tts.SampleRate`) — the wire
format the playback side expects.

## Facade

```go
svc, err := tts.New(cfg)                 // provider from config
svc  := tts.NewWith(&tts.Client{...})    // or inject a Provider directly

err := svc.Stream(ctx, text, func(chunk []int16) error { /* play */ return nil })
clip, err := svc.Batch(ctx, text)        // accumulate the stream
st := svc.Status()                       // ready | processing | speaking
```

`Status` is a single shared value — one utterance at a time; serialise
`Stream`/`Batch` in the consumer (you don't speak two things at once).

## Providers

- **qwen3** (default) — local faster-qwen3-tts via `POST /v1/audio/speech`
  (`response_format:"pcm"`), voice clone, streaming. Needs the Python server (see below).
- **elevenlabs** — remote ElevenLabs Flash; streams `pcm_24000`.
- **fallback** — ElevenLabs primary, local Qwen3 secondary (degrade-to-local), so
  a cloud/network outage still speaks.

## Config

`config.json` (schema in `pkg/config`); minimal `{}` defaults to local qwen3:

```json
{
  "tts": {
    "provider": "qwen3",
    "qwen3":      { "base_url": "http://localhost:8007/v1", "voice": "", "model": "" },
    "elevenlabs": { "api_key": "", "voice_id": "", "model": "", "base_url": "" }
  }
}
```

## Speaker (the playback "mouth")

`pkg/play.Speaker` owns the **output device** and turns reply text into sound —
play-as-you-go, look-ahead synthesis, and chunk-granular **barge-in**. The
consumer drives it by text and control only (never PCM), so all audio stays in
this module:

```go
sp, err := play.NewSpeaker(&tts.Client{BaseURL: "http://localhost:8007/v1"})
defer sp.Close()

sp.Enqueue("Привет! Как дела?")   // queue a reply (paragraphs play in order)
sp.Interrupt()                    // barge-in: stop within ~one chunk (on the ASR's VAD signal)
st := sp.Status()                 // ready | processing | speaking
```

The device is [`ebitengine/oto`](https://github.com/ebitengine/oto) — **cgo-free**
on Linux: pure-Go PulseAudio (`jfreymuth/pulse`) with an ALSA fallback loaded via
`purego`, so the module builds with `CGO_ENABLED=0` and no audio dev headers. At
runtime it needs a reachable PulseAudio server (set `PULSE_SERVER` if it isn't
auto-discovered) or `libasound.so.2` present for the ALSA fallback. Pinned to
`v3.5.0-alpha.x` — the cgo-free Linux rewrite isn't in stable `v3.4.0` yet.
Capture stays in [`xcono/asr`](../asr) (the mic, on PortAudio); this owns the
speaker. The playback timeline lives in `pkg/voiceq`.

## Standalone debug binaries

`cmd/say` synthesises text and writes a WAV (pure-Go, no audio device — runs
headless); `cmd/play` synthesises **and plays** it on the speaker (oto):

```bash
go run ./cmd/say  -text "Привет, мир" -o out.wav         # local Qwen3-TTS (:8007) → WAV
go run ./cmd/say  -config config.json -text "..."        # provider from config
aplay out.wav                                            # play the WAV
go run ./cmd/play -text "Привет, мир"                    # synthesise AND play (needs a speaker)
```

## The TTS server (external dependency)

`llm/qwen3-tts/openai_server.py` is the faster-qwen3-tts OpenAI-compatible server
(Python — the model lib has no Go equivalent). Run it via Docker:

```bash
docker compose up -d        # builds llm/qwen3-tts, serves :8007
```

The compose joins the shared `voices` network and reuses the `llms_hf-cache`
volume, so it co-resides with the [`xcono/voices`](../voices) stack
(`tts:8007`) and still runs on its own. **Needs the GPU box + NVIDIA Container
Toolkit — don't build in a sandbox.**

## Layout

- `tts.go` — the `Service` synthesis facade (Stream/Batch/Status).
- `provider.go` / `client.go` / `elevenlabs.go` / `fallback.go` — the `Provider` contract + backends.
- `pkg/play` — the `Speaker` playback facade: owns the oto output device + the playback loop.
- `pkg/voiceq` — the playback timeline (pure queue; no device).
- `pkg/audio` — PCM16LE codec (no device).
- `pkg/config` — config schema + loader.
- `cmd/say` — debug binary: synthesise → WAV (headless).
- `cmd/play` — debug binary: synthesise → play on the speaker (oto).
- `llm/qwen3-tts` — the Python TTS server + its Dockerfile.
