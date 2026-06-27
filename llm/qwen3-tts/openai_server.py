#!/usr/bin/env python3
"""
OpenAI-compatible TTS API server for faster-qwen3-tts (streaming voice clone).

Exposes POST /v1/audio/speech compatible with OpenAI's TTS API. Streams audio as
the model generates it (generate_voice_clone_streaming) for low time-to-first-
audio — the same fast path the sibling pipecat/voicekit stacks use.

MODEL: use the *0.6B*-Base voice-clone model. The 1.7B-Base model WEDGES this
RTX 5060 Ti (Blackwell sm_120): after 1-2 syntheses a GPU op hangs, spinning a
CPU core at 100% with the model lock held, never raising (verified on the rig).
The watchdog below is a safety net for such hangs, NOT a substitute for 0.6B.

Usage:
    python openai_server.py --ref-audio voice.wav --ref-text "transcript" \\
        --language Russian --port 8007
    python openai_server.py --voices voices.json    # multiple named voices

API:
    curl -s http://localhost:8007/v1/audio/speech \\
        -H "Content-Type: application/json" \\
        -d '{"input": "Привет!", "voice": "alloy", "response_format": "wav"}' \\
        --output speech.wav
"""
import argparse
import asyncio
import io
import json
import logging
import os
import queue
import struct
import sys
import threading
import time
from typing import AsyncGenerator, Optional

import numpy as np
import torch
import uvicorn
from fastapi import FastAPI, HTTPException
from fastapi.responses import Response, StreamingResponse
from pydantic import BaseModel

sys.path.insert(0, os.path.join(os.path.dirname(__file__), ".."))

logging.basicConfig(level=logging.INFO)
logger = logging.getLogger(__name__)

# ---------------------------------------------------------------------------
# Global state
# ---------------------------------------------------------------------------

app = FastAPI(title="faster-qwen3-tts OpenAI-compatible API")

tts_model = None
voices: dict = {}
default_voice: Optional[str] = None
SAMPLE_RATE = 24000  # updated once the model loads
_model_lock = threading.Lock()  # prevent concurrent GPU inference

# Max seconds a generation may make no progress before we treat the CUDA context
# as wedged. A sporadic CUDA fault can manifest as an unbounded CPU spin inside a
# GPU op rather than a raised exception, so _die_if_fatal never fires; the
# producer would then hold _model_lock forever and block every later request
# while /health still says OK. Streaming refreshes this deadline on every chunk,
# so it really bounds time-to-first-chunk and the gap between chunks.
WATCHDOG_TIMEOUT = float(os.environ.get("TTS_WATCHDOG_TIMEOUT", "120"))

# Independent watchdog state. A generation registers a deadline while it holds
# _model_lock, refreshes it per chunk, and clears it when done. A standalone
# thread (not tied to any client connection) trips if the deadline passes — so a
# wedge is caught even after the client disconnects and the request coroutine is
# cancelled, which would otherwise leave the producer thread spinning forever
# with the lock held.
_watchdog_lock = threading.Lock()
_watchdog_deadline: Optional[float] = None  # time.monotonic() value, or None when idle


def _wd_start() -> None:
    global _watchdog_deadline
    with _watchdog_lock:
        _watchdog_deadline = time.monotonic() + WATCHDOG_TIMEOUT


def _wd_progress() -> None:
    global _watchdog_deadline
    with _watchdog_lock:
        if _watchdog_deadline is not None:
            _watchdog_deadline = time.monotonic() + WATCHDOG_TIMEOUT


def _wd_stop() -> None:
    global _watchdog_deadline
    with _watchdog_lock:
        _watchdog_deadline = None


def _watchdog_loop() -> None:
    interval = max(0.05, min(5.0, WATCHDOG_TIMEOUT / 3.0))
    while True:
        time.sleep(interval)
        with _watchdog_lock:
            dl = _watchdog_deadline
        if dl is not None and time.monotonic() > dl:
            _die_wedged()


def _start_watchdog() -> None:
    if WATCHDOG_TIMEOUT > 0:
        threading.Thread(target=_watchdog_loop, name="tts-watchdog", daemon=True).start()
        logger.info("generation watchdog armed: %.0fs no-progress deadline", WATCHDOG_TIMEOUT)

# ---------------------------------------------------------------------------
# Request / response models
# ---------------------------------------------------------------------------


