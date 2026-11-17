#!/usr/bin/env python3
"""Early-EOS diagnostic: how often does a synthesis come back near-empty?

Runs the same short texts N times through
  native   — stock qwen_tts Qwen3TTSModel.generate_voice_clone (no CUDA graphs)
  fast     — faster_qwen3_tts generate_voice_clone_streaming (CUDA graphs, as the server)
  fastns   — same, non_streaming_mode=True (whole text prefilled)
and prints audio durations. Run inside the lab image with the server stopped:
  docker run --rm --gpus device=0 -v llms_hf-cache:/root/.cache/huggingface \
    -v $PWD:/lab xcono-tts-lab:tf5151 python3 /lab/diag_eos.py <model> <mode> [N]
"""
import sys
import time

import numpy as np
import torch

MODEL = sys.argv[1]
MODE = sys.argv[2]
N = int(sys.argv[3]) if len(sys.argv) > 3 else 8
REF = "/app/ref_stas.wav"
REF_TEXT = ("Это нелегко. Ты думаешь, меня окружают прекрасные люди. Лучшей командой желать нельзя. "
            "Но всё равно что-то может пройти не так. Этот мир не прощает ошибок. Он жесток.")
TEXTS = [
    "Сборка прошла, тесты зелёные.",
    "Я задеплоил новый релиз, и сразу посыпались пятисотки.",
    "Откатил деплой, поднял реплику и пока зафиксил хотфиксом.",
]

if MODE == "native":
    from qwen_tts import Qwen3TTSModel
    m = Qwen3TTSModel.from_pretrained(MODEL, device_map="cuda:0", dtype=torch.bfloat16)

    def run(t):
        w, sr = m.generate_voice_clone(text=t, language="Russian", ref_audio=REF, ref_text=REF_TEXT)
        return len(w[0]) / sr
else:
    from faster_qwen3_tts import FasterQwen3TTS
    m = FasterQwen3TTS.from_pretrained(MODEL, device="cuda", dtype=torch.bfloat16)

    def run(t):
        n = 0
        for c, sr, _ in m.generate_voice_clone_streaming(text=t, language="Russian", ref_audio=REF,
                                                          ref_text=REF_TEXT, chunk_size=12,
                                                          non_streaming_mode=(MODE == "fastns")):
            n += len(c)
        return n / 24000

for t in TEXTS:
    durs = []
    t0 = time.monotonic()
    for _ in range(N):
        durs.append(run(t))
    short = sum(d < 0.5 for d in durs)
    print(f"{MODE}\t{len(t)}ch\tempty={short}/{N}\tdurs={np.round(durs, 2).tolist()}\t"
          f"{(time.monotonic() - t0) / N:.2f}s/run", flush=True)
print("peak_alloc_GiB", round(torch.cuda.max_memory_allocated() / 2**30, 2))
