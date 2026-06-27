# tts

Decoupled **text-to-speech "mouth"** for the realtime voice agent. A pure-Go
client library + a small importable facade over an OpenAI-compatible TTS server
(local faster-qwen3-tts by default, cloud ElevenLabs optionally), consumed
in-process by [`xcono/voices`](../voices). No NATS — synthesis streams over Go
channels / an emit callback (the LLM loop plays the PCM).

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

## Standalone debug binary

`cmd/say` synthesises text and writes a WAV (pure-Go, no audio device — runs
headless), logging status transitions and time-to-first-audio:

```bash
go run ./cmd/say -text "Привет, мир" -o out.wav          # local Qwen3-TTS (:8007)
go run ./cmd/say -config config.json -text "..."         # provider from config
aplay out.wav                                            # play it
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

- `tts.go` — the `Service` facade (Stream/Batch/Status).
- `provider.go` / `client.go` / `elevenlabs.go` / `fallback.go` — the `Provider` contract + backends.
- `pkg/audio` — PCM16LE codec (no playback; the consumer owns the device).
- `pkg/config` — config schema + loader.
- `cmd/say` — standalone debug binary.
- `llm/qwen3-tts` — the Python TTS server + its Dockerfile.
