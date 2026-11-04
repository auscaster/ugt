FROM python:3.11-slim-bookworm@sha256:94a8fee35252d06ab75856f15f12c7bae67b10e8a6da93c7fded82b2610926cd
RUN apt-get update && apt-get install -y --no-install-recommends tesseract-ocr-eng tesseract-ocr fonts-dejavu-core libgl1 libglib2.0-0 && rm -rf /var/lib/apt/lists/*
COPY wheels.txt /tmp/wheels.txt
RUN pip download --no-deps --only-binary=:all: --require-hashes -r /tmp/wheels.txt -d /tmp/wheels && pip install --no-cache-dir --only-binary=:all: /tmp/wheels/*.whl Pillow openpyxl && rm -rf /tmp/wheels
WORKDIR /work
ENTRYPOINT ["python","check/task.py"]
