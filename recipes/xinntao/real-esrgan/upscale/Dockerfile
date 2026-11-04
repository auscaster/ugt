FROM python:3.11-slim-bookworm@sha256:94a8fee35252d06ab75856f15f12c7bae67b10e8a6da93c7fded82b2610926cd
RUN apt-get update && apt-get install -y --no-install-recommends libgl1 libglib2.0-0 && rm -rf /var/lib/apt/lists/*
RUN pip install --no-cache-dir 'numpy<2' 'setuptools<81' wheel && \
    (pip install --no-cache-dir torch==2.1.2 torchvision==0.16.2 --index-url https://download.pytorch.org/whl/cpu || pip install --no-cache-dir torch==2.1.2 torchvision==0.16.2) && \
    pip install --no-cache-dir --no-build-isolation 'numpy<2' 'opencv-python<4.12' basicsr==1.4.2
ADD https://github.com/xinntao/Real-ESRGAN.git#fa4c8a03ae3dbc9ea6ed471a6ab5da94ac15c2ea /src
ENV OMP_NUM_THREADS=2 PYTHONPATH=/src/realesrgan
WORKDIR /work
ENTRYPOINT ["python","check/upscale.py"]
