# Archive — historical, not current

Nothing here describes how the module works today. Do not read these files to
answer a question about the current engine, settings or performance: use
[`../tts-engine.md`](../tts-engine.md), `README.md` and `AGENTS.md`.

- `qwen3-tts-0.6b-quality.md` — control surface of Qwen3-TTS 0.6B-Base, the
  engine before 2026-11-17. Its server (`llm/qwen3-tts`) was removed from the
  repository on 2026-11-18; the last commit that has it is `ad2466d`.
- `2026-11-tts-selection/` — the November 2026 engine search.
  - Three web-research reports (new models on Hugging Face, the Russian
    ecosystem, quality evidence). Written before anything was installed; several
    claims were later contradicted by listening (for example, the reports ranked
    Qwen3 stress fine-tune first; OmniVoice won by ear).
  - `2026-11-test-omnivoice.md` — first OmniVoice test notes.
  - `2026-11-omnivoice-community-engines.md` — read-only survey of vLLM-Omni,
    audio.cpp and RealtimeTTS, written before any of them was run. Its speed
    figures are the authors' own; the measured ones are in `../tts-engine.md`.
  - `lab/latency/` — the latency protocol (fixed phrases, 15 s pauses) and raw
    results for every variant. `proto_vllm_omni.py PORT` is still the way to
    measure the server. Under `vllm-omni/` is the lab launch of the fastest
    configuration; it was promoted to `llm/vllm-omni/` (which is current), and
    its `start.sh` points at paths that no longer exist.
  - The hand-written Python server for OmniVoice (`llm/omnivoice`, with the
    FlashInfer option) was removed on 2026-11-18; last commit with it: `ad2466d`.
  - `lab/` — throwaway scripts, input texts and blind-test keys. Paths inside
    them point at the old locations (`llm/lab/...`, `docs/research/...`).
  - `samples/` — WAV files from the tests (git-ignored).
