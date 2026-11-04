FROM python:3.11-slim-bookworm@sha256:94a8fee35252d06ab75856f15f12c7bae67b10e8a6da93c7fded82b2610926cd
RUN apt-get update && apt-get install -y --no-install-recommends espeak-ng && rm -rf /var/lib/apt/lists/*
RUN pip install --no-cache-dir torch==2.5.1 --index-url https://download.pytorch.org/whl/cpu || pip install --no-cache-dir torch==2.5.1
ADD --checksum=sha256:fbf633262797f8cf46fdac3315cf9cade67dc8b762c0feccf334892772fb9ac4 https://files.pythonhosted.org/packages/e8/48/88b8cdf28b068d070195c2817175549dee48e7682e3ab8994bee5f69217e/kokoro-0.9.4.tar.gz /tmp/kokoro-0.9.4.tar.gz
RUN pip install --no-cache-dir /tmp/kokoro-0.9.4.tar.gz 'transformers>=4.46,<4.50' 'spacy>=3.8,<3.9' 'https://github.com/explosion/spacy-models/releases/download/en_core_web_sm-3.8.0/en_core_web_sm-3.8.0-py3-none-any.whl'
ADD https://huggingface.co/hexgrad/Kokoro-82M/resolve/f3ff3571791e39611d31c381e3a41a3af07b4987/config.json /models/config.json
ADD --checksum=sha256:496dba118d1a58f5f3db2efc88dbdc216e0483fc89fe6e47ee1f2c53f18ad1e4 https://huggingface.co/hexgrad/Kokoro-82M/resolve/f3ff3571791e39611d31c381e3a41a3af07b4987/kokoro-v1_0.pth /models/kokoro.pth
ADD --checksum=sha256:0ab5709b8ffab19bfd849cd11d98f75b60af7733253ad0d67b12382a102cb4ff https://huggingface.co/hexgrad/Kokoro-82M/resolve/f3ff3571791e39611d31c381e3a41a3af07b4987/voices/af_heart.pt /models/af_heart.pt
ENV HF_HUB_OFFLINE=1 OMP_NUM_THREADS=2
WORKDIR /work
ENTRYPOINT ["python","check/speak.py"]
