# Official qwen-tts OpenAI-compatible TTS server (POST /v1/audio/speech).
#
# CUDA 12.8 base — required for Blackwell GPUs (RTX 50xx, incl. the 5060 Ti).
# Ubuntu 22.04's default python3 is 3.10, which satisfies qwen-tts (>=3.10), so we
# use the distro python rather than a deadsnakes PPA.
#
# MODEL: the *0.6B*-Base voice-clone model is the safe baked default — same as the
# sibling pipecat/voicekit stacks, which run reliably. The 1.7B-Base model WEDGES
# on this RTX 5060 Ti (Blackwell sm_120): after 1-2 syntheses a GPU op hangs,
# spinning a CPU core at 100% with the model lock held, never raising. Verified by
# A/B on the rig (1.7B: hangs in 1-2 reqs; 0.6B: 20/20 clean). The watchdog guards
# startup warmup AND every request, so a wedge self-heals via a (visible) Docker
# restart. Override the model at runtime via QWEN_TTS_MODEL (docker-compose / .env).
#
# We use `faster-qwen3-tts` (andimarafioti) — manual CUDA graph capture + streaming
# (generate_voice_clone_streaming) for low time-to-first-audio, matching pipecat/
# voicekit. The port was never the wedge cause (it works at 0.6B); the 1.7B model
# is. The pip console script is an interactive REPL, not an HTTP server, so we run
# our own vendored `openai_server.py` on its voice-clone path: voice-clone only,
# needs a reference WAV + transcript.
#   The baked reference is ref_stas.wav — a real ~12s human voice recording,
#   mono/24kHz; its transcript (produced via OpenAI Whisper) is QWEN_TTS_REF_TEXT
#   below. ref_text must match the audio for good cloning.
#   `openai_server.py` is vendored (copied) so the build has no runtime dependency
#   on raw.githubusercontent.com.

FROM nvidia/cuda:12.8.1-cudnn-runtime-ubuntu22.04

ENV DEBIAN_FRONTEND=noninteractive PYTHONUNBUFFERED=1
RUN apt-get update && apt-get install -y --no-install-recommends \
        python3 python3-pip ffmpeg curl ca-certificates \
    && rm -rf /var/lib/apt/lists/*

# cu128 torch wheels (torch 2.7+ ship cu128; satisfies faster-qwen3-tts's torch>=2.5.1
# and is required for sm_120 / Blackwell). [demo] extra pulls fastapi + uvicorn,
# which openai_server.py needs.
RUN python3 -m pip install --no-cache-dir --upgrade pip \
    && python3 -m pip install --no-cache-dir torch torchaudio --index-url https://download.pytorch.org/whl/cu128 \
    && python3 -m pip install --no-cache-dir "faster-qwen3-tts[demo]"

WORKDIR /app
COPY openai_server.py /app/openai_server.py
COPY ref_stas.wav /app/ref_stas.wav

ENV HF_HOME=/root/.cache/huggingface \
    QWEN_TTS_MODEL=Qwen/Qwen3-TTS-12Hz-0.6B-Base \
    QWEN_TTS_REF_AUDIO=/app/ref_stas.wav \
    QWEN_TTS_REF_TEXT="Это нелегко. Ты думаешь, меня окружают прекрасные люди. Лучшей командой желать нельзя. Но всё равно что-то может пройти не так. Этот мир не прощает ошибок. Он жесток." \
    QWEN_TTS_LANGUAGE=Russian
EXPOSE 8007

# Serve the OpenAI-compatible /v1/audio/speech endpoint on 0.0.0.0:8007.
# Model + ref audio + ref text + language all come from the QWEN_TTS_* env vars above
# (openai_server.py reads them as the defaults for --model/--ref-audio/--ref-text/--language).
CMD ["python3", "/app/openai_server.py", "--host", "0.0.0.0", "--port", "8007"]
