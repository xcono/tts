# Build/run image for the ZONOS3-Mini ggml server (CUDA, Blackwell sm_120). Source +
# build live on a mounted volume (~/.cache/tts-lab/zonos3/src), not in the image.
FROM nvidia/cuda:12.8.1-devel-ubuntu22.04
ENV DEBIAN_FRONTEND=noninteractive
RUN apt-get update && apt-get install -y --no-install-recommends \
      build-essential cmake git curl ca-certificates ffmpeg python3 pkg-config \
    && rm -rf /var/lib/apt/lists/*
ENV RUSTUP_HOME=/opt/rustup CARGO_HOME=/opt/cargo PATH=/opt/cargo/bin:$PATH
RUN curl -sSf https://sh.rustup.rs | sh -s -- -y --profile minimal --default-toolchain stable \
    && chmod -R a+rwX /opt/cargo /opt/rustup
WORKDIR /src/servers/ggml
