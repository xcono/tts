# tts

Decoupled text-to-speech module for the realtime voice agent: a pure-Go TTS
client library + a small importable facade, consumed **in-process** by
[`xcono/voices`](../voices). Sibling of [`xcono/asr`](../asr) (the STT "ears").

**Source PoC:** `xcono/voices` `pkg/tts` — this repo extracts the TTS client +
adds the `Service` facade and a standalone debug path. Out of scope: VAD, ASR,
the orchestrator turn loop, audio capture, and **NATS** (the library streams over
Go channels — NATS can't stream bytes).

## Build / run

```bash
go build ./...      # pure-Go, no cgo
go test ./...
go run ./cmd/say -text "Привет" -o out.wav   # synthesise to WAV (headless)
docker compose up -d                          # the Python TTS server on :8007
```

A `go.work` at the `xcono/` parent (`use ./asr ./tts ./voices`) lets gopls and
`go build` resolve the cross-module imports when developing alongside voices.

## Architecture / data flow

```
tts.Service (tts.go) ─┬─ Stream(ctx, text, emit)  play-as-you-go
                      ├─ Batch(ctx, text)          accumulate the stream
                      └─ Status()                  ready|processing|speaking
                          └─ Provider (provider.go): Client | ElevenLabs | Fallback
```

- `tts.go` — `Service` facade. `New(cfg)` selects the provider from config;
  `NewWith(p)` injects any `Provider`. `Stream` drives `Status` (Processing →
  Speaking on first chunk → Ready on return); `Batch` routes through `Stream`.
- `provider.go` — `Provider` = `Synthesizer` (streaming) + one-shot `Synthesize`.
  `Client` (local server), `ElevenLabs` (cloud), and `Fallback` all satisfy it.
- `client.go` — `POST /v1/audio/speech` with `response_format:"pcm"`; emits each
  24 kHz mono int16 LE chunk as it arrives.
- `pkg/audio` — PCM16LE encode/decode only. **No playback** — the consumer
  (voices) owns the audio device; keeping device/cgo out keeps this module pure-Go.
- `pkg/config` — schema + `Load` (mirrors the asr repo's config shape).
- `cmd/say` — standalone debug binary: synthesise → WAV, log status + TTFA.

## Wire format (hard constraint)

Output is **24 kHz / mono / int16 LE PCM** — `tts.SampleRate` (24000), fixed
(the server's rate). The capture/VAD side runs at 16 kHz; resample at the
boundary if you ever cross them (the consumer's concern, not this module's).

## Conventions

- Short package names, one concern per file. Interfaces at the consumer side.
- Error wrapping `fmt.Errorf("tts: ...: %w", err)`; package-prefixed messages.
- Doc comments on exported identifiers say *why*, not *what*.
- Keep this module **pure-Go** — no cgo, no audio device. `cmd/say` writes a WAV
  rather than playing, precisely to preserve that.

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
