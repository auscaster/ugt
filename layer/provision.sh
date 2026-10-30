#!/bin/sh
# Provisions UGT's Linux layer: Docker Engine with Compose and Buildx on
# Alpine Linux, started when the distro boots. It runs as root in a fresh
# Alpine minirootfs: inside the WSL distro UGT, run by ugt setup, and in CI to
# build a prebuilt layer. Running it again changes nothing.
set -eu

apk add --no-cache docker docker-cli-compose docker-cli-buildx ca-certificates

mkdir -p /etc/docker /var/lib/ugt/workspaces /var/log
# The proxy's absolute path, so the engine starts whatever PATH the distro's
# boot command runs with: there, a lookup failed and dockerd refused to start.
proxy=$(command -v docker-proxy)
cat > /etc/docker/daemon.json <<JSON
{
  "userland-proxy-path": "$proxy",
  "log-driver": "json-file",
  "log-opts": { "max-size": "10m", "max-file": "3" }
}
JSON

# ugt-engine starts dockerd once and waits until it answers. The distro's
# boot command runs it, and ugt runs it again before use, so the engine is up
# whichever starts first.
cat > /usr/local/bin/ugt-engine <<'SH'
#!/bin/sh
export PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
docker info >/dev/null 2>&1 && exit 0
# the boot command and ugt can both get here: one starts dockerd, the other waits
exec 9>/tmp/ugt-engine.lock
flock 9
docker info >/dev/null 2>&1 && exit 0 # another caller started it while this one waited
if ! pgrep -x dockerd >/dev/null; then
  # 9>&-: dockerd must not inherit the lock, or it would hold it for good
  nohup dockerd >>/var/log/dockerd.log 2>&1 9>&- &
fi
i=0
while [ "$i" -lt 60 ]; do
  docker info >/dev/null 2>&1 && exit 0
  pgrep -x dockerd >/dev/null || break # it stopped: waiting longer is no use
  i=$((i + 1))
  sleep 1
done
echo "dockerd did not start; its log: /var/log/dockerd.log" >&2
tail -n 20 /var/log/dockerd.log >&2
exit 1
SH
chmod 755 /usr/local/bin/ugt-engine

# The distro keeps to itself: no Windows drives mounted and no Windows
# programs on its path or callable from it. Files reach it only through ugt.
cat > /etc/wsl.conf <<'CONF'
[boot]
command = /usr/local/bin/ugt-engine
[automount]
enabled = false
mountFsTab = false
[interop]
enabled = false
appendWindowsPath = false
[user]
default = root
CONF

echo "ugt layer: $(docker --version), $(docker compose version), $(docker buildx version)"
