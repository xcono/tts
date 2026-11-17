# OmniVoice on top of the lab image; torch/torchaudio/transformers pinned to what's already there.
FROM xcono-tts-lab:tf5151
RUN pip freeze | grep -iE '^(torch|torchaudio|transformers)==' > /tmp/pins.txt && cat /tmp/pins.txt \
 && pip install --no-cache-dir -c /tmp/pins.txt omnivoice==0.2.1 \
 && pip freeze | grep -iE '^(torch|torchaudio|transformers)==' | diff - /tmp/pins.txt
