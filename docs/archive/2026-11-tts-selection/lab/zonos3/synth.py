#!/usr/bin/env python3
"""Synthesise a text file with the ZONOS3-Mini ggml server (POST /v2/generate).

Stdlib only. Voice is cloned from a reference clip in embedding mode.
Example:
  python3 synth.py --text text-A.txt --ref ref_stas.wav --seed 1 -o out.wav \
      --extra '{"temperature": 0.5}'
"""
import argparse, base64, json, sys, time, urllib.error, urllib.request, wave


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--text", required=True, help="UTF-8 text file")
    ap.add_argument("--ref", required=True, help="reference audio file")
    ap.add_argument("-o", "--out", required=True)
    ap.add_argument("--seed", type=int, default=1)
    ap.add_argument("--language", default="ru")
    ap.add_argument("--url", default="http://localhost:1919/v2/generate")
    ap.add_argument("--extra", default="{}", help="JSON object merged into the request")
    a = ap.parse_args()

    text = open(a.text, encoding="utf-8").read().strip()
    with open(a.ref, "rb") as f:
        ref = base64.b64encode(f.read()).decode()
    req = {
        "text": text,
        "language": a.language,
        "stream": False,
        "format": "wav",
        "seed": a.seed,
        "speaker_audio_base64": ref,
        "speaker_mode": "embedding",
    }
    req.update(json.loads(a.extra))

    r = urllib.request.Request(a.url, data=json.dumps(req).encode(),
                               headers={"Content-Type": "application/json"})
    t0 = time.monotonic()
    try:
        with urllib.request.urlopen(r, timeout=600) as resp:
            body = resp.read()
    except urllib.error.HTTPError as e:
        print(f"HTTP {e.code}: {e.read().decode(errors='replace')[:500]}", file=sys.stderr)
        sys.exit(2)
    dt = time.monotonic() - t0
    with open(a.out, "wb") as f:
        f.write(body)
    with wave.open(a.out) as w:
        dur = w.getnframes() / w.getframerate()
    print(f"{a.out}: request {dt:.2f}s, audio {dur:.2f}s, RTF {dt / dur:.3f}")


if __name__ == "__main__":
    main()
