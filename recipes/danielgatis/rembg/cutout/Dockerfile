FROM python:3.11-slim-bookworm@sha256:94a8fee35252d06ab75856f15f12c7bae67b10e8a6da93c7fded82b2610926cd
ADD https://github.com/danielgatis/rembg.git#c5b48b0417a0254df7d461efa14573597f369a83 /src
ENV REMBG_HOME=/models NUMBA_CACHE_DIR=/tmp/numba OMP_NUM_THREADS=2
RUN POETRY_DYNAMIC_VERSIONING_BYPASS=2.0.86 pip install --no-cache-dir '/src[cpu]'
ADD --checksum=sha256:8d10d2f3bb75ae3b6d527c77944fc5e7dcd94b29809d47a739a7a728a912b491 https://github.com/danielgatis/rembg/releases/download/v0.0.0/u2net.onnx /models/models/u2net/u2net.onnx
WORKDIR /work
ENTRYPOINT ["python", "check/cutout.py"]
