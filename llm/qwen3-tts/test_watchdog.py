"""Regression tests for the independent TTS watchdog (streaming path).

Run (no GPU needed — the model is faked):

    docker run --rm -e PYTHONPATH=/app -v "$PWD/llm/qwen3-tts:/app:ro" \\
        --entrypoint python3 voices-tts /app/test_watchdog.py

Bug: a sporadic CUDA fault wedges the model inside a GPU op (a CPU spin, not a
raised exception) while the producer thread holds _model_lock. When the Go client
gives up and disconnects, Starlette cancels the request coroutine — but the
producer thread keeps running the wedged generator with the lock held. The
watchdog must be independent of the client connection.
"""
import asyncio
import threading
import time

import numpy as np

import openai_server as srv

srv.WATCHDOG_TIMEOUT = 0.5

fired = threading.Event()
exit_code = {}


def fake_exit(code):
    exit_code["code"] = code
    fired.set()
    while True:  # park the watchdog thread; os._exit is noreturn in production
        time.sleep(3600)


srv.os._exit = fake_exit
srv._start_watchdog()

voice = {"ref_audio": "x", "ref_text": "", "language": "Russian"}


class WedgedModel:
    def generate_voice_clone_streaming(self, **kw):
        time.sleep(3)  # wedged: never yields a chunk before the deadline (0.5s)
        if False:
            yield


class FastModel:
    def generate_voice_clone_streaming(self, **kw):
        for _ in range(3):
            time.sleep(0.05)
            yield np.zeros(10, dtype=np.float32), 24000, {}


def test_no_false_trip():
    srv.tts_model = FastModel()

    async def run():
        return [c async for c in srv._stream_chunks(voice, "привет")]

    chunks = asyncio.run(run())
    assert len(chunks) == 3, f"expected 3 chunks, got {len(chunks)}"
    assert not fired.is_set(), "watchdog falsely tripped on healthy streaming"
    assert srv._watchdog_deadline is None, "deadline not cleared after completion"
    print("PASS: healthy streaming, no false watchdog trip")


def test_fires_after_disconnect():
    srv.tts_model = WedgedModel()

    async def run():
        gen = srv._stream_chunks(voice, "привет")
        task = asyncio.ensure_future(gen.__anext__())
        await asyncio.sleep(0.2)  # let the producer acquire the lock + arm watchdog
        task.cancel()             # client disconnect → coroutine cancelled
        try:
            await task
        except BaseException:
            pass
        await gen.aclose()        # drop the consumer entirely

    # Manual loop: a faked os._exit leaves the wedged producer's executor thread
    # alive, and asyncio.run()'s graceful shutdown would join it. close() does not.
    loop = asyncio.new_event_loop()
    try:
        loop.run_until_complete(run())
    finally:
        loop.close()
    assert fired.wait(timeout=10), "watchdog did NOT fire after client disconnect"
    print(f"PASS: watchdog fired after disconnect, exit code {exit_code.get('code')}")


test_no_false_trip()
test_fires_after_disconnect()
print("ALL TESTS PASSED")
