FROM python:3.13-slim-bookworm
RUN apt-get update && apt-get install -y --no-install-recommends libgl1 libglib2.0-0 libxrender1 libxext6 libsm6 \
 && rm -rf /var/lib/apt/lists/*
# the v0.8.2 release wheel, checked against the SHA-256 PyPI publishes
ADD --checksum=sha256:f81111a46b5788e0dbcab3835433a37723ed508f55f5c3ba812e6c3e21e390f2 \
    https://files.pythonhosted.org/packages/a3/ea/238bef28921b4f3624f07c17b6729457c5202b1b8e1deca4b1a9a95448ce/cadgen-0.8.2-py3-none-any.whl /tmp/
RUN pip install --no-cache-dir /tmp/cadgen-0.8.2-py3-none-any.whl 'trimesh>=4,<5' \
 && rm /tmp/cadgen-0.8.2-py3-none-any.whl
RUN useradd -m app
USER app
# upstream telemetry is on by default; turn it off before anything runs
RUN cadgen telemetry off
WORKDIR /work
