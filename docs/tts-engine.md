# TTS engine — current state

The one current document about the local speech engine: what runs, why, how it
measures, and what is left. Everything under `docs/archive/` is history.

Last updated 2026-11-18.

## What runs

| | |
|---|---|
| Model | [OmniVoice](https://github.com/k2-fsa/OmniVoice), weights `k2-fsa/OmniVoice` (3.27 GB, in the `llms_hf-cache` volume). Weights are **CC-BY-NC**. |
| Server | [vLLM-Omni](https://github.com/vllm-project/vllm-omni) `v0.32.0`, image `xcono-tts-vllm` built from `llm/vllm-omni/`, compose service `tts`, port 8007 |
| Contract | `POST /v1/audio/speech`: one text fragment + the voice → one whole clip, raw 24 kHz mono int16 LE (`response_format: "pcm"`); `GET /health` |
| Voice | cloned from `llm/vllm-omni/ref_stas.wav` (11.8 s, baked into the image at `/ref/`) with its exact transcript `ref_stas.txt`; sent in every request |
| Settings | 32 steps, guidance 2.0, language `ru` — chosen by ear, do not change for speed. Defaults in `pkg/config`. |
| GPU | RTX 5060 Ti 16 GB (Blackwell sm_120), compose default `TTS_GPU=1`; about 5.3 GB on the card with the server idle (`nvidia-smi`, 2026-11-18) |

The server is stateless and returns a clip whole. Everything about conversation
is in the Go module: `pkg/play.Speaker` decides what goes into each request,
queues, plays, and handles barge-in.

## Architecture (decided 2026-11-17, implemented 2026-11-18)

1. **Models behind a plain HTTP API, conversation logic in Go.** "Text fragment +
   voice → audio of that fragment". No WebSocket, no server-side streaming
   protocol. vLLM-Omni's full-duplex and realtime APIs are not used: they serve
   end-to-end speech models and merge ear, brain and mouth.
2. **The Go side owns** text cutting, look-ahead synthesis, the queue, playback
   and interruption (this module), capture and VAD (`xcono/asr`), and the brain
   (`xcono/voices`). Contract for the brain: `Enqueue(text)`, `Interrupt()`,
   `Status()`.
3. Whisper through vLLM instead of whisper.cpp is a uniformity question for the
   `asr` repo, not decided. GigaAM is no longer a candidate.

## How text becomes requests

Implemented in `pkg/play/speaker.go`; the reasons are measured facts about this
server and model.

- **One request at a time**; the next goes out as soon as the previous returns.
- **The first enqueued text goes out as is** — first sound in about 0.5 s.
- **Text queued meanwhile is merged** into one request, up to 250 characters
  (about 18 s of speech; the model is stable to about 30 s per request). The
  model intones one request as one passage.
- **One fragment in reserve** beyond the one playing. Synthesis is about ten
  times faster than speech, so a larger reserve brings sound no sooner; it only
  takes away the chance to merge and adds work the server cannot cancel.
- **Barge-in** flushes the device, closes the request and drops the queue.

Listening result (owner, 2026-11-18, five sentences fed 300 ms apart):

| | Merged (cap 250) | A request per sentence (cap 0) |
|---|---|---|
| Requests | 3 | 5 |
| First sound | 0.49 s | 0.50 s |
| Verdict | sounds very good | stress errors appear; every sentence is intoned separately, "like a call to battle" |

The seam between the first fragment and the second is audible: a jump in tone,
the second part starts louder. Left as is for now. Ideas not tried: level-match
neighbouring clips in Go; make the first fragment longer than one sentence.

## Measurements (one protocol)

Protocol: two fixed phrases, 5 repeats each, **15 s idle before every request**,
GPU 1 at ~20 W and 37–42 °C before each request, time from request to the full
audio of one phrase. Settings identical everywhere: our voice, 32 steps,
guidance 2.0, Russian. Script: `docs/archive/2026-11-tts-selection/lab/latency/proto_vllm_omni.py PORT`.

- Short (56 chars, 4.2 s audio): «Тесты прошли, изменения закоммичены, pull request открыт.»
- Long (106 chars, 7.4 s audio): «Репозиторий лежит в корневой папке, все Go-файлы целы и не изменены, ветка main, последний коммит прежний.»

| Variant | Short | Long |
|---|---|---|
| Hand-written Python server (PyTorch fp16, omnivoice 0.2.1) — removed | 1.27 s | 1.51 s |
| audio.cpp, f16 + `flash_attention` — rejected | 0.77 s | 1.00 s |
| vLLM-Omni 0.32.0, bf16, defaults | 0.70 s | 0.88 s |
| vLLM-Omni, bf16 + FLASH_ATTN (patch 1) | 0.55 s | 0.66 s |
| **vLLM-Omni, bf16 + FLASH_ATTN + graph alignment 32 (patches 1+2) — what runs** | **0.48 s** | **0.66 s** |
| Python server + FlashInfer — removed | 0.46 s | 0.64 s |

Spread between repeats is 1–5 ms in every row. The image built from
`llm/vllm-omni/` was re-measured on 2026-11-18: 0.484 / 0.663 s.

Other measured facts (2026-11-17/18, raw logs were kept outside the repo):

- **No cancellation.** Closing the connection does not stop the render: OmniVoice
  in v0.32.0 does not opt into in-run cancellation. A 29 s paragraph (2.0 s of
  compute) aborted at 0.3 s kept the card busy 1.8–2.2 s more; a short phrase
  sent right after came back in 1.82 s instead of 0.48 s. With fragments capped
  at 250 characters the worst case is about 1.3 s; in practice ASR and the LLM
  take longer than that after a barge-in.
- **Idle does not slow it.** 0.488 s after 5 minutes idle, 0.487 s after 20
  minutes (base 0.48 s). Only the first request after server start is slower
  (0.64 s); no warm-up is done.
- **A stored voice is not faster.** `POST /v1/audio/voices` saves the clip; a
  request by voice name gets it substituted as `ref_audio` (0.66 s vs 0.663 s).
  Stored voices live in `SPEAKER_SAMPLES_DIR` inside the container and vanish
  when it is recreated. Hence: the reference goes in every request.
- **Through the Speaker**, with a real device: first sound 0.49 s; a new reply
  after barge-in is requested within 0.1 s and rendered in 0.4 s.

Earlier back-to-back runs held the card at 100 % / 150–170 W / 70 °C. That load
pattern is not acceptable on this machine — always measure with pauses.

## The server image

`llm/vllm-omni/`: `Dockerfile`, `omnivoice.yaml`, `patches/`, `ref_stas.wav`,
`ref_stas.txt`.

- Base `vllm/vllm-omni:v0.32.0` (9.95 GB compressed, 35 GB on disk; torch 2.13,
  CUDA 13.0 — driver 580.178 reports CUDA 13.0). Model from the shared HF cache,
  `HF_HUB_OFFLINE=1` by default.
- Deploy config: `dtype: bfloat16`, `max_num_seqs: 1`,
  `gpu_memory_utilization: 0.5`; flag `--diffusion-attention-backend FLASH_ATTN`.
- **Patch 1** (4 lines, `platforms/cuda/platform.py`): allow the bundled
  FlashAttention 2 on sm_120. Without it the server refuses FLASH_ATTN on this card.
- **Patch 2** (one constant, `omnivoice_generator.py`):
  `_LAZY_CAPTURE_ALIGNMENT` 128 → 32, less padding for single requests.
- Patches are applied at build time with `git apply`; the build fails if the
  source no longer matches. They are tied to v0.32.0 — re-derive on any upgrade.
- Build with `BUILDX_BUILDER=default`: a `docker-container` builder pulls the
  35 GB base again into its own cache.
- **Fixed post-processing** on cloned output: gaps ≥ 500 ms removed, edge silence
  trimmed, 100 ms fades and 100 ms of silence added, level matched to the
  reference. It cannot be switched off. The owner listened: pauses are shorter,
  the result sounds good and natural.
- Worker logs are lost in v0.32.0 (vllm-omni#8517); the archive has a
  `sitecustomize` workaround (`lab/latency/vllm-omni/`).
- Did not help: `enforce_eager: false` (torch.compile), `step_execution`,
  float16 (slower than bf16), no CUDA graphs. Not tried: `OMNIVOICE_TF32`, FA4.

## Rejected

- **The hand-written Python server** (with or without FlashInfer). FlashInfer
  was marginally the fastest (0.46 / 0.64 s) but needed our own server, OmniVoice
  from an unreleased git commit and an override for sm_120. Removed on
  2026-11-18; last commit with it: `ad2466d`.
- **audio.cpp** — runs on this card from the published image and loads the
  original safetensors, but is slower than vLLM-Omni.
- **RealtimeTTS** — a Python library that does the cutting, queueing and playback
  itself, i.e. the role of the Go module; re-encodes the reference every sentence.
- Fewer steps (16) and a 5 s reference are faster but were rejected by ear.
- **A request per sentence** — see the listening result above.

## Known limits of the model

- **Stress.** Not trained on stress marks. Wrong on a few plain words
  («откАтил», «дЕплой», «аудиО»). A sparse U+0301 after the vowel on the
  offending word often fixes it; marking every word degrades the voice.
- **Text is read as written.** No Russian number or abbreviation expansion.
  Latin IT terms are read correctly as is.
- **No emotion control**; only inline tags such as `[laughter]`, `[sigh]`.
- **No streaming inside a request** — a clip is ready whole or not at all.
- **The last word can be dropped** when a Russian fragment has no final stop
  (OmniVoice #204); `tts.Client` appends one.
- The reference clip is processed on every pass: the same sentence without a
  reference renders about 2.5× faster. Reference length is the largest remaining
  speed lever, at a cost the owner hears.
- The same text does not always take the same time to say (1.7–2.1 s for one
  short phrase across runs): the model is not deterministic.

## Not done

- A dictionary for numbers, abbreviations and the few mis-stressed words (text
  preparation before `Enqueue`).
- The audible seam after the first fragment.
- Up to 100 ms between a finished synthesis and the next request: the playback
  loop looks at events between 100 ms device writes. Does not affect first sound.

## Why OmniVoice

Chosen by the owner's ear on his own text and voice, not by benchmarks.

| Model | Result |
|---|---|
| **OmniVoice** | Won a blind A/B against ZONOS3-Mini 9 pairs out of 10. Deep voice, natural intonation, pauses and breathing. |
| ZONOS3-Mini | Clean and fast, but a narrower, slightly metallic sound. Has true streaming, emotion controls, Apache-2.0 weights. The runner-up. |
| Qwen3-TTS 1.7B ru-stress-cf | Honours stress marks, but 4× slower, with a slight foreign accent. |

The search itself — reports, scripts, Dockerfiles for the runner-ups — is in
`docs/archive/2026-11-tts-selection/`.

## Rules that came out of this work

- GPU only. No CPU inference or CPU offloading on this machine, even for a test.
- Measure with pauses between requests; never run requests back to back.
- Pin versions: image tags, Python deps, git commits.
- Judge models by listening on the owner's text; benchmarks only shortlist.
- Blind listening is for choosing by quality, not for speed-ups.