class SpeechRequest(BaseModel):
    model: str = "tts-1"
    input: str
    voice: str = "alloy"
    response_format: str = "wav"  # wav | pcm | mp3
    speed: float = 1.0           # accepted but not yet applied


# ---------------------------------------------------------------------------
# Audio helpers
# ---------------------------------------------------------------------------


def _to_pcm16(pcm: np.ndarray) -> bytes:
    """Convert float32 numpy array to raw 16-bit little-endian PCM bytes."""
    return np.clip(pcm * 32768, -32768, 32767).astype(np.int16).tobytes()


def _wav_header(sample_rate: int, data_len: int = 0xFFFFFFFF) -> bytes:
    """Build a WAV header.  Use data_len=0xFFFFFFFF for streaming (unknown size)."""
    n_channels = 1
    bits = 16
    byte_rate = sample_rate * n_channels * bits // 8
    block_align = n_channels * bits // 8
    riff_size = 0xFFFFFFFF if data_len == 0xFFFFFFFF else 36 + data_len
    buf = io.BytesIO()
    buf.write(b"RIFF")
    buf.write(struct.pack("<I", riff_size))
    buf.write(b"WAVE")
    buf.write(b"fmt ")
    buf.write(struct.pack("<IHHIIHH", 16, 1, n_channels, sample_rate,
                          byte_rate, block_align, bits))
    buf.write(b"data")
    buf.write(struct.pack("<I", data_len))
    return buf.getvalue()


def _to_mp3_bytes(pcm: np.ndarray, sample_rate: int) -> bytes:
    """Convert float32 numpy array to MP3 bytes (requires pydub + ffmpeg)."""
    try:
        from pydub import AudioSegment
    except ImportError:
        raise HTTPException(
            status_code=400,
            detail="response_format='mp3' requires pydub: pip install pydub",
        )
    segment = AudioSegment(
        _to_pcm16(pcm),
        frame_rate=sample_rate,
        sample_width=2,
        channels=1,
    )
    buf = io.BytesIO()
    segment.export(buf, format="mp3")
    return buf.getvalue()


# ---------------------------------------------------------------------------
# Fatal-error self-heal
# ---------------------------------------------------------------------------


def _is_fatal_cuda(exc: BaseException) -> bool:
    """True for an unrecoverable CUDA context fault (e.g. cudaErrorLaunchFailure).
    Such a fault makes every subsequent GPU op fail until the process restarts.
    OOM is excluded — it is usually recoverable."""
    s = str(exc)
    if "out of memory" in s.lower():
        return False
    acc_err = getattr(torch, "AcceleratorError", ())
    return isinstance(exc, acc_err) or "CUDA error" in s


def _die_if_fatal(exc: BaseException) -> None:
    """Exit so Docker (restart: unless-stopped) brings up a fresh CUDA context,
    instead of staying alive and failing every request with a dead context."""
    if _is_fatal_cuda(exc):
        logger.error("fatal CUDA fault — exiting for a clean restart: %s", exc)
        os._exit(1)


def _die_wedged() -> None:
    """Exit when a generation stalls past the watchdog deadline.

    A sporadic CUDA fault can wedge the context inside a GPU op, spinning a CPU
    core at 100% instead of raising. _die_if_fatal only catches *raised* faults,
    so the hang would otherwise hold _model_lock forever and block every later
    request while /health still says OK. Exiting hands recovery to Docker
    (restart: unless-stopped) — same self-heal as _die_if_fatal, for a hang."""
    logger.error(
        "TTS generation stalled >%.0fs (wedged CUDA context) — exiting for a clean restart",
        WATCHDOG_TIMEOUT,
    )
    os._exit(1)


# ---------------------------------------------------------------------------
# Voice resolution
# ---------------------------------------------------------------------------


def resolve_voice(voice_name: str) -> dict:
    """Return voice config dict or fall back to default, else raise 400."""
    if voice_name in voices:
        return voices[voice_name]
    if default_voice and default_voice in voices:
        logger.warning(
            "Voice %r not configured; falling back to default voice %r",
            voice_name,
            default_voice,
        )
        return voices[default_voice]
    raise HTTPException(
        status_code=400,
        detail=(
            f"Voice {voice_name!r} is not configured. "
            f"Available voices: {list(voices.keys())}"
        ),
    )


# ---------------------------------------------------------------------------
# Streaming helper: run sync generator in a background thread
# ---------------------------------------------------------------------------


