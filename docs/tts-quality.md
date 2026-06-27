# TTS quality — Qwen3-TTS 0.6B-Base control surface & levers

What we can and cannot do to improve synthesis quality on the deployed model
(`Qwen/Qwen3-TTS-12Hz-0.6B-Base`, voice-clone). Researched 2026-06-20 from the
installed `qwen_tts` library (authoritative for our exact checkpoint), the
Qwen3-TTS GitHub README, the HF model card, and the `faster-qwen3-tts` README.
Model/GPU facts live in [`infrastructure.md`](infrastructure.md); the normalization
work is tracked in [`../roadmap.md`](../roadmap.md) ("Next — TTS text normalization").

## How Qwen3-TTS control works (the family)

- **No markup / no SSML.** The only special tokens in the whole package are the
  standard Qwen chat tokens `<|im_start|>` / `<|im_end|>`. There is **no** `<break>`,
  no emphasis tag, no pause/emotion token — confirmed in code and docs ("not SSML
  markup or special tokens… no punctuation-based control is documented").
- **All expressive control is a natural-language `instruct` directive** (emotion,
  tone, speaking rate, prosody), passed as a `<|im_start|>user\n{instruct}<|im_end|>`
  turn. Examples from the README:
  - `instruct="用特别愤怒的语气说"`  — "speak very angrily"
  - `instruct="Speak in an incredulous tone, with panic creeping in"`
  - `instruct="体现撒娇稚嫩的萝莉女声，音调偏高且起伏明显"` — childlike voice, high pitch, strong variation
- The text itself is just BPE-tokenized (`Qwen2TokenizerFast`) over raw graphemes;
  numbers, abbreviations, and Latin words are **not** normalized by the model.

## What our 0.6B-Base actually exposes

Qwen3-TTS ships three model *types*; instruct/emotion control lives in the ones we
cannot run:

| capability | model type / size | available to us? |
|---|---|---|
| voice clone from reference audio (ICL) | **Base** (0.6B/1.7B) | ✅ **the only mode we have** |
| `instruct` emotion / tone / rate | CustomVoice + VoiceDesign | ❌ |
| rich natural-language voice design | VoiceDesign — **1.7B only** | ❌ (1.7B wedges GPU1 — see infrastructure.md) |

Sealed by two gates in `qwen_tts/inference/qwen3_tts_model.py`:
- `generate_custom_voice` (the instruct path) **raises** unless `tts_model_type == "custom_voice"`; ours is `base`.
- Even CustomVoice-0.6B drops it: `if tts_model_size in "0b6": instruct = None` ("for 0b6 model, instruct is not supported").
- Passing instruct to the Base voice-clone path is possible but flagged experimental/
  unreliable upstream — not a dependable lever.

**Consequence:** emphasis tags, pause markup, and instruct-based emotion are all
**unavailable** to us. The 1.7B that would unlock instruct/VoiceDesign is the model
that takes down GPU1 at the driver level.

## Prompt structure (ICL voice clone — our mode)

`x_vector_only_mode=False` (we pass `ref_text`), so the model conditions on the
reference speech + transcript, then continues with our text:

```
<|im_start|>assistant\n{ref_text}<|im_end|>\n        # reference turn — sets voice + baseline prosody
<|im_start|>assistant\n{text}<|im_end|>\n<|im_start|>assistant\n   # the text we want spoken
```

So our text lands in an assistant turn; the reference establishes identity, pace,
and baseline emotion.

## Levers we CAN use (ranked)

1. **Text normalization — primary, biggest correctness win.** The model reads
   graphemes verbatim, so expand numbers/dates/units → words, expand abbreviations,
   and transliterate Latin → Cyrillic. (Tracked in `roadmap.md`; deterministic,
   per-sentence, off the first-audio critical path.)
2. **Punctuation as de-facto prosody/pause control — *empirical*, not a documented
   knob.** The model learned pausing and intonation from punctuated text, so commas,
   periods, dashes, `…`, and `?`/`!` are how we place pauses and shape emphasis.
   There is no explicit pause token — repairing/normalizing punctuation (and testing
   whether `…`/`—` lengthen a pause) IS the pause mechanism. Verify by ear.
3. **Sentence segmentation.** Synthesis-unit boundaries set phrasing and pause
   placement (and already drive streaming). Clean sentence units → cleaner prosody.
4. **Reference audio + `ref_text`.** In ICL mode the reference sets voice *and*
   baseline emotion/pace — our only "emotion" dial. A more expressive reference WAV
   yields more expressive output; `ref_text` must match the audio.
5. **Sampling params** (`temperature`, `top_k`, `top_p`, `repetition_penalty`, on
   `generate_voice_clone`). Lower temperature = steadier/flatter; higher = livelier
   but less stable. Tune for naturalness vs stability.
6. **`language="Russian"`** — already set; gives language context (not normalization).

## Do NOT chase

- SSML / emphasis / pause **markup** — does not exist in this model.
- **instruct** emotion/style on 0.6B — gated off in code.
- **1.7B VoiceDesign/CustomVoice** for richer control — 1.7B wedges GPU1 (reboot to
  recover); not viable on this card.
- **G2P / IPA / phonemes / stress marks** — the model takes graphemes; combining
  accents become junk BPE tokens. (Why the phonetic half of `awesome-russian-speech`
  is out of scope.)

## Bottom line

On 0.6B-Base, quality is improved by **preparing the text**, not by instructing the
model: normalization (numbers, abbreviations, Latin→Cyrillic) is the main lever;
**punctuation normalization** is the only route to pause/emphasis; a **sampling-param
sweep** and a more expressive **reference voice** are the remaining dials.
