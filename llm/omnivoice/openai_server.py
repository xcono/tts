#!/usr/bin/env python3
"""OpenAI-compatible TTS server for OmniVoice (k2-fsa), voice-clone only.

Same wire contract as llm/qwen3-tts/openai_server.py, so the Go client needs no
change: POST /v1/audio/speech with response_format "pcm" streams raw 24 kHz mono
int16 LE; GET /health reports readiness.

OmniVoice is not autoregressive: it renders a whole utterance in a fixed number
of passes, so nothing can be decoded incrementally. To keep time-to-first-audio
low the server splits the input into sentences and streams each sentence's PCM
as soon as it is rendered (a sentence takes a fraction of a second on the GPU).

The reference voice is encoded once at startup into a clone prompt. ref_text is
always given: without it omnivoice downloads Whisper to transcribe the reference.
"""
import argparse
import asyncio
import io
import logging
import os
import queue
import re
import struct
import threading
import time
from typing import AsyncGenerator, Optional

import numpy as np
import torch
import uvicorn
from fastapi import FastAPI, HTTPException
from fastapi.responses import Response, StreamingResponse
from pydantic import BaseModel

logging.basicConfig(level=logging.INFO, format="%(asctime)s %(levelname)s %(message)s")
logger = logging.getLogger("omnivoice-server")

SAMPLE_RATE = 24000  # OmniVoice output rate == the Go module's tts.SampleRate

app = FastAPI(title="OmniVoice OpenAI-compatible TTS")

model = None
clone_prompt = None
cfg: argparse.Namespace
_model_lock = threading.Lock()  # one utterance at a time on the GPU

# Sentence boundary: terminal punctuation (with closing quotes/brackets) followed
# by whitespace. Short fragments are merged forward: the model is trained on
# sentences and one- or two-word clips come out unstable.
_SENT_END = re.compile(r"(?<=[.!?…])[\"»”)\]]*\s+")
_MIN_CHARS = 25


def split_sentences(text: str) -> list[str]:
    """Split text into synthesis units, merging fragments shorter than _MIN_CHARS."""
    parts = [p.strip() for p in _SENT_END.split(text.strip()) if p and p.strip()]
    units: list[str] = []
    pending = ""
    for p in parts:
        pending = f"{pending} {p}".strip() if pending else p
        if len(pending) >= _MIN_CHARS:
            units.append(pending)
            pending = ""
    if pending:
        if units:
            units[-1] = f"{units[-1]} {pending}"
        else:
            units.append(pending)
    # OmniVoice #204: Russian sometimes drops the last word without a final stop.
    return [u if u.endswith((".", "!", "?", "…")) else u + "." for u in units]


class SpeechRequest(BaseModel):
    model: str = "omnivoice"
    input: str
    voice: str = ""              # accepted for API compatibility; one cloned voice
    response_format: str = "pcm"  # pcm | wav
    speed: float = 1.0


def _to_pcm16(pcm: np.ndarray) -> bytes:
    return np.clip(pcm * 32768, -32768, 32767).astype(np.int16).tobytes()


def _wav_header(data_len: int) -> bytes:
    buf = io.BytesIO()
    buf.write(b"RIFF" + struct.pack("<I", 36 + data_len) + b"WAVEfmt ")
    buf.write(struct.pack("<IHHIIHH", 16, 1, 1, SAMPLE_RATE, SAMPLE_RATE * 2, 2, 16))
    buf.write(b"data" + struct.pack("<I", data_len))
    return buf.getvalue()


def _render(unit: str, speed: float) -> np.ndarray:
    """Render one synthesis unit to float32 mono PCM. Caller holds _model_lock."""
    with torch.inference_mode():
        wavs = model.generate(
            text=unit,
            language=cfg.language,
            voice_clone_prompt=clone_prompt,
            num_step=cfg.num_step,
            guidance_scale=cfg.guidance_scale,
            speed=speed if speed and speed != 1.0 else None,
        )
    return np.asarray(wavs[0], dtype=np.float32).squeeze()


