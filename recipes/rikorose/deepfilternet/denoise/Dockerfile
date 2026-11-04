FROM python:3.11-slim-bookworm@sha256:94a8fee35252d06ab75856f15f12c7bae67b10e8a6da93c7fded82b2610926cd
RUN apt-get update && apt-get install -y --no-install-recommends ffmpeg espeak-ng && rm -rf /var/lib/apt/lists/*
WORKDIR /work
ENTRYPOINT ["sh","-ec"]
