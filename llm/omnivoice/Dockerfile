# OmniVoice (k2-fsa) OpenAI-compatible TTS server (POST /v1/audio/speech).
#
# Build context is ./llm (see docker-compose.yaml) so the reference voice shared
# with the Qwen3 server can be copied from qwen3-tts/.
#
# CUDA 12.8 base + cu128 torch wheels — required for Blackwell GPUs (RTX 50xx,
# sm_120). Every Python dependency that touches the model is PINNED to the set
# verified on the rig (2026-11-17): an unpinned rebuild of the Qwen3 image pulled
# a transformers release that could no longer load its model.
#
# PYTORCH_CUDA_ALLOC_CONF: request lengths differ every call, so cached allocator
# blocks rarely fit the next request and reserved VRAM creeps up (OmniVoice #199,
# reported on this exact card). Expandable segments keep it flat.

FROM nvidia/cuda:12.8.1-cudnn-runtime-ubuntu22.04

ENV DEBIAN_FRONTEND=noninteractive PYTHONUNBUFFERED=1
RUN apt-get update && apt-get install -y --no-install-recommends \
        python3 python3-pip ffmpeg curl ca-certificates \
    && rm -rf /var/lib/apt/lists/*

RUN python3 -m pip install --no-cache-dir --upgrade pip \
    && python3 -m pip install --no-cache-dir torch==2.11.0 torchaudio==2.11.0 \
        --index-url https://download.pytorch.org/whl/cu128 \
    && python3 -m pip install --no-cache-dir omnivoice==0.2.1 transformers==5.15.1 \
        fastapi uvicorn

WORKDIR /app
COPY omnivoice/openai_server.py /app/openai_server.py
COPY qwen3-tts/ref_stas.wav /app/ref_stas.wav

ENV HF_HOME=/root/.cache/huggingface \
    PYTORCH_CUDA_ALLOC_CONF=expandable_segments:True \
    OMNIVOICE_MODEL=k2-fsa/OmniVoice \
    OMNIVOICE_REF_AUDIO=/app/ref_stas.wav \
    OMNIVOICE_REF_TEXT="Это нелегко. Ты думаешь, меня окружают прекрасные люди. Лучшей командой желать нельзя. Но всё равно что-то может пройти не так. Этот мир не прощает ошибок. Он жесток." \
    OMNIVOICE_LANGUAGE=ru
EXPOSE 8007

CMD ["python3", "/app/openai_server.py", "--host", "0.0.0.0", "--port", "8007"]
