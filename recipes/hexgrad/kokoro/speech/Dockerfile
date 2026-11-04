FROM python:3.11-slim-bookworm@sha256:94a8fee35252d06ab75856f15f12c7bae67b10e8a6da93c7fded82b2610926cd
RUN apt-get update && apt-get install -y --no-install-recommends espeak-ng && rm -rf /var/lib/apt/lists/*
RUN pip install --no-cache-dir torch==2.5.1 --index-url https://download.pytorch.org/whl/cpu || pip install --no-cache-dir torch==2.5.1
ADD https://github.com/hexgrad/kokoro.git#dfb907a02bba8152ca444717ca5d78747ccb4bec /src
RUN pip install --no-cache-dir /src 'transformers>=4.46,<4.50' 'spacy>=3.8,<3.9' 'https://github.com/explosion/spacy-models/releases/download/en_core_web_sm-3.8.0/en_core_web_sm-3.8.0-py3-none-any.whl'
ADD https://huggingface.co/hexgrad/Kokoro-82M/resolve/f3ff3571791e39611d31c381e3a41a3af07b4987/config.json /models/config.json
ADD --checksum=sha256:496dba118d1a58f5f3db2efc88dbdc216e0483fc89fe6e47ee1f2c53f18ad1e4 https://huggingface.co/hexgrad/Kokoro-82M/resolve/f3ff3571791e39611d31c381e3a41a3af07b4987/kokoro-v1_0.pth /models/kokoro.pth
ADD --checksum=sha256:0ab5709b8ffab19bfd849cd11d98f75b60af7733253ad0d67b12382a102cb4ff https://huggingface.co/hexgrad/Kokoro-82M/resolve/f3ff3571791e39611d31c381e3a41a3af07b4987/voices/af_heart.pt /models/af_heart.pt
ENV HF_HUB_OFFLINE=1 OMP_NUM_THREADS=2
WORKDIR /work
ENTRYPOINT ["python","check/speak.py"]
