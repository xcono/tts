#!/usr/bin/env python3
"""Stream /v1/audio/speech (pcm) and record time, TTFA, RTF and peak VRAM.

Usage:
  bench.py --url http://localhost:8017 --gpu 0 --runs 24 --log stability.tsv
      runs a fixed set of texts of varied length in a loop (stability test)
  bench.py --url ... --gpu 0 --text-file text-B.txt --out B-stress-latin-1.wav
      synthesises one text to a 24 kHz mono WAV
A request that sends nothing for --timeout seconds counts as a hang.
"""
import argparse
import subprocess
import sys
import threading
import time
import wave

import requests

SR = 24000

TEXTS = [
    "Привет.",
    "Сборка прошла, тесты зелёные.",
    "Я задеплоил новый релиз, и сразу посыпались пятисотки.",
    "Откатил деплой, поднял реплику и пока зафиксил хотфиксом.",
    "Сначала грешил на race condition в воркере, но по трейсам видно — это connection pool в Postgres исчерпался.",
    "Так, коллеги, у нас на проде лежит чекаут. Я задеплоил новый релиз, и сразу посыпались пятисотки. "
    "Сначала грешил на race condition в воркере, но по трейсам видно — это connection pool в Postgres исчерпался, "
    "потому что кто-то забыл закрыть транзакцию. Откатил деплой, поднял реплику и пока зафиксил хотфиксом. "
    "Сижу, греплю логи в Kibana и пью третий эспрессо.",
    "Миграция базы данных заняла сорок минут, потому что индекс пересобирался на каждой таблице, "
    "а репликация отставала почти на час.",
    "Ок.",
]


class VramPeak(threading.Thread):
    def __init__(self, gpu: int):
        super().__init__(daemon=True)
        self.gpu, self.peak, self.stop = gpu, 0, False

    def run(self):
        while not self.stop:
            try:
                out = subprocess.run(
                    ["nvidia-smi", "-i", str(self.gpu), "--query-gpu=memory.used",
                     "--format=csv,noheader,nounits"], capture_output=True, text=True, timeout=5)
                self.peak = max(self.peak, int(out.stdout.strip()))
            except Exception:
                pass
            time.sleep(0.2)


def synth(url: str, text: str, timeout: float):
    t0 = time.monotonic()
    ttfa = None
    buf = bytearray()
    with requests.post(f"{url}/v1/audio/speech",
                       json={"input": text, "response_format": "pcm"},
                       stream=True, timeout=(10, timeout)) as r:
        r.raise_for_status()
        for chunk in r.iter_content(chunk_size=4096):
            if chunk:
                if ttfa is None:
                    ttfa = time.monotonic() - t0
                buf += chunk
    total = time.monotonic() - t0
    if len(buf) % 2:
        buf = buf[:-1]
    dur = len(buf) / 2 / SR
    return bytes(buf), total, ttfa or total, dur


def write_wav(path: str, pcm: bytes):
    with wave.open(path, "wb") as w:
        w.setnchannels(1)
        w.setsampwidth(2)
        w.setframerate(SR)
        w.writeframes(pcm)


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--url", default="http://localhost:8017")
    ap.add_argument("--gpu", type=int, default=0)
    ap.add_argument("--runs", type=int, default=24)
    ap.add_argument("--timeout", type=float, default=180)
    ap.add_argument("--log", default=None)
    ap.add_argument("--text-file")
    ap.add_argument("--out")
    a = ap.parse_args()

    vp = VramPeak(a.gpu)
    vp.start()
    if a.text_file:
        text = open(a.text_file, encoding="utf-8").read().strip()
        pcm, total, ttfa, dur = synth(a.url, text, a.timeout)
        write_wav(a.out, pcm)
        print(f"{a.out}\ttotal={total:.2f}s\tttfa={ttfa:.2f}s\taudio={dur:.2f}s\t"
              f"rtf={total / max(dur, 1e-6):.3f}\tvram_peak={vp.peak}MiB", flush=True)
        return

    log = open(a.log, "w") if a.log else sys.stdout
    print("run\tchars\ttotal_s\tttfa_s\taudio_s\trtf\tvram_peak_mib\tstatus", file=log, flush=True)
    fails = 0
    for i in range(a.runs):
        text = TEXTS[i % len(TEXTS)]
        try:
            _, total, ttfa, dur = synth(a.url, text, a.timeout)
            status = "ok"
        except Exception as exc:  # timeout = hang, or server died
            total = ttfa = dur = 0.0
            status = f"FAIL:{type(exc).__name__}:{str(exc)[:80]}"
            fails += 1
        rtf = total / dur if dur else 0.0
        print(f"{i + 1}\t{len(text)}\t{total:.2f}\t{ttfa:.2f}\t{dur:.2f}\t{rtf:.3f}\t{vp.peak}\t{status}",
              file=log, flush=True)
        if fails >= 2:
            break
    vp.stop = True
    sys.exit(1 if fails else 0)


if __name__ == "__main__":
    main()
