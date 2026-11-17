# tts

Decoupled **text-to-speech "mouth"** for the realtime voice agent. A cgo-free
client library over an OpenAI-compatible TTS server (local OmniVoice by
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
                              └─ Provider: Client (local server) | ElevenLabs | Fallback
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

- **qwen3** (default) — the local server via `POST /v1/audio/speech`
  (`response_format:"pcm"`): one text fragment and the voice to clone in, one
  whole PCM clip out. The key keeps its historical name; the server behind it is
  vLLM-Omni with OmniVoice (see below).
- **elevenlabs** — remote ElevenLabs Flash; streams `pcm_24000`.
- **fallback** — ElevenLabs primary, local server secondary (degrade-to-local), so
  a cloud/network outage still speaks.

## Config

`config.json` (schema in `pkg/config`); minimal `{}` defaults to the local server
and the stock voice:

```json
{
  "tts": {
    "provider": "qwen3",
    "qwen3": {
      "base_url": "http://localhost:8007/v1",
      "language": "ru",
      "ref_audio": "file:///ref/ref_stas.wav",
      "ref_text": "exact transcript of the reference clip",
      "steps": 32,
      "guidance": 2.0
    },
    "elevenlabs": { "api_key": "", "voice_id": "", "model": "", "base_url": "" }
  }
}
```

Every `qwen3` field is optional: the values above are the defaults
(`ref_text` defaults to the transcript of the clip baked into the server image).
`ref_audio` is a path as the server sees it. Build the client with
`tts.NewClient(cfg.TTS.Qwen3)` so the defaults apply.

## Speaker (the playback "mouth")

`pkg/play.Speaker` owns the **output device** and turns reply text into sound —
play-as-you-go, look-ahead synthesis, merging of queued text into larger
fragments, and chunk-granular **barge-in**. The
consumer drives it by text and control only (never PCM), so all audio stays in
this module:

```go
sp, err := play.NewSpeaker(&tts.Client{BaseURL: "http://localhost:8007/v1"})
defer sp.Close()

sp.Enqueue("Привет! Как дела?")   // queue text; call it per sentence as the LLM streams
sp.Interrupt()                    // barge-in: stop sound, abort the request, drop the queue
st := sp.Status()                 // ready | processing | speaking — by what is actually heard
```

The first enqueued text is synthesised at once, so sound starts in about half a
second. Text that arrives while it is being synthesised and played goes out
merged into one request (up to 250 characters; `play.WithMaxFragment`): the model
intones one request as one passage, which sounds markedly better than a request
per sentence. Requests go one at a time, one fragment ahead of playback.

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
go run ./cmd/say  -text "Привет, мир" -o out.wav         # local server (:8007) → WAV
go run ./cmd/say  -config config.json -text "..."        # provider from config
aplay out.wav                                            # play the WAV
go run ./cmd/play -text "Привет, мир"                    # synthesise AND play (needs a speaker)
```

## The TTS server (external dependency)

`llm/vllm-omni` builds [vLLM-Omni](https://github.com/vllm-project/vllm-omni)
v0.32.0 serving [OmniVoice](https://github.com/k2-fsa/OmniVoice), with two small
patches for the RTX 50xx and the reference voice baked in:

```bash
BUILDX_BUILDER=default docker compose up -d --build   # serves :8007
```

The server is stateless — a text fragment and a voice in, one clip out; about
0.5 s for a sentence on an RTX 5060 Ti. It reads the text as written: expand
numbers and abbreviations to words before synthesis. The model weights are
CC-BY-NC (non-commercial). Settings, measurements and what was rejected:
[`docs/tts-engine.md`](docs/tts-engine.md).

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
- `llm/vllm-omni` — the inference server image: Dockerfile, deploy config, two
  patches, the reference voice `ref_stas.wav` + its transcript.
