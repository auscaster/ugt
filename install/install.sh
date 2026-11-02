#!/bin/sh
# Installs UGT (Use the GitHub Thing) on a Mac or Linux:
#
#   curl -fsSL https://usegithubthing.com/install | sh
#
# It puts ugt in ~/.local/bin, checked against the release's digests, then sets
# UGT up and connects the AI apps it finds. It needs no admin rights: on an
# Apple silicon Mac UGT runs its own Linux machine, and elsewhere it uses the
# container engine already there. UGT_VERSION=0.2.0 installs that release
# instead of the newest; UGT_INSTALL_DIR puts ugt somewhere else.
set -eu

releases=${UGT_RELEASES:-https://github.com/auscaster/ugt/releases}
dir=${UGT_INSTALL_DIR:-$HOME/.local/bin}

say() { printf '%s\n' "$*"; }
fail() {
	printf 'ugt install: %s\n' "$*" >&2
	exit 1
}

case "$(uname -s)" in
Darwin) os=darwin ;;
Linux) os=linux ;;
*) fail "this script is for a Mac or Linux. On Windows, in PowerShell: irm https://usegithubthing.com/install.ps1 | iex" ;;
esac
case "$(uname -m)" in
arm64 | aarch64) arch=arm64 ;;
x86_64 | amd64) arch=amd64 ;;
*) fail "UGT has no release for $(uname -m)" ;;
esac
# a shell running under Rosetta on Apple silicon still gets the Apple silicon ugt
if [ "$os" = darwin ] && [ "$arch" = amd64 ] && [ "$(sysctl -n sysctl.proc_translated 2>/dev/null || true)" = 1 ]; then
	arch=arm64
fi

if [ -n "${UGT_VERSION:-}" ]; then
	base="$releases/download/v$UGT_VERSION"
else
	base="$releases/latest/download"
fi
name="ugt-$os-$arch"

# downloaded beside where it goes, so putting it there is one rename and a
# ugt running now keeps the file it started from
mkdir -p "$dir"
part="$dir/.ugt-install-$$"
sums="$part.sums"
trap 'rm -f "$part" "$sums"' EXIT
fetch() { curl -fsSL --retry 3 -o "$2" "$1" || fail "could not download $1"; }

say "Downloading $name from $base"
fetch "$base/SHA256SUMS" "$sums"
fetch "$base/$name" "$part"
want=$(awk -v n="$name" '$2 == n { print $1 }' "$sums")
[ -n "$want" ] || fail "the release lists no $name"
if command -v sha256sum >/dev/null 2>&1; then
	got=$(sha256sum "$part" | awk '{ print $1 }')
else
	got=$(shasum -a 256 "$part" | awk '{ print $1 }')
fi
[ "$got" = "$want" ] || fail "the download of $name does not match the release's digest: not installing it"
chmod 755 "$part"
mv -f "$part" "$dir/ugt"
say "Installed $("$dir/ugt" version | head -n 1) in $dir"

"$dir/ugt" setup || say "UGT is installed; ugt setup said what is left above."
"$dir/ugt" connect || say "Connect an AI app later with: $dir/ugt connect"

case ":$PATH:" in
*":$dir:"*) ;;
*) say "To type ugt alone, add $dir to your PATH, for example: echo 'export PATH=\"$dir:\$PATH\"' >> ~/.zprofile" ;;
esac
