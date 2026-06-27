# Spec 01 — Importable TTS module + facade (`package tts`)

## Context
Per the decoupling plan (stt repo `goal.txt`), TTS becomes its own Go module,
`github.com/xcono/tts`, imported in-process by `../voices` (the LLM loop) over Go
channels/callbacks — no NATS. The synthesis code was ported from
`../voices/pkg/tts`; this repo adds the facade, config, and a copied minimal
`pkg/audio`.

## Goal
A `tts.Service` facade mirroring `stt.Service`: Stream / Batch + a Status signal,
over interchangeable providers (local Qwen3, cloud ElevenLabs, degrade-to-local
Fallback).

## Layout
Flattened — unlike stt (which kept a pre-existing `pkg/asr`), TTS is small and
cgo-free, so providers and facade share the root `package tts`:

```
tts.go         Service (facade) + Status + New/NewWith
provider.go    Provider / Synthesizer interfaces
client.go      *Client (local Qwen3-TTS) + shared streamPCM16LE / accumulate
elevenlabs.go  *ElevenLabs (cloud)
fallback.go    *Fallback (degrade-to-local)
pkg/audio      PCM16LE / DecodePCM16LE (copied; TTS needs no capture/resample)
pkg/config     Config schema + Load (mirrors stt's pkg/config)
```

## Contract (`tts.go`)
```go
func New(cfg *config.Config) (*Service, error) // provider per config
func NewWith(p Provider) *Service               // inject any Provider

func (s *Service) Stream(ctx, text, emit func([]int16) error) error // play-as-you-go
func (s *Service) Batch(ctx, text) ([]int16, error)                 // whole clip
func (s *Service) Status() Status                                   // ready|processing|speaking
```

## Design decisions
- **Returns concrete `*Service`**, like stt — `../voices` declares its own narrow
  consumer interface (e.g. `type Mouth interface { Stream(...); Status() tts.Status }`).
- **Status** is an `atomic.Int32`: `Ready` → `Processing` (request sent) →
  `Speaking` (first chunk emitted) → `Ready` (on return). The TTS counterpart of
  stt's `IsSpeaking`; it is what lets the loop honour "VAD TRUE → interrupt TTS"
  and "don't start a new utterance while one plays."
- **One utterance at a time** — Status is a single shared value, so Stream/Batch
  aren't for concurrent use on one Service. The consumer serialises.
- **Batch routes through Stream** so status tracking is identical on both paths.
- **`New` default provider is local Qwen3** — a minimal config with no cloud
  creds still runs. `"fallback"` wires ElevenLabs primary + Qwen3 secondary.
- **Shared `pkg/audio` is copied**, not centralised (per the plan), until drift
  forces an `xcono/audio` module.

## Tests
Ported provider tests (client/elevenlabs/fallback, httptest-backed) + facade
tests (status lifecycle, speaking-during-stream, reset-after-error, provider
selection). `go test -race ./...` clean.

## Not in scope (deferred)
A standalone debug binary (`cmd/...`), the TTS `docker-compose.yaml` + Python
server wiring (rename `llm/qwen3-tts/asr_server.py` → `tts_server.py`), and
`README.md`/`AGENTS.md`. The Python server already lives at `llm/qwen3-tts/`.
