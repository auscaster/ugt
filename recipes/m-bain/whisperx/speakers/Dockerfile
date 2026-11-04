FROM python:3.11-slim-bookworm@sha256:94a8fee35252d06ab75856f15f12c7bae67b10e8a6da93c7fded82b2610926cd
RUN apt-get update && apt-get install -y --no-install-recommends ffmpeg espeak-ng libsndfile1 && rm -rf /var/lib/apt/lists/*
RUN pip install --no-cache-dir uv==0.13.7
ADD https://github.com/m-bain/whisperX.git#5d3678b8535698a2826a5db9eb3e4105aef85dd9 /src
# Same torchcodec platform exclusion as upstream's uv override; upstream passes decoded PCM to pyannote.
RUN printf '%s\n' "torchcodec>=0.6,<0.8; platform_machine == 'x86_64'" >/tmp/overrides.txt \
 && uv pip install --no-cache --system --torch-backend cpu --overrides /tmp/overrides.txt /src
ENV OMP_NUM_THREADS=2
WORKDIR /work
ENTRYPOINT ["python","check/transcribe.py"]
