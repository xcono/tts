#!/usr/bin/env python3
"""Synthesise listening samples with the stock qwen_tts path (no CUDA graphs).

Why native, not the faster-qwen3-tts server: on this stack the faster path ends
~50% of short utterances after 2-5 codec frames (see diag_eos.py); stock qwen_tts
gave 0/24 such cuts. Sampling = the checkpoint's generation_config (T=0.9,
top_k=50, rep. penalty 1.05); seeded so a take can be reproduced.

  docker run --rm --gpus device=0 -v llms_hf-cache:/root/.cache/huggingface \
    -v $PWD:/lab:ro -v <samples-dir>:/out xcono-tts-lab:tf5151 \
    python3 /lab/synth_native.py <model> <prefix> <text-file>:<name> ...
Writes /out/<prefix><name>-<take>.wav (24 kHz mono int16), two takes each.
"""
import sys
import time

import numpy as np
import soundfile as sf
import torch
from qwen_tts import Qwen3TTSModel

REF = "/app/ref_stas.wav"
REF_TEXT = ("Это нелегко. Ты думаешь, меня окружают прекрасные люди. Лучшей командой желать нельзя. "
            "Но всё равно что-то может пройти не так. Этот мир не прощает ошибок. Он жесток.")
TAKES = 2

model_id, prefix, jobs = sys.argv[1], sys.argv[2], sys.argv[3:]
t0 = time.monotonic()
m = Qwen3TTSModel.from_pretrained(model_id, device_map="cuda:0", dtype=torch.bfloat16)
print(f"load {time.monotonic() - t0:.1f}s", flush=True)

for job in jobs:
    path, name = job.split(":")
    text = open(path, encoding="utf-8").read().strip()
    for take in range(1, TAKES + 1):
        torch.manual_seed(1000 + take)
        t = time.monotonic()
        wavs, sr = m.generate_voice_clone(text=text, language="Russian", ref_audio=REF, ref_text=REF_TEXT)
        el = time.monotonic() - t
        w = np.asarray(wavs[0], dtype=np.float32)
        out = f"/out/{prefix}{name}-{take}.wav"
        sf.write(out, w, sr, subtype="PCM_16")
        dur = len(w) / sr
        print(f"{out}\tsr={sr}\taudio={dur:.2f}s\ttime={el:.2f}s\trtf={el / dur:.3f}", flush=True)
print("peak_alloc_GiB", round(torch.cuda.max_memory_allocated() / 2**30, 2), flush=True)
