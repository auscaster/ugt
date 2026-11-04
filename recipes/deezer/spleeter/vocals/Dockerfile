FROM python:3.11-slim-bookworm@sha256:94a8fee35252d06ab75856f15f12c7bae67b10e8a6da93c7fded82b2610926cd
RUN apt-get update && apt-get install -y --no-install-recommends ffmpeg espeak-ng libsndfile1 && rm -rf /var/lib/apt/lists/*
ADD https://github.com/deezer/spleeter.git#6f2c8a2e283914faa544631430f65b793f1b1218 /src
RUN pip install --no-cache-dir /src soundfile
ENV MODEL_PATH=/models OMP_NUM_THREADS=2 TF_NUM_INTRAOP_THREADS=2 TF_NUM_INTEROP_THREADS=1
WORKDIR /work
ENTRYPOINT ["/bin/sh"]
