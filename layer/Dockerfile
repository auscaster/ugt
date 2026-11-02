# UGT's Linux layer: Alpine Linux with Docker Engine, Compose and Buildx, as one
# file system in one image layer. CI builds it once per revision of this folder
# and publishes it as ghcr.io/auscaster/ugt/layer:<revision>. Windows imports it
# as UGT's WSL distro and verification runs recipes inside it; the machine
# target, published as <revision>-machine, is what UGT's machine boots on a Mac
# and on Linux, and the qemu target, <revision>-qemu, runs it on Linux. So every
# recipe runs on exactly the engine it was verified on.
FROM alpine:3.24@sha256:be2656061b536c1883c4034790e21d0a09c5aaaf51968b89368a3c0371ea2c9a AS build
ARG REVISION
COPY provision.sh /tmp/provision.sh
RUN test -n "$REVISION" \
 && sh /tmp/provision.sh && rm /tmp/provision.sh \
 && mkdir -p /usr/share/ugt && echo "$REVISION" > /usr/share/ugt/revision \
 && find / -xdev ! -type d -print | sort > /usr/share/ugt/layer.files

FROM scratch AS layer
COPY --from=build / /

# The machine's root: the layer with Alpine's virtual-machine kernel, an
# initramfs that mounts a squashfs root under a memory overlay, sshd and socat
# for commands, tini as PID 1's reaper, and the machine's init.
FROM build AS machine-root
RUN apk add --no-cache linux-virt openssh-server socat tini e2fsprogs \
 && echo 'features="base virtio squashfs ext4"' > /etc/mkinitfs/mkinitfs.conf \
 && mkinitfs "$(ls /lib/modules)"
COPY --chmod=755 machine/init /usr/local/bin/ugt-init
COPY --chmod=755 machine/poweroff /usr/local/bin/ugt-poweroff
COPY machine/sshd_config /etc/ssh/ugt_sshd_config

# The machine's three files: the kernel, the initramfs, and the root as
# squashfs. On arm64 the kernel is the raw one Apple's boot loader needs, taken
# out of Alpine's EFI zboot image; a PC boots Alpine's own as it is.
FROM alpine:3.24@sha256:be2656061b536c1883c4034790e21d0a09c5aaaf51968b89368a3c0371ea2c9a AS machine-files
RUN apk add --no-cache squashfs-tools
COPY --from=machine-root / /root-fs
RUN set -eu; mkdir /out; k=/root-fs/boot/vmlinuz-virt; \
 if [ "$(uname -m)" = aarch64 ]; then \
   test "$(head -c 8 "$k" | tail -c 4)" = zimg \
   && off=$(od -An -t u4 -j 8 -N 4 "$k" | tr -d ' ') && size=$(od -An -t u4 -j 12 -N 4 "$k" | tr -d ' ') \
   && tail -c +$((off + 1)) "$k" | head -c "$size" | gunzip > /out/kernel \
   && test "$(dd if=/out/kernel bs=1 skip=56 count=4 2>/dev/null)" = "$(printf 'ARM\144')"; \
 else \
   test "$(dd if="$k" bs=1 skip=514 count=4 2>/dev/null)" = HdrS && cp "$k" /out/kernel; \
 fi \
 && cp /root-fs/boot/initramfs-virt /out/initramfs \
 && rm -rf /root-fs/boot/* \
 && mksquashfs /root-fs /out/root.squashfs -comp zstd -noappend -quiet

FROM scratch AS machine
COPY --from=machine-files /out/ /

# QEMU for UGT's machine on Linux, built static from QEMU's signed release, so
# it needs nothing installed: headless, KVM only, with the firmware a PC boots
# a kernel with. QEMU's own libfdt is pinned by its wrap file's commit.
FROM alpine:3.24@sha256:be2656061b536c1883c4034790e21d0a09c5aaaf51968b89368a3c0371ea2c9a AS qemu-build
RUN apk add --no-cache build-base python3 ninja meson pkgconf bash perl flex bison linux-headers git \
    glib-dev glib-static zlib-dev zlib-static pcre2-static libffi-dev
ADD --checksum=sha256:5a8289c7940dc77d3873c81a6c29ba3afe1446fc9d36e77fa1658cdbecb84b22 \
    https://download.qemu.org/qemu-11.1.3.tar.xz /qemu.tar.xz
RUN set -eu; mkdir /src /out; tar -xf /qemu.tar.xz -C /src --strip-components=1; cd /src \
 && ./configure --static --target-list=x86_64-softmmu --without-default-features --enable-kvm --disable-tcg \
      --disable-pixman --disable-docs --disable-tools --disable-user --disable-guest-agent --enable-fdt=internal \
      --disable-werror --prefix=/opt/qemu >/dev/null \
 && make -j"$(nproc)" >/dev/null && make install >/dev/null \
 && strip -o /out/qemu-system-x86_64 /opt/qemu/bin/qemu-system-x86_64 \
 && cp /opt/qemu/share/qemu/bios-256k.bin /opt/qemu/share/qemu/linuxboot_dma.bin /opt/qemu/share/qemu/kvmvapic.bin /out/

FROM scratch AS qemu
COPY --from=qemu-build /out/ /
