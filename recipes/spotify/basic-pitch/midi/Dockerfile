FROM python:3.11-slim-bookworm@sha256:94a8fee35252d06ab75856f15f12c7bae67b10e8a6da93c7fded82b2610926cd
RUN apt-get update && apt-get install -y --no-install-recommends ffmpeg libsndfile1 && rm -rf /var/lib/apt/lists/*
ADD https://github.com/spotify/basic-pitch.git#9991303bba609a3b93089d13ec80d1d495083596 /src
RUN pip install --no-cache-dir 'numpy<2' /src
ENV TF_NUM_INTRAOP_THREADS=2 TF_NUM_INTEROP_THREADS=1 OMP_NUM_THREADS=2
WORKDIR /work
ENTRYPOINT ["python","check/transcribe.py"]
