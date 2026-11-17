# vLLM-Omni serving OmniVoice (k2-fsa/OmniVoice): POST /v1/audio/speech.
#
# Build context is ./llm (see docker-compose.yaml) so the reference voice can be
# copied from qwen3-tts/ until it moves here.
#
# The image tag is PINNED. Both patches below are tied to v0.32.0 and are
# re-derived, not carried over, on any upgrade — `git apply` fails the build if
# the upstream source no longer matches, and the greps check the result.
#
#   01 — let vLLM's bundled FlashAttention 2 run on consumer Blackwell (sm_120,
#        RTX 50xx). Without it the server refuses --diffusion-attention-backend
#        FLASH_ATTN on this card. 0.70 → 0.55 s on the short test phrase.
#   02 — CUDA-graph capture alignment 128 → 32 tokens: less padding for single
#        requests. 0.55 → 0.48 s.
#
# Measurements and the protocol: docs/tts-engine.md.

FROM vllm/vllm-omni:v0.32.0

COPY vllm-omni/patches/ /tmp/patches/
RUN cd /usr/local/lib/python3.12/dist-packages/vllm_omni \
    && git apply --verbose /tmp/patches/01-flash-attn-sm120.patch \
                           /tmp/patches/02-lazy-graph-align32.patch \
    && grep -q "compute_capability.major == 12" platforms/cuda/platform.py \
    && grep -q "_LAZY_CAPTURE_ALIGNMENT = 32" model_executor/models/omnivoice/omnivoice_generator.py \
    && rm -rf /tmp/patches

COPY vllm-omni/omnivoice.yaml /cfg/omnivoice.yaml
# The cloned voice. Requests name it as ref_audio "file:///ref/ref_stas.wav".
COPY qwen3-tts/ref_stas.wav /ref/ref_stas.wav

ENV HF_HOME=/root/.cache/huggingface
EXPOSE 8007

CMD ["vllm", "serve", "k2-fsa/OmniVoice", "--omni", \
     "--host", "0.0.0.0", "--port", "8007", "--trust-remote-code", \
     "--deploy-config", "/cfg/omnivoice.yaml", \
     "--allowed-local-media-path", "/ref", \
     "--diffusion-attention-backend", "FLASH_ATTN"]
