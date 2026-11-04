FROM python:3.11-slim-bookworm@sha256:94a8fee35252d06ab75856f15f12c7bae67b10e8a6da93c7fded82b2610926cd
RUN apt-get update && apt-get install -y --no-install-recommends ffmpeg espeak-ng libsndfile1 && rm -rf /var/lib/apt/lists/*
ADD --checksum=sha256:e56d4832cfa001d937bac237349716b6e3579ca23862504a51c10c2393d9f3f0 https://files.pythonhosted.org/packages/20/15/1394f6c6e0fb3561b64dc147f354b12a228638e49e5f852e512b0662ed11/spleeter-2.4.2.tar.gz /tmp/spleeter-2.4.2.tar.gz
RUN pip install --no-cache-dir /tmp/spleeter-2.4.2.tar.gz soundfile
ENV MODEL_PATH=/models OMP_NUM_THREADS=2 TF_NUM_INTRAOP_THREADS=2 TF_NUM_INTEROP_THREADS=1
WORKDIR /work
ENTRYPOINT ["/bin/sh"]