async def _stream_chunks(voice_cfg: dict, text: str) -> AsyncGenerator[bytes, None]:
    """
    Run generate_voice_clone_streaming in a background thread and yield
    raw PCM bytes for each chunk as they arrive.
    """
    q: queue.Queue = queue.Queue()
    _DONE = object()

    def producer():
        try:
            with _model_lock:
                # Arm the watchdog only while we hold the lock and run the model —
                # time queued behind another request must not count. Refresh on
                # every chunk so it bounds prefill + inter-chunk gaps.
                _wd_start()
                try:
                    for chunk, _sr, _timing in tts_model.generate_voice_clone_streaming(
                        text=text,
                        language=voice_cfg.get("language", "Auto"),
                        ref_audio=voice_cfg["ref_audio"],
                        ref_text=voice_cfg.get("ref_text", ""),
                        chunk_size=voice_cfg.get("chunk_size", 12),
                        non_streaming_mode=False,
                    ):
                        _wd_progress()
                        q.put(chunk)
                finally:
                    _wd_stop()
        except Exception as exc:
            q.put(exc)
            _die_if_fatal(exc)
        finally:
            q.put(_DONE)

    thread = threading.Thread(target=producer, daemon=True)
    thread.start()

    # Plain blocking get: the producer runs (and the watchdog guards it)
    # independently of this consumer, so a client disconnect that cancels this
    # coroutine cannot strand a wedged producer.
    loop = asyncio.get_event_loop()
    while True:
        item = await loop.run_in_executor(None, q.get)
        if item is _DONE:
            break
        if isinstance(item, Exception):
            raise item
        yield _to_pcm16(item)


# ---------------------------------------------------------------------------
# Endpoints
# ---------------------------------------------------------------------------


@app.get("/health")
async def health():
    return {"status": "ok", "model_loaded": tts_model is not None}


@app.post("/v1/audio/speech")
async def create_speech(req: SpeechRequest):
    if tts_model is None:
        raise HTTPException(status_code=503, detail="Model not loaded")
    if not req.input.strip():
        raise HTTPException(status_code=400, detail="'input' text is empty")

    voice_cfg = resolve_voice(req.voice)
    fmt = req.response_format.lower()

    _CONTENT_TYPES = {
        "wav": "audio/wav",
        "pcm": "audio/pcm",
        "mp3": "audio/mpeg",
    }
    if fmt not in _CONTENT_TYPES:
        raise HTTPException(
            status_code=400,
            detail=f"response_format {fmt!r} not supported. Use: wav, pcm, mp3",
        )
    content_type = _CONTENT_TYPES[fmt]

    # --- MP3: generate all audio, then encode (non-streaming) ---
    if fmt == "mp3":
        loop = asyncio.get_event_loop()

        def _generate():
            with _model_lock:
                _wd_start()
                try:
                    return tts_model.generate_voice_clone(
                        text=req.input,
                        language=voice_cfg.get("language", "Auto"),
                        ref_audio=voice_cfg["ref_audio"],
                        ref_text=voice_cfg.get("ref_text", ""),
                    )
                finally:
                    _wd_stop()

        try:
            audio_arrays, sr = await loop.run_in_executor(None, _generate)
        except Exception as exc:
            _die_if_fatal(exc)
            raise
        audio = audio_arrays[0] if audio_arrays else np.zeros(1, dtype=np.float32)
        return Response(content=_to_mp3_bytes(audio, sr), media_type=content_type)

    # --- WAV / PCM: stream chunks as they are generated ---
    async def audio_stream():
        if fmt == "wav":
            yield _wav_header(SAMPLE_RATE)  # stream with unknown data length
        async for raw_chunk in _stream_chunks(voice_cfg, req.input):
            yield raw_chunk

    return StreamingResponse(audio_stream(), media_type=content_type)


# ---------------------------------------------------------------------------
# Entry point
# ---------------------------------------------------------------------------


