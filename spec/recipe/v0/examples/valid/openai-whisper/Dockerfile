FROM python:3.11-slim-bookworm
RUN apt-get update && apt-get install -y --no-install-recommends ffmpeg && rm -rf /var/lib/apt/lists/*
RUN pip install --no-cache-dir torch==2.5.1 --index-url https://download.pytorch.org/whl/cpu \
 || pip install --no-cache-dir torch==2.5.1
# the pinned source, fetched by the build itself: no clone on the host
ADD https://github.com/openai/whisper.git#86098128c0b4f24f0e2aa2994de830614b474227 /src
RUN pip install --no-cache-dir /src
# bake the base model (~139 MB) into the image so runs work offline
RUN python -c "import whisper; whisper.load_model('base', download_root='/models')"
ENV XDG_CACHE_HOME=/tmp
WORKDIR /work
ENTRYPOINT ["whisper"]
CMD ["--help"]
