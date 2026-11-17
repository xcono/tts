# tts

Decoupled text-to-speech **"mouth"** for the realtime voice agent: a cgo-free TTS
client library + synthesis facade (`Service`) + a device-owning playback facade
(`play.Speaker`), consumed **in-process** by [`xcono/voices`](../voices). Sibling
of [`xcono/asr`](../asr) (the STT "ears" — which owns the mic).

**Source PoC:** `xcono/voices` — this repo extracts the TTS client, adds the
`Service` synthesis facade, and (per the audio-ownership split) absorbs the
**speaker**: the output device + the orchestrator's playback loop (`pkg/play`,
`pkg/voiceq`), so voices does no audio. Out of scope: VAD, ASR, **audio capture**
(the mic stays in asr), the orchestrator *mind* (answer routing / personas stays
in voices' `act`), and **NATS** (the library streams over Go channels).

## Build / run

```bash
go build ./...      # core + cmd/say are pure-Go; pkg/play/cmd/play use oto (still cgo-free)
go test ./...
go run ./cmd/say  -text "Привет" -o out.wav   # synthesise to WAV (headless, no device)
go run ./cmd/play -text "Привет"              # synthesise AND play on the speaker (oto)
docker compose up -d                          # the Python TTS server on :8007
```

A `go.work` at the `xcono/` parent (`use ./asr ./tts ./voices`) lets gopls and
`go build` resolve the cross-module imports when developing alongside voices.

## Architecture / data flow

```
tts.Service (tts.go) ─┬─ Stream(ctx, text, emit)  play-as-you-go (PCM, no device)
                      ├─ Batch(ctx, text)          accumulate the stream
                      └─ Status()                  ready|processing|speaking
                          └─ Provider (provider.go): Client | ElevenLabs | Fallback

play.Speaker (pkg/play) ─┬─ Enqueue(text)   queue a reply; splits paragraphs, plays in order
                         ├─ Interrupt()     barge-in: stop current within ~one chunk
                         ├─ Status()        ready|processing|speaking
                         └─ Close()
                             ├─ Synth (SynthesizeStream) — any Provider above
                             └─ oto output device (cgo-free) via pkg/voiceq timeline
```

- `tts.go` — `Service` facade. `New(cfg)` selects the provider from config;
  `NewWith(p)` injects any `Provider`. `Stream` drives `Status` (Processing →
  Speaking on first chunk → Ready on return); `Batch` routes through `Stream`.
- `provider.go` — `Provider` = `Synthesizer` (streaming) + one-shot `Synthesize`.
  `Client` (local server), `ElevenLabs` (cloud), and `Fallback` all satisfy it.
- `client.go` — `POST /v1/audio/speech` with `response_format:"pcm"`; emits each
  24 kHz mono int16 LE chunk as it arrives.
- `pkg/play` — the **speaker**: `Speaker` owns the output device and plays
  enqueued replies (look-ahead synthesis, play-as-you-go, chunked barge-in). The
  device is `github.com/ebitengine/oto/v3` — **cgo-free** on Linux (pure-Go
  PulseAudio via `jfreymuth/pulse`, ALSA fallback loaded via `purego`), so the
  module builds with `CGO_ENABLED=0` and needs no audio dev headers. **Runtime:** a
  reachable PulseAudio server (set `PULSE_SERVER` if not auto-discovered) or
  `libasound.so.2` present for the ALSA fallback. Pinned to `v3.5.0-alpha.x` —
  the cgo-free unix rewrite isn't in stable `v3.4.0` yet (see go.mod). `speaker.go`
  is the engine (extracted from the voices orchestrator's playback loop); `oto.go`
  is the device sink (an `io.Pipe` the oto player drains, so `Write` blocks/paces).
- `pkg/voiceq` — the playback timeline: a pure, navigable, session-grouped queue
  of synthesised replies. No device, no goroutine — `Speaker` mutates it from its
  loop. Moved here from voices with the playback engine.
- `pkg/audio` — PCM16LE encode/decode only. No device (the device lives in
  `pkg/play`); keeping it codec-only keeps `cmd/say` pure-Go/headless.
- `pkg/config` — schema + `Load` (mirrors the asr repo's config shape).
- `cmd/say` — standalone debug binary: synthesise → WAV, log status + TTFA (no device).
- `cmd/play` — standalone debug binary: synthesise → **play** on the speaker (oto).

## Wire format (hard constraint)

Output is **24 kHz / mono / int16 LE PCM** — `tts.SampleRate` (24000), fixed
(the server's rate). The capture/VAD side runs at 16 kHz; resample at the
boundary if you ever cross them (the consumer's concern, not this module's).

## Conventions

- Short package names, one concern per file. Interfaces at the consumer side.
- Error wrapping `fmt.Errorf("tts: ...: %w", err)`; package-prefixed messages.
- Doc comments on exported identifiers say *why*, not *what*.
- **Cgo-free, device isolated to `pkg/play`.** The whole module builds without
  cgo: the core (Service, providers, `pkg/audio`, `cmd/say`) is pure-Go and
  headless; the speaker (`pkg/play`, `cmd/play`) drives the **cgo-free** oto
  device. Don't pull an audio device or cgo dep into the core packages — if you
  need the device, you're in `pkg/play`.

## Gotchas

- **One utterance at a time.** `Status` is a single shared atomic — `Stream`/
  `Batch` are not safe to call concurrently on one `Service`. Serialise in the consumer.
- **`pcm` response format** — raw int16 stream, no header (`wav` prepends a
  fake-length streaming header; `mp3` is non-streaming).
- **Fallback is degrade-to-local** — ElevenLabs primary, local server secondary; a
  cloud outage still speaks. Don't invert the order.
- **Sentence-level streaming.** OmniVoice is not autoregressive; the server
  streams one sentence at a time, so chunks are whole sentences, not ~100 ms frames.
- **Text is read as written.** No number/abbreviation normalization for Russian,
  and marking stress on every word degrades the voice — mark only the words that
  come out wrong (U+0301 after the vowel).
- **Pin the Python deps.** An unpinned rebuild of the Qwen3 image pulled a
  transformers release that could not load the model; `llm/omnivoice/Dockerfile`
  pins torch, transformers and omnivoice to the set verified on the rig.
- **NATS-free by design.** If you need an event bus, it belongs in the consumer,
  not here — synthesis is a channel/callback stream.

## Python constraint

`llm/omnivoice/openai_server.py` (OmniVoice, the default engine since 2026-11-17)
is Python — the model lib has no Go equivalent, so it stays an external server
pointed at via `qwen3.base_url` (the config key keeps its historical name). The
Go side is a thin client. `llm/qwen3-tts` (faster-qwen3-tts) is the previous
engine, kept behind the compose profile `qwen3`; both servers speak the same
contract. If a Go-native TTS becomes viable, replace the `Client`.

**Direction (decided 2026-11-17, not yet implemented).** Models sit behind a
plain stateless HTTP API ("text fragment + voice → audio"); everything about
conversation lives in Go: text preparation, cutting text into fragments,
look-ahead synthesis, queueing, interruption. The hand-written Python server is
to be replaced by vLLM-Omni serving OmniVoice, which returns one whole clip per
request — so sentence cutting must move from `openai_server.py` into this module
first. No WebSocket and no vLLM-Omni duplex API. Details, measurements and the
working vLLM-Omni configuration: `docs/tts-engine.md`.

## Docs

- `docs/tts-engine.md` — the current engine: settings, measurements, limits,
  next steps, how to revive the runner-ups. Read this first for engine questions.
- `docs/specs/` — module specs.
- `docs/archive/` — **historical, superseded.** Research reports and experiment
  scripts from the engine search; do not use them as current facts.

## Development

Built iteratively by AI coding agents. Small reviewed increments. Keep
`README.md` (user-facing) and this file (agent-facing) consistent on behavior
changes.