def _warmup_model() -> None:
    """Capture the CUDA graphs at startup so the first real request is fast.

    faster-qwen3-tts captures its predictor/talker CUDA graphs lazily on the
    FIRST generation (model._warmup, gated by _warmed_up). Measured on the rig
    that first request costs ~3.3 s vs ~220 ms once warm — and graphs do NOT
    recapture per text length (the talker graph is position-independent), so a
    single warmup synthesis here makes every subsequent request fast, including
    the user's first utterance. We run the real streaming path (not _warmup
    directly) so this also validates the model end-to-end before serving.
    """
    if default_voice is None or default_voice not in voices:
        logger.warning("no default voice; skipping startup warmup")
        return
    cfg = voices[default_voice]
    t0 = time.monotonic()
    # Guard warmup with the watchdog: the wedge-prone 1.7B model can hang inside a
    # GPU op on the very first synthesis, and warmup runs before we serve — without
    # this it would hang the whole process with no self-heal. _start_watchdog must
    # be called before this (see main).
    _wd_start()
    try:
        for _chunk, _sr, _timing in tts_model.generate_voice_clone_streaming(
            text="Прогрев.",
            language=cfg.get("language", "Auto"),
            ref_audio=cfg["ref_audio"],
            ref_text=cfg.get("ref_text", ""),
            chunk_size=cfg.get("chunk_size", 12),
            non_streaming_mode=False,
        ):
            _wd_progress()
        logger.info("startup warmup complete (%.1fs) — CUDA graphs captured",
                    time.monotonic() - t0)
    except Exception as exc:  # noqa: BLE001 — warmup must not block startup
        logger.warning("startup warmup failed (continuing): %s", exc)
    finally:
        _wd_stop()


def _parse_args():
    p = argparse.ArgumentParser(
        description="OpenAI-compatible TTS server for faster-qwen3-tts",
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog=__doc__,
    )
    p.add_argument(
        "--model",
        default=os.environ.get("QWEN_TTS_MODEL", "Qwen/Qwen3-TTS-12Hz-0.6B-Base"),
        help="HuggingFace model ID or local path (default: the 0.6B-Base clone model)",
    )
    p.add_argument(
        "--voices",
        default=os.environ.get("QWEN_TTS_VOICES"),
        metavar="FILE",
        help="JSON file mapping voice names to {ref_audio, ref_text, language}",
    )
    p.add_argument(
        "--ref-audio",
        default=os.environ.get("QWEN_TTS_REF_AUDIO"),
        metavar="FILE",
        help="Reference audio file when --voices is not used",
    )
    p.add_argument(
        "--ref-text",
        default=os.environ.get("QWEN_TTS_REF_TEXT", ""),
        help="Transcript of --ref-audio (must match it for good cloning)",
    )
    p.add_argument(
        "--language",
        default=os.environ.get("QWEN_TTS_LANGUAGE", "Auto"),
        help="Target language (Russian, English, Auto, …) when --voices is not used",
    )
    p.add_argument("--host", default="0.0.0.0", help="Bind host (default: 0.0.0.0)")
    p.add_argument("--port", type=int, default=8000, help="Bind port (default: 8000)")
    p.add_argument("--device", default="cuda", help="Torch device (default: cuda)")
    return p.parse_args()


def main():
    global tts_model, voices, default_voice, SAMPLE_RATE

    args = _parse_args()

    # Build voice registry
    if args.voices:
        with open(args.voices) as f:
            voices = json.load(f)
        default_voice = next(iter(voices))
        logger.info("Loaded %d voice(s) from %s", len(voices), args.voices)
    elif args.ref_audio:
        voices = {
            "default": {
                "ref_audio": args.ref_audio,
                "ref_text": args.ref_text,
                "language": args.language,
            }
        }
        default_voice = "default"
        logger.info("Using single voice from --ref-audio: %s", args.ref_audio)
    else:
        print(
            "ERROR: provide --ref-audio <file> or --voices <config.json>",
            file=sys.stderr,
        )
        sys.exit(1)

    from faster_qwen3_tts import FasterQwen3TTS

    logger.info("Loading model %s on %s …", args.model, args.device)
    tts_model = FasterQwen3TTS.from_pretrained(
        args.model,
        device=args.device,
        dtype=torch.bfloat16,
    )
    SAMPLE_RATE = tts_model.sample_rate
    logger.info("Model ready. Sample rate: %d Hz", SAMPLE_RATE)

    _start_watchdog()  # arm before warmup so a wedge during warmup self-heals too
    _warmup_model()    # capture CUDA graphs now so the first request isn't ~3.3s

    logger.info("Server listening on http://%s:%d", args.host, args.port)
    uvicorn.run(app, host=args.host, port=args.port)


if __name__ == "__main__":
    main()
