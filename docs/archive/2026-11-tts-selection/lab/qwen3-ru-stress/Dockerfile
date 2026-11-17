# Lab image: the fresh xcono-tts-lab build (llm/qwen3-tts/Dockerfile, built
# 2026-11-17 with --no-cache --pull) + transformers pinned back to 5.15.1.
# Why: pip resolved transformers 5.20.2, where MimiConfig no longer has
# `rope_theta`; qwen_tts's 5.x shim (_transformers_compat._default_rope_parameters)
# reads config.rope_theta and model load dies with AttributeError.
# 5.15.1 is the lower bound declared by qwen-tts-hf / faster-qwen3-tts.
FROM xcono-tts-lab
RUN python3 -m pip install --no-cache-dir "transformers==5.15.1"
