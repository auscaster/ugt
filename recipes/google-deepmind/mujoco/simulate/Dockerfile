FROM python:3.11-slim-bookworm@sha256:94a8fee35252d06ab75856f15f12c7bae67b10e8a6da93c7fded82b2610926cd
RUN apt-get update && apt-get install -y --no-install-recommends libosmesa6 ffmpeg && rm -rf /var/lib/apt/lists/*
RUN pip install --no-cache-dir mujoco==4.0.0 numpy==2.3.4 pillow==12.0.0
ENV MUJOCO_GL=osmesa PYOPENGL_PLATFORM=osmesa PYTHONDONTWRITEBYTECODE=1
WORKDIR /work
ENTRYPOINT ["python", "/recipe/simulate.py"]