async def _stream(text: str, speed: float) -> AsyncGenerator[bytes, None]:
    """Render sentence by sentence in a worker thread, yielding PCM as it lands."""
    q: queue.Queue = queue.Queue()
    done = object()
    cancelled = threading.Event()

    def producer() -> None:
        try:
            with _model_lock:
                for unit in split_sentences(text):
                    if cancelled.is_set():
                        break
                    t = time.monotonic()
                    pcm = _render(unit, speed)
                    logger.info("unit %d chars -> %.2fs audio in %.2fs",
                                len(unit), len(pcm) / SAMPLE_RATE, time.monotonic() - t)
                    q.put(_to_pcm16(pcm))
        except Exception as exc:  # surfaced to the client as a broken stream
            logger.exception("synthesis failed")
            q.put(exc)
        finally:
            q.put(done)

    threading.Thread(target=producer, daemon=True).start()
    loop = asyncio.get_event_loop()
    try:
        while True:
            item = await loop.run_in_executor(None, q.get)
            if item is done:
                break
            if isinstance(item, Exception):
                raise item
            yield item
    finally:
        cancelled.set()  # client went away: stop after the current sentence


@app.get("/health")
async def health():
    return {"status": "ok", "model_loaded": model is not None}


@app.post("/v1/audio/speech")
async def speech(req: SpeechRequest):
    if model is None:
        raise HTTPException(status_code=503, detail="Model not loaded")
    if not req.input.strip():
        raise HTTPException(status_code=400, detail="input is empty")
    fmt = req.response_format.lower()
    if fmt == "pcm":
        return StreamingResponse(_stream(req.input, req.speed), media_type="audio/pcm")
    if fmt == "wav":
        chunks = [c async for c in _stream(req.input, req.speed)]
        data = b"".join(chunks)
        return Response(_wav_header(len(data)) + data, media_type="audio/wav")
    raise HTTPException(status_code=400, detail="response_format must be 'pcm' or 'wav'")


def _load(args: argparse.Namespace) -> None:
    global model, clone_prompt
    from omnivoice import OmniVoice

    t = time.monotonic()
    m = OmniVoice.from_pretrained(args.model, device_map=args.device, dtype=torch.float16)
    logger.info("model %s loaded in %.1fs", args.model, time.monotonic() - t)
    if not args.ref_text.strip():
        raise SystemExit("OMNIVOICE_REF_TEXT is required (an empty transcript makes omnivoice download Whisper)")
    # OmniVoice #282: the prompt encoder allocates without inference_mode by default.
    with torch.inference_mode():
        prompt = m.create_voice_clone_prompt(ref_audio=args.ref_audio, ref_text=args.ref_text)
    model, clone_prompt = m, prompt
    # Warm up so the first real request does not pay first-call costs.
    t = time.monotonic()
    with _model_lock:
        _render("Проверка связи перед началом работы.", 1.0)
    logger.info("warmup done in %.1fs", time.monotonic() - t)


def _args() -> argparse.Namespace:
    e = os.environ.get
    p = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    p.add_argument("--model", default=e("OMNIVOICE_MODEL", "k2-fsa/OmniVoice"))
    p.add_argument("--ref-audio", default=e("OMNIVOICE_REF_AUDIO"), required=e("OMNIVOICE_REF_AUDIO") is None)
    p.add_argument("--ref-text", default=e("OMNIVOICE_REF_TEXT", ""))
    p.add_argument("--language", default=e("OMNIVOICE_LANGUAGE", "ru"))
    p.add_argument("--num-step", type=int, default=int(e("OMNIVOICE_NUM_STEP", "32")))
    p.add_argument("--guidance-scale", type=float, default=float(e("OMNIVOICE_GUIDANCE_SCALE", "2.0")))
    p.add_argument("--device", default=e("OMNIVOICE_DEVICE", "cuda:0"))
    p.add_argument("--host", default="0.0.0.0")
    p.add_argument("--port", type=int, default=8007)
    return p.parse_args()


def main() -> None:
    global cfg
    cfg = _args()
    _load(cfg)
    uvicorn.run(app, host=cfg.host, port=cfg.port, log_level="info")


if __name__ == "__main__":
    main()
