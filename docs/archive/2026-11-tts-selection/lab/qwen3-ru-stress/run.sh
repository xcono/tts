#!/usr/bin/env bash
# Start the lab TTS container (no restart policy, so a wedge/watchdog exit stays visible).
#   ./run.sh [model] [gpu] [port] [name]
# Image: docker build --no-cache --pull -t xcono-tts-lab ../../qwen3-tts && docker build -t xcono-tts-lab:tf5151 .
set -euo pipefail
MODEL=${1:-siriusfreak/qwen3-tts-12hz-1.7b-ru-stress-cf}
GPU=${2:-0}
PORT=${3:-8017}
NAME=${4:-tts-lab}
IMAGE=${IMAGE:-xcono-tts-lab:tf5151}
docker run -d --name "$NAME" \
  --gpus "device=$GPU" \
  -p "$PORT:8007" \
  -v llms_hf-cache:/root/.cache/huggingface \
  -e QWEN_TTS_MODEL="$MODEL" \
  -e TTS_WATCHDOG_TIMEOUT=120 \
  "$IMAGE"
