# UGT's Linux layer: Alpine Linux with Docker Engine, Compose and Buildx, as one
# file system in one image layer. CI builds it once per revision of this folder
# and publishes it as ghcr.io/auscaster/ugt/layer:<revision>. Windows imports it
# as UGT's WSL distro, UGT's Mac machine boots it, and verification runs recipes
# inside it, so every recipe runs on exactly the engine it was verified on.
FROM alpine:3.24@sha256:be2656061b536c1883c4034790e21d0a09c5aaaf51968b89368a3c0371ea2c9a AS build
ARG REVISION
COPY provision.sh /tmp/provision.sh
RUN test -n "$REVISION" \
 && sh /tmp/provision.sh && rm /tmp/provision.sh \
 && mkdir -p /usr/share/ugt && echo "$REVISION" > /usr/share/ugt/revision \
 && find / -xdev ! -type d -print | sort > /usr/share/ugt/layer.files

FROM scratch
COPY --from=build / /
