# TTS engine — current state

The one current document about the local speech engine: what runs, what was
decided, how it measures, and what is next. Everything under `docs/archive/` is
history.

Last updated 2026-11-17 (evening).

## What runs today

| | |
|---|---|
| Model | [OmniVoice](https://github.com/k2-fsa/OmniVoice), weights `k2-fsa/OmniVoice` (3.27 GB, in the `llms_hf-cache` volume). Weights are **CC-BY-NC**. |
| Server | `llm/omnivoice/openai_server.py`, image `xcono-tts`, compose service `tts`, port 8007 |
| Contract | `POST /v1/audio/speech`, `response_format: "pcm"` → raw 24 kHz mono int16 LE; `GET /health` |
| Voice | cloned from `llm/qwen3-tts/ref_stas.wav` (11.8 s) with its transcript; encoded once at startup |
| Settings | `num_step` 32, `guidance_scale` 2.0, `language` ru — chosen by ear, do not change for speed |
| Pinned deps | torch 2.11.0+cu128, transformers 5.15.1, omnivoice 0.2.1 (CUDA 12.8.1 base) |
| GPU | RTX 5060 Ti 16 GB (Blackwell sm_120), compose default `TTS_GPU=1`, about 2.5 GB VRAM |

The server splits the input into sentences and streams each sentence's PCM when
rendered. The Go module is a thin client (`qwen3.base_url`, historical key name).
The previous engine, faster-qwen3-tts, stays behind the compose profile `qwen3`
on port 8009.

## Decided direction (not yet implemented)

Agreed with the owner on 2026-11-17:

1. **Models behind a plain HTTP API, conversation logic in Go.** The inference
   server is stateless: "text fragment + voice → audio of that fragment" and
   "audio segment → text". No WebSocket, no server-side streaming protocol.
2. **The Go side owns** capture and VAD, the utterance queue, text preparation
   (numbers, abbreviations, sparse stress marks), cutting text into fragments,
   look-ahead synthesis, playback, and interruption (barge-in from VAD).
   Contract for the "brain": utterance text comes in from the ear; "say this" and
   "stop" go to the mouth.
3. **Inference server: vLLM-Omni** (`vllm/vllm-omni:v0.32.0`) serving
   `k2-fsa/OmniVoice`, replacing the hand-written Python server. Its full-duplex
   and realtime WebSocket APIs are *not* used: they serve end-to-end speech
   models (Qwen3-Omni, MiniCPM-o) and merge ear, brain and mouth.
4. Whisper through vLLM instead of whisper.cpp is a uniformity question for the
   `asr` repo, not a blocker. GigaAM is no longer a candidate.

Order of work: move fragment cutting into Go → switch the server to vLLM-Omni →
pin and document → stop touching the speech layer and move on to the brain.

Fragment-cutting idea to try first (owner's, generalised): send the first
sentence alone so audio starts at once; while a fragment plays, the next request
takes everything accumulated so far, capped at 15–20 s of audio (OmniVoice is
stable up to about 30 s per request; vLLM-Omni chunks above that by itself). A
first fragment under 2–3 s of audio does not cover the synthesis of the next one.
Whether larger fragments improve intonation is unverified — the owner judges by ear.

## Measurements (one protocol, 2026-11-17)

Protocol: two fixed phrases, 5 repeats each, **15 s idle before every request**,
GPU 1 at ~20 W and 37–42 °C before each request, time from request to the full
audio of one phrase. Settings identical everywhere: our voice, 32 steps,
guidance 2.0, Russian.

- Short (56 chars, 4.2 s audio): «Тесты прошли, изменения закоммичены, pull request открыт.»
- Long (106 chars, 7.4 s audio): «Репозиторий лежит в корневой папке, все Go-файлы целы и не изменены, ветка main, последний коммит прежний.»

| Variant | Short | Long | VRAM |
|---|---|---|---|
| Current server (PyTorch fp16, omnivoice 0.2.1) | 1.27 s | 1.51 s | 2.5 GB |
| audio.cpp, f16 + `flash_attention` | 0.77 s | 1.00 s | 2.9 GB |
| vLLM-Omni 0.32.0, bf16, defaults | 0.70 s | 0.88 s | 3.0 GB |
| vLLM-Omni, bf16 + FLASH_ATTN (patch 1) | 0.55 s | 0.66 s | 3.0 GB |
| **vLLM-Omni, bf16 + FLASH_ATTN + graph alignment 32 (patches 1+2)** | **0.48 s** | **0.66 s** | 3.0 GB |
| Python server + FlashInfer | 0.46 s | 0.64 s | 3.2 GB |

Spread between repeats is 1–5 ms in every row. Scripts, raw output and configs:
`docs/archive/2026-11-tts-selection/lab/latency/`.

Not measured: latency after a long idle (minutes); cancellation behaviour;
two vLLM processes on one card.

Earlier back-to-back runs held the card at 100 % / 150–170 W / 70 °C. Their
numbers happened to match the protocol, but that load pattern is not acceptable
on this machine — always measure with pauses.

## vLLM-Omni configuration that reached 0.48 / 0.66 s

Everything needed is in `docs/archive/2026-11-tts-selection/lab/latency/vllm-omni/`
(`start.sh`, `omnivoice-bf16.yaml`, `request.json`, patches). To be promoted to
`llm/` when the server is switched.

- Image `vllm/vllm-omni:v0.32.0` (9.95 GB compressed, 35 GB on disk; torch 2.13,
  CUDA 13.0 — driver 580.178 reports CUDA 13.0). Model from the existing HF
  cache, `HF_HUB_OFFLINE=1`.
- Deploy config: `dtype: bfloat16`, `max_num_seqs: 1`,
  `gpu_memory_utilization: 0.5`; flag `--diffusion-attention-backend FLASH_ATTN`.
- **Patch 1** (4 lines, `vllm_omni/platforms/cuda/platform.py`): allow the
  bundled FlashAttention 2 on sm_120. Without it the server refuses FLASH_ATTN
  on this card.
- **Patch 2** (one constant, `omnivoice_generator.py`):
  `_LAZY_CAPTURE_ALIGNMENT` 128 → 32, less padding for single requests.
- Both patched files are mounted read-only over the image; no rebuild. They are
  tied to v0.32.0 — re-derive them on any upgrade.
- Request: `ref_audio: "file:///ref/ref_stas.wav"` (needs
  `--allowed-local-media-path`), `ref_text`, `language: "ru"`,
  `response_format: "pcm"`, `extra_params: {num_inference_steps: 32, guidance_scale: 2.0}`.
  A voice can also be uploaded once via `POST /v1/audio/voices` (same speed).
- Worker logs are lost in v0.32.0 (vllm-omni#8517); `sitecustomize-worker-logs.py`
  mirrors them. With it the log confirms FLASH_ATTN and CUDA graphs are in use.

Differences from the current server that the Go side must account for:

- **No sentence streaming.** One request returns one whole clip; the server
  chunks only above ~30 s of estimated audio.
- **Fixed post-processing** on cloned output: gaps ≥ 500 ms removed, edge silence
  trimmed, 100 ms fades and 100 ms of silence added, level matched to the
  reference. It cannot be switched off. The owner listened: pauses are shorter,
  the result sounds good and natural.
- Did not help: `enforce_eager: false` (torch.compile), `step_execution`,
  float16 (slower than bf16), no CUDA graphs. Not tried: `OMNIVOICE_TF32`, FA4.

## Fallback: FlashInfer in the current Python server

Works on this card and is marginally the fastest, with the original model code.
Kept until the switch to vLLM-Omni is done.

- Lab image `xcono-tts-lab:flashinfer`, built by `llm/omnivoice/Dockerfile.flashinfer`
  on top of `xcono-tts`: `flashinfer-python==0.6.15.post1`,
  `flashinfer-jit-cache==0.6.15.post1+cu128`, and OmniVoice from git commit
  `08be0b4c` (PyPI 0.2.1 has no FlashInfer module).
- Server options, both off by default: `OMNIVOICE_FLASHINFER=off|on|graph`,
  `OMNIVOICE_SEED`.
- Required on sm_120: `FLASHINFER_CUDA_ARCH_LIST=12.0a` (FlashInfer 0.6.15
  rejects consumer Blackwell on CUDA 12.8; the prebuilt kernels are there) and
  `FLASHINFER_DISABLE_JIT=1`.
- Use mode `on`. Mode `graph` is no faster and grows VRAM with every new
  sentence length (11 GB after 90 requests).

## Rejected

- **audio.cpp** — runs on this card from the published image and loads the
  original safetensors, but is slower than both options above. Its `native`
  precision is f32; a fair comparison needs `omnivoice.generator_weight_type=f16`.
- **RealtimeTTS** — a Python library that does the cutting, queueing and playback
  itself, i.e. the role reserved for Go; re-encodes the reference every sentence.
- Fewer steps (16) and a 5 s reference are faster but were rejected by ear.

## Known limits of the model

- **Stress.** Not trained on stress marks. Wrong on a few plain words
  («откАтил», «дЕплой», «аудиО»). A sparse U+0301 after the vowel on the
  offending word often fixes it; marking every word degrades the voice.
- **Text is read as written.** No Russian number or abbreviation expansion.
  Latin IT terms are read correctly as is.
- **No emotion control**; only inline tags such as `[laughter]`, `[sigh]`.
- **No streaming inside a phrase** — a clip is ready whole or not at all.
- The reference clip is re-processed on every pass: the same sentence without a
  reference renders about 2.5× faster. Reference length is the largest remaining
  speed lever, at a cost the owner hears.
- Current Python server only: appends a final stop (last word can be cut in
  Russian without one) and merges fragments under 25 characters. This logic must
  move to Go with the cutting.

## Why OmniVoice

Chosen by the owner's ear on his own text and voice, not by benchmarks.

| Model | Result |
|---|---|
| **OmniVoice** | Won a blind A/B against ZONOS3-Mini 9 pairs out of 10. Deep voice, natural intonation, pauses and breathing. |
| ZONOS3-Mini | Clean and fast, but a narrower, slightly metallic sound. Has true streaming, emotion controls, Apache-2.0 weights. The runner-up. |
| Qwen3-TTS 1.7B ru-stress-cf | Honours stress marks, but 4× slower, with a slight foreign accent. |

## Reviving the runner-ups

**ZONOS3-Mini.** Source and CUDA build are kept outside the repo in
`~/.cache/tts-lab/zonos3/src`; build image `xcono-tts-lab:zonos3-build`
(Dockerfile in the archive, `lab/zonos3/`). Run:

```bash
docker run -d --name zonos3-lab --gpus device=0 -p 1919:1919 \
  -v ~/.cache/tts-lab/zonos3/src:/src xcono-tts-lab:zonos3-build \
  ./build-cuda/zonos3-server gguf/backbone-ptq.gguf --gpu --host 0.0.0.0 --port 1919
```

`POST /v2/generate` with `language: "ru"`, `text_normalization: false`,
`decoder: "full_mr"`, `speaker_mode: "embedding"`, `stream: false`,
`format: "wav"`. Output is 44.1 kHz. Never download the whole HF repo (30.7 GB).

**Qwen3-TTS 1.7B ru-stress-cf** (`siriusfreak/qwen3-tts-12hz-1.7b-ru-stress-cf`).
Loads with transformers 5.15.1 (5.20 breaks it). Use the stock `qwen_tts` path;
`faster-qwen3-tts` cuts about half of short phrases. 13–14 s for 25 s of speech.

## Rules that came out of this work

- GPU only. No CPU inference or CPU offloading on this machine, even for a test.
- Measure with pauses between requests; never run requests back to back.
- Pin versions: image tags, Python deps, git commits.
- Judge models by listening on the owner's text; benchmarks only shortlist.
- Blind listening is for choosing by quality, not for speed-ups.
