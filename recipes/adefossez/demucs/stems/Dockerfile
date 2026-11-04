FROM python:3.11-slim-bookworm@sha256:94a8fee35252d06ab75856f15f12c7bae67b10e8a6da93c7fded82b2610926cd
RUN apt-get update && apt-get install -y --no-install-recommends ffmpeg espeak-ng && rm -rf /var/lib/apt/lists/*
RUN pip install --no-cache-dir torch==2.5.1 --index-url https://download.pytorch.org/whl/cpu || pip install --no-cache-dir torch==2.5.1
ADD --checksum=sha256:870e8ebc9b3fb660d7c55aa27ca786bca3aa2da0576c09e36b7a0ebc27b03a11 https://codeload.github.com/adefossez/demucs/tar.gz/829d21890023800713e5dd231c67e43ecc06069b /tmp/demucs.tar.gz
RUN pip install --no-cache-dir /tmp/demucs.tar.gz soundfile
ENV OMP_NUM_THREADS=2
WORKDIR /work
ENTRYPOINT ["python","check/separate.py"]
