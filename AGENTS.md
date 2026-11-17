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
docker compose up -d                          # the inference server (vLLM-Omni + OmniVoice) on :8007
```

A `go.work` at the `xcono/` parent (`use ./asr ./tts ./voices`) lets gopls and
`go build` resolve the cross-module imports when developing alongside voices.

## Architecture / data flow

```
tts.Service (tts.go) ─┬─ Stream(ctx, text, emit)  play-as-you-go (PCM, no device)
                      ├─ Batch(ctx, text)          accumulate the stream
                      └─ Status()                  ready|processing|speaking
                          └─ Provider (provider.go): Client | ElevenLabs | Fallback

play.Speaker (pkg/play) ─┬─ Enqueue(text)   queue text; merged into fragments, played in order
                         ├─ Interrupt()     barge-in: stop sound, abort the request, drop the queue
                         ├─ Status()        ready|processing|speaking — by what is heard
                         └─ Close()
                             ├─ Synth (SynthesizeStream) — any Provider above
                             └─ oto output device (cgo-free) via pkg/voiceq timeline
```

- `tts.go` — `Service` facade. `New(cfg)` selects the provider from config;
  `NewWith(p)` injects any `Provider`. `Stream` drives `Status` (Processing →
  Speaking on first chunk → Ready on return); `Batch` routes through `Stream`.
- `provider.go` — `Provider` = `Synthesizer` (streaming) + one-shot `Synthesize`.
  `Client` (local server), `ElevenLabs` (cloud), and `Fallback` all satisfy it.
- `client.go` — `POST /v1/audio/speech` with `response_format:"pcm"`: one text
  fragment plus the voice (`language`, `ref_audio`, `ref_text`, `extra_params`)
  in, 24 kHz mono int16 LE out. `NewClient(cfg)` fills the project defaults;
  consumers use it instead of building `Client` by hand. Adds a final stop to a
  fragment that lacks one.
- `pkg/play` — the **speaker**: `Speaker` owns the output device and plays
  enqueued replies. It decides what goes into each request (see "How the Speaker
  cuts and queues" below). The
  device is `github.com/ebitengine/oto/v3` — **cgo-free** on Linux (pure-Go
  PulseAudio via `jfreymuth/pulse`, ALSA fallback loaded via `purego`), so the
  module builds with `CGO_ENABLED=0` and needs no audio dev headers. **Runtime:** a
  reachable PulseAudio server (set `PULSE_SERVER` if not auto-discovered) or
  `libasound.so.2` present for the ALSA fallback. Pinned to `v3.5.0-alpha.x` —
  the cgo-free unix rewrite isn't in stable `v3.4.0` yet (see go.mod). `speaker.go`
  is the engine (extracted from the voices orchestrator's playback loop); `oto.go`
  is the device sink (an `io.Pipe` the oto player drains, so `Write` blocks/paces).
- `pkg/voiceq` — the playback timeline: a pure, session-grouped queue of
  replies. No device, no goroutine — `Speaker` mutates it from its loop.
  `MergePending` joins queued text, `Abandon` is barge-in, a played message
  releases its audio.
- `pkg/audio` — PCM16LE encode/decode only. No device (the device lives in
  `pkg/play`); keeping it codec-only keeps `cmd/say` pure-Go/headless.
- `pkg/config` — schema + `Load` (mirrors the asr repo's config shape).
- `cmd/say` — standalone debug binary: synthesise → WAV, log status + TTFA (no device).
- `cmd/play` — standalone debug binary: synthesise → **play** on the speaker (oto).
  Feeds text sentence by sentence like voices does, logs every request, can
  barge in (`-interrupt-after`, `-then`, `-max-fragment`).

## How the Speaker cuts and queues

The server returns one whole clip per request and cannot cancel a running one,
so all of this is the Speaker's job (`pkg/play/speaker.go`):

- **One request at a time.** The next goes out as soon as the previous returns;
  playback does not hold it back.
- **First message as is**, so sound starts at once (about 0.5 s).
- **Queued text is merged.** A fragment sent ahead of playback first absorbs the
  messages queued behind it, up to `DefaultMaxFragment` (250 characters, about
  18 s of speech). Only whole messages are joined. The owner compared by ear:
  merged text sounds markedly better; a request per sentence gives stress errors
  and a separate intonation for every sentence. `WithMaxFragment(0)` turns
  merging off.
- **One fragment in reserve** beyond the one playing (`DefaultLookahead`).
  Synthesis is ~10× faster than speech, so more reserve brings nothing — but text
  already sent cannot be merged, and on barge-in the server still renders it.
- **Interrupt** flushes the device, aborts the request in flight, drops its late
  result and everything queued.
- **Status is what is heard.** Processing until the first chunk reaches the
  device; Speaking until the device has finished sounding (`sink.Pending`), not
  merely accepted the audio; then Ready.

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
- **No streaming inside a request.** OmniVoice is not autoregressive: a fragment
  comes back whole or not at all. First sound is bounded by the first fragment's
  synthesis time, which is why it is sent alone.
- **The server does not cancel.** Closing the connection frees the client, not
  the GPU: the fragment is rendered to the end, and the next request waits for it.
- **Text is read as written.** No number/abbreviation normalization for Russian,
  and marking stress on every word degrades the voice — mark only the words that
  come out wrong (U+0301 after the vowel).
- **Pin the server.** `llm/vllm-omni/Dockerfile` pins the image tag; its two
  patches are tied to that tag and fail the build if the source moved. Build with
  `BUILDX_BUILDER=default` — a container builder re-pulls the 35 GB base image.
- **Voice = clip + exact transcript.** `ref_stas.wav` in the image and
  `DefaultRefText` in `pkg/config` are a pair; change them together.
- **NATS-free by design.** If you need an event bus, it belongs in the consumer,
  not here — synthesis is a channel/callback stream.

## The inference server

`llm/vllm-omni` — vLLM-Omni v0.32.0 serving `k2-fsa/OmniVoice` on :8007, the only
Python in the stack and none of it ours: a Dockerfile, a deploy config, two small
patches, the reference voice. It is stateless — "text fragment + voice → audio";
no WebSocket, no vLLM-Omni duplex API. The Go side points at it via
`qwen3.base_url` (the config key keeps its historical name, voices reads it too).
Measurements, the patches and what was rejected: `docs/tts-engine.md`.

## Docs

- `docs/tts-engine.md` — the current engine: settings, measurements, limits,
  what was rejected and why. Read this first for engine questions.
- `docs/specs/` — module specs.
- `docs/archive/` — **historical, superseded.** Research reports and experiment
  scripts from the engine search; do not use them as current facts.

## Development

Built iteratively by AI coding agents. Small reviewed increments. Keep
`README.md` (user-facing) and this file (agent-facing) consistent on behavior
changes.
