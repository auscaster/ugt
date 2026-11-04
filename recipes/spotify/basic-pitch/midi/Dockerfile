FROM python:3.11-slim-bookworm@sha256:94a8fee35252d06ab75856f15f12c7bae67b10e8a6da93c7fded82b2610926cd
RUN apt-get update && apt-get install -y --no-install-recommends ffmpeg libsndfile1 && rm -rf /var/lib/apt/lists/*
ADD --checksum=sha256:6f48ac4b909c990fd59460622137a03b857c829bb1f3e65c71708da576ab68e5 https://files.pythonhosted.org/packages/d3/68/384dcaa2493497581ef7a0f18c76ce35afa26ea8a8d703f4761bdae632e6/basic_pitch-0.4.0.tar.gz /tmp/basic_pitch-0.4.0.tar.gz
RUN pip install --no-cache-dir 'numpy<2' /tmp/basic_pitch-0.4.0.tar.gz
ENV TF_NUM_INTRAOP_THREADS=2 TF_NUM_INTEROP_THREADS=1 OMP_NUM_THREADS=2
WORKDIR /work
ENTRYPOINT ["python","check/transcribe.py"]
