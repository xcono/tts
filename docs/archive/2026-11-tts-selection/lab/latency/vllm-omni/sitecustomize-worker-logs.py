# LAB ONLY (tts-lab-vllm): vllm-omni#8517 drops diffusion worker logs; mirror selected loggers to /logs/proc-<pid>.log
import logging, os
_PFX = ("vllm_omni.model_executor.models.omnivoice", "vllm_omni.diffusion.attention", "vllm_omni.platforms",
        "vllm_omni.diffusion.worker", "vllm_omni.diffusion.models.omnivoice")
_fh = None
def _file():
    global _fh
    if _fh is None:
        _fh = logging.FileHandler("/logs/proc-%d.log" % os.getpid())
        _fh.setFormatter(logging.Formatter("%(asctime)s %(levelname)s %(name)s: %(message)s"))
    return _fh
_orig_enabled = logging.Logger.isEnabledFor
def _enabled(self, level):
    if self.name.startswith(_PFX) and level >= logging.INFO:
        return True
    return _orig_enabled(self, level)
_orig_call = logging.Logger.callHandlers
def _call(self, record):
    if record.name.startswith(_PFX) and record.levelno >= logging.INFO:
        try: _file().handle(record)
        except Exception: pass
    return _orig_call(self, record)
logging.Logger.isEnabledFor = _enabled
logging.Logger.callHandlers = _call
