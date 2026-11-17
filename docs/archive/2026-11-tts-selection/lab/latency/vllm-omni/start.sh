#!/bin/bash
# Start vLLM-Omni v0.32.0 + OmniVoice on GPU 1 (RTX 5060 Ti, sm_120), best config: bf16 + FLASH_ATTN (FA2 via platform-gate patch).
# Usage: ./start.sh            -> bf16 + FLASH_ATTN                      (0.546 / 0.661 s)
#        ./start.sh align32    -> + lazy CUDA-graph alignment 32 patch    (0.484 / 0.663 s)
#        LOGS=1 ./start.sh     -> also mirror worker logs to ./logs/proc-<pid>.log (vllm-omni#8517 workaround)
set -e
D=$(cd "$(dirname "$0")" && pwd)
REF=$(cd "$D/../../../../../.." && pwd)/llm/qwen3-tts/ref_stas.wav
EXTRA=()
if [ "$1" = align32 ]; then
  EXTRA+=(-v "$D/omnivoice_generator-align32.py:/usr/local/lib/python3.12/dist-packages/vllm_omni/model_executor/models/omnivoice/omnivoice_generator.py:ro")
fi
if [ -n "$LOGS" ]; then
  mkdir -p "$D/logs"; chmod 777 "$D/logs"
  EXTRA+=(-v "$D/logs:/logs" -v "$D/sitecustomize-worker-logs.py:/usr/lib/python3.12/sitecustomize.py:ro")
fi
docker rm -f tts-lab-vllm >/dev/null 2>&1 || true
docker run -d --name tts-lab-vllm --gpus device=1 --ipc=host -p 127.0.0.1:8019:8019 \
  -v llms_hf-cache:/root/.cache/huggingface:ro \
  -v "$D/omnivoice-bf16.yaml:/cfg/omnivoice.yaml:ro" \
  -v "$D/platform.py:/usr/local/lib/python3.12/dist-packages/vllm_omni/platforms/cuda/platform.py:ro" \
  -v "$REF:/ref/ref_stas.wav:ro" \
  -e HF_HUB_OFFLINE=1 -e TRANSFORMERS_OFFLINE=1 \
  "${EXTRA[@]}" \
  vllm/vllm-omni:v0.32.0 \
  vllm serve k2-fsa/OmniVoice --omni --host 0.0.0.0 --port 8019 --trust-remote-code \
    --deploy-config /cfg/omnivoice.yaml --allowed-local-media-path /ref \
    --diffusion-attention-backend FLASH_ATTN
timeout 400 bash -c 'until docker logs tts-lab-vllm 2>&1 | grep -qE "Application startup complete|Traceback"; do sleep 3; done'
docker inspect -f 'tts-lab-vllm: {{.State.Status}}' tts-lab-vllm
