# TTS engine — current state

The one current document about the local speech engine: what runs, why, how it
behaves, and what is next. Everything under `docs/archive/` is history.

Last updated 2026-11-17.

## What runs

| | |
|---|---|
| Engine | [OmniVoice](https://github.com/k2-fsa/OmniVoice) 0.2.1, weights `k2-fsa/OmniVoice` (3.27 GB, in the `llms_hf-cache` volume) |
| Server | `llm/omnivoice/openai_server.py`, image `xcono-tts`, compose service `tts`, port 8007 |
| Contract | `POST /v1/audio/speech`, `response_format: "pcm"` → raw 24 kHz mono int16 LE; `GET /health` |
| Voice | cloned from `llm/qwen3-tts/ref_stas.wav` (11.8 s) with its transcript; encoded once at startup |
| Settings | `num_step` 32, `guidance_scale` 2.0, `language` ru |
| Pinned deps | torch 2.11.0+cu128, transformers 5.15.1, omnivoice 0.2.1 (CUDA 12.8.1 base) |
| GPU | RTX 5060 Ti 16 GB (Blackwell sm_120); compose default `TTS_GPU=1`; uses about 2.5 GB VRAM |
| License | code Apache-2.0, **weights CC-BY-NC** (non-commercial) |

The Go module is unchanged: `Client` talks to this server through the config key
`qwen3.base_url` (historical name). The previous engine, faster-qwen3-tts
(`llm/qwen3-tts`, Qwen3-TTS 0.6B-Base), is kept behind the compose profile
`qwen3` on port 8009.

## Why OmniVoice

Chosen by the owner's ear on his own text and voice, not by benchmarks.
Three small models were installed and listened to; heavy models (Fish S2 Pro,
Fish Drama 3) were excluded up front because of fan noise.

| Model | Result |
|---|---|
| **OmniVoice** | Won a blind A/B against ZONOS3-Mini 9 pairs out of 10 on ten paragraphs of real agent replies. Deep voice, natural intonation, pauses and breathing. |
| ZONOS3-Mini | Clean and fast, but a narrower, slightly metallic sound. Has what OmniVoice lacks: true streaming, emotion controls, a C++ server without Python, Apache-2.0 weights. The runner-up. |
| Qwen3-TTS 1.7B ru-stress-cf | Honours stress marks on every word, but 4× slower, with a slight foreign accent (soft «е» in «трейс»). |

On real agent text (Russian with Latin IT terms, numbers written as words) both
finalists made almost no mistakes: one wrong stress in ten paragraphs.

## Measured behaviour (GPU 1, 2026-11-17)

- **Time to first audio 1.0–1.4 s** through `cmd/say`. The server splits the
  input into sentences and streams each one when rendered; a sentence costs a
  fixed ~1.2 s (32 passes) plus ~0.08 s per second of audio.
- **Whole 17 s paragraph in 3.4 s.**
- **VRAM flat** over 50 consecutive requests (4618 MiB on the card before and
  after, shared with other processes), with `PYTORCH_CUDA_ALLOC_CONF=expandable_segments:True`.
- Per-sentence latency by setting (one short sentence):

  | Reference | 32 steps | 16 steps |
  |---|---|---|
  | 12 s (current) | 1.2 s | 0.6 s |
  | 5 s | 0.7 s | 0.36 s |

  In a blind listen the owner preferred 32 steps in both test phrases; reference
  length showed no consistent preference. Keep 32 steps. A 5 s reference is a
  possible later lever, not adopted (one sample had whistling sibilants).
- The server sets no seed: the same text sounds slightly different each time.

## Known limits

- **Stress.** The model was not trained on stress marks. Wrong on a few plain
  words («откАтил», «дЕплой», «аудиО»). A sparse U+0301 after the vowel on the
  offending word often fixes it; marking every word degrades the voice.
- **Text is read as written.** No Russian number or abbreviation expansion —
  convert to words before synthesis. Latin IT terms are read correctly as is.
- **No emotion control**; only inline non-verbal tags such as `[laughter]`, `[sigh]`.
- **No true streaming** — sentence-level chunks only.
- **Last word can be cut** in Russian without a final stop; the server appends one.
- Fragments under 25 characters are merged into the next sentence (the model is
  unstable on one- or two-word clips).

## Next steps

1. **FlashInfer** — the lossless speed-up shipped with OmniVoice (claimed 2–2.9×,
   CUDA graphs recommended for single requests):
   `pip install flashinfer-python==0.6.15.post1 "flashinfer-jit-cache==0.6.15.post1+cu128" --extra-index-url https://flashinfer.ai/whl/cu128/`,
   then `from omnivoice.models.omnivoice_flashinfer import apply_flashinfer; apply_flashinfer(model, enable_cuda_graph=True)`.
   Not yet tried; support for sm_120 is unverified.
2. **[audio.cpp](https://github.com/0xShug0/audio.cpp)** — GGML engine without
   Python, supports OmniVoice voice cloning on CUDA with a server and chunked
   output; claimed 1.3–1.5× faster than Python. Candidate to replace the Python
   server. Not tried.
3. Text preparation for the agent: a term dictionary with sparse stress marks,
   number expansion (`ru-normalizr`), an accentuator (`silero-stress` or RUAccent)
   applied only to known problem words.
4. If stress errors become a problem: OmniVoice documents LoRA fine-tuning
   (`docs/lora_finetuning.md` upstream).

Not useful now: vLLM-Omni serves OmniVoice online only with the automatic voice
(no cloning); `omnivoice-rs` is a small Rust port; `omnivoice-trtllm` targets Modal.

## Reviving the runner-ups

**ZONOS3-Mini** (if streaming or emotions are needed). Source and CUDA build are
kept outside the repo in `~/.cache/tts-lab/zonos3/src` (`servers/ggml/build-cuda`,
GGUF files in `servers/ggml/gguf/`); build image `xcono-tts-lab:zonos3-build`
(Dockerfile in the archive, `lab/zonos3/`). Run:

```bash
docker run -d --name zonos3-lab --gpus device=0 -p 1919:1919 \
  -v ~/.cache/tts-lab/zonos3/src:/src xcono-tts-lab:zonos3-build \
  ./build-cuda/zonos3-server gguf/backbone-ptq.gguf --gpu --host 0.0.0.0 --port 1919
```

`POST /v2/generate` with `language: "ru"`, `text_normalization: false` (the
Russian normalizer spells Latin words letter by letter), `decoder: "full_mr"`,
`speaker_mode: "embedding"`, `stream: false`, `format: "wav"`. Output is
44.1 kHz, so the Go module would need resampling. About 3 s for 24 s of speech.
Never download the whole HF repo (30.7 GB); the four GGUF files are enough.

**Qwen3-TTS 1.7B ru-stress-cf** (`siriusfreak/qwen3-tts-12hz-1.7b-ru-stress-cf`).
Loads with transformers 5.15.1 (5.20 breaks it). 24 requests in a row on GPU 0
without the wedge seen in June 2026. The `faster-qwen3-tts` path cuts about half
of short phrases after a fraction of a second; the stock `qwen_tts` path does not,
but has no streaming. Stress is U+0301 after the vowel. 13–14 s for 25 s of
speech, 4.6 GB VRAM.

## Rules that came out of this work

- GPU only. No CPU inference on this machine, not even for a quick test.
- Pin Python dependencies in every server image.
- Judge models by listening on the owner's text; use benchmarks only to shortlist.
