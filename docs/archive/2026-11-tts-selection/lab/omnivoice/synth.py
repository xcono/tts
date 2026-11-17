#!/usr/bin/env python3
"""Synthesise OmniVoice listening samples (voice clone from a reference WAV).

ref_text is always passed: without it omnivoice downloads Whisper to transcribe
the reference. Seeded per take so a take can be reproduced.

  docker run --rm --gpus device=0 -v llms_hf-cache:/root/.cache/huggingface \
    -v $PWD:/lab:ro -v <samples-dir>:/out xcono-tts-lab:omnivoice \
    python3 /lab/synth.py --text-file /lab/text.txt --out /out/01-x [--takes 2] \
      [--ref /app/ref_stas.wav --ref-text "..."] [--num-step 32] [--guidance 2.0]
Writes <out>-<take>.wav (24 kHz mono int16), or <out>.wav when --takes 1.
"""
import argparse
import time

import numpy as np
import soundfile as sf
import torch
from omnivoice import OmniVoice

REF = "/app/ref_stas.wav"
REF_TEXT = ("Это нелегко. Ты думаешь, меня окружают прекрасные люди. Лучшей командой желать нельзя. "
            "Но всё равно что-то может пройти не так. Этот мир не прощает ошибок. Он жесток.")
SR = 24000

ap = argparse.ArgumentParser()
ap.add_argument("--text-file", required=True)
ap.add_argument("--out", required=True, help="output path without .wav")
ap.add_argument("--takes", type=int, default=2)
ap.add_argument("--ref", default=REF)
ap.add_argument("--ref-text", default=REF_TEXT)
ap.add_argument("--language", default="ru")
ap.add_argument("--num-step", type=int, default=32)
ap.add_argument("--guidance", type=float, default=2.0)
a = ap.parse_args()

text = open(a.text_file, encoding="utf-8").read().strip()
if not text.endswith((".", "!", "?")):
    text += "."  # #204: Russian sometimes drops the last word without a final stop
print("text:", text, flush=True)

t0 = time.monotonic()
m = OmniVoice.from_pretrained("k2-fsa/OmniVoice", device_map="cuda:0", dtype=torch.float16)
print(f"load {time.monotonic() - t0:.1f}s", flush=True)

for take in range(1, a.takes + 1):
    torch.manual_seed(1000 + take)
    t = time.monotonic()
    wavs = m.generate(text=text, language=a.language, ref_audio=a.ref, ref_text=a.ref_text,
                      num_step=a.num_step, guidance_scale=a.guidance)
    torch.cuda.synchronize()
    el = time.monotonic() - t
    w = np.asarray(wavs[0], dtype=np.float32).squeeze()
    out = f"{a.out}-{take}.wav" if a.takes > 1 else f"{a.out}.wav"
    sf.write(out, w, SR, subtype="PCM_16")
    dur = len(w) / SR
    print(f"{out}\taudio={dur:.2f}s\ttime={el:.2f}s\trtf={el / dur:.3f}", flush=True)
print("peak_alloc_GiB", round(torch.cuda.max_memory_allocated() / 2**30, 2), flush=True)
