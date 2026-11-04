FROM python:3.11-slim-bookworm@sha256:94a8fee35252d06ab75856f15f12c7bae67b10e8a6da93c7fded82b2610926cd
RUN apt-get update && apt-get install -y --no-install-recommends ffmpeg espeak-ng && rm -rf /var/lib/apt/lists/*
ARG TARGETARCH
RUN case "$TARGETARCH" in arm64) asset=aarch64-unknown-linux-gnu; digest=14e02a1c0028f3ca0bdf83b62b3336e56ba0556894ef295a95e8573f06557166 ;; amd64) asset=x86_64-unknown-linux-musl; digest=70775e251eee44c0f2451a1e833326cf8bcbbe304d3e7cd12851e6fce72ef7da ;; *) exit 1 ;; esac \
 && python -c 'from urllib.request import urlretrieve; import sys; urlretrieve(sys.argv[1], "/usr/local/bin/deep-filter")' "https://github.com/Rikorose/DeepFilterNet/releases/download/v0.5.6/deep-filter-0.5.6-$asset" \
 && echo "$digest  /usr/local/bin/deep-filter" | sha256sum -c - && chmod 755 /usr/local/bin/deep-filter
WORKDIR /work
ENTRYPOINT ["sh","-ec"]
