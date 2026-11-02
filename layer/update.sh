#!/bin/sh
# Updates UGT's Linux layer in place to the one on stdin, a gzipped tar of its
# file system: it stops the engine, unpacks the new layer over the old, and
# removes the files the old layer had and the new one does not. Saved data,
# images and workspaces are not the layer's, so they are never touched.
set -eu
pkill -x dockerd 2>/dev/null || true
while pgrep -x dockerd >/dev/null; do sleep 1; done
old=$(mktemp)
cp /usr/share/ugt/layer.files "$old" 2>/dev/null || : >"$old"
# the files the distro's own system writes at start stay as it wrote them
tar -xzf - -C / --numeric-owner --exclude etc/resolv.conf --exclude etc/hosts --exclude etc/hostname
sort /usr/share/ugt/layer.files | comm -23 "$old" - | while IFS= read -r f; do
  case "$f" in
  /var/lib/docker/* | /var/lib/containerd/* | /var/lib/ugt/* | /var/log/* | /tmp/* | /root/*) ;;
  *) [ -d "$f" ] || rm -f "$f" ;;
  esac
done
rm -f "$old"
echo "ugt layer: $(cat /usr/share/ugt/revision)"
