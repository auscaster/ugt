# The converter the task runs, on an official base named by digest.
FROM debian:bookworm-slim@sha256:99c0b25c46d0214d5a2091092e7908907dcaecd6a962cde0ac700acee222b6f3
RUN apt-get update && apt-get install -y --no-install-recommends pandoc \
 && rm -rf /var/lib/apt/lists/* \
 && printf '#!/bin/sh\nexec pandoc "$1" -o "$2"\n' > /usr/local/bin/convert \
 && chmod +x /usr/local/bin/convert
WORKDIR /work
