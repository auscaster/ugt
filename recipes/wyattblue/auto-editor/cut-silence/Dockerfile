FROM python:3.11-slim-trixie@sha256:db20d8506e2f15b6db39b40b6015ca46bae855dec6dd326d462cfe79ae972893
RUN apt-get update && apt-get install -y --no-install-recommends ffmpeg && rm -rf /var/lib/apt/lists/*
ARG TARGETARCH
RUN case "$TARGETARCH" in arm64) cpu=aarch64; digest=ca00d14dfa2a62f89f8d6e51d6c2d6210d62622b9ddf6bda8027ae79cc7c4005 ;; amd64) cpu=x86_64; digest=ab6da1fb01019505eea416b948d053b4d5918778ddaeea9955b90bafd8227713 ;; *) exit 1 ;; esac \
 && python -c 'from urllib.request import urlretrieve; import sys; urlretrieve(sys.argv[1],"/usr/local/bin/auto-editor")' "https://github.com/WyattBlue/auto-editor/releases/download/31.8.0/auto-editor-linux-$cpu" \
 && echo "$digest  /usr/local/bin/auto-editor" | sha256sum -c - && chmod 755 /usr/local/bin/auto-editor
WORKDIR /work
ENTRYPOINT ["auto-editor"]
