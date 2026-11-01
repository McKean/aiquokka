#!/bin/sh
# Install aiquokka from the published release.
#
# Picks the archive for this OS and architecture, verifies its sha256
# checksum, and moves the binary into INSTALL_DIR (~/.local/bin).
#
#   curl -fsSL https://raw.githubusercontent.com/McKean/aiquokka/main/install.sh | sh
#
# AIQUOKKA_VERSION pins a release tag instead of the latest; INSTALL_DIR
# changes where the binary goes; AIQUOKKA_DOWNLOAD_BASE changes where the
# release files come from (the test points it at local fixtures).
set -eu

say() { printf '%s\n' "$*"; }
die() { printf 'aiquokka: %s\n' "$*" >&2; exit 1; }

command -v curl >/dev/null 2>&1 || die "need curl to download the release"

os=$(uname -s)
case "$os" in
  Darwin) os=darwin ;;
  Linux) os=linux ;;
  *) die "unsupported OS: $os (releases cover darwin and linux)" ;;
esac

arch=$(uname -m)
case "$arch" in
  arm64 | aarch64) arch=arm64 ;;
  amd64 | x86_64) arch=amd64 ;;
  *) die "unsupported architecture: $arch (releases cover arm64 and amd64)" ;;
esac

if [ -n "${AIQUOKKA_VERSION:-}" ]; then
  version=$AIQUOKKA_VERSION
else
  latest=$(curl -fsSL -o /dev/null -w '%{url_effective}' \
    https://github.com/McKean/aiquokka/releases/latest) \
    || die "could not find the latest release (is one published yet?)"
  version=${latest##*/}
  case "$version" in
    "" | releases | latest) die "could not find the latest release (is one published yet?)" ;;
  esac
fi

base=${AIQUOKKA_DOWNLOAD_BASE:-https://github.com/McKean/aiquokka/releases/download}
archive=aiquokka-$version-$os-$arch.tar.gz

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT INT TERM

curl -fsSL -o "$tmp/$archive" "$base/$version/$archive" \
  || die "could not download $archive from release $version"
curl -fsSL -o "$tmp/checksums.txt" "$base/$version/checksums.txt" \
  || die "could not download checksums.txt from release $version"

# Verify before anything is installed, so a failure leaves nothing behind.
want=$(awk -v a="$archive" '$2 == a { print $1 }' "$tmp/checksums.txt")
[ -n "$want" ] || die "no checksum for $archive in release $version"
if command -v sha256sum >/dev/null 2>&1; then
  sum=$(sha256sum "$tmp/$archive")
elif command -v shasum >/dev/null 2>&1; then
  sum=$(shasum -a 256 "$tmp/$archive")
else
  die "need sha256sum or shasum to verify the download"
fi
have=${sum%% *}
[ "$have" = "$want" ] || die "checksum mismatch for $archive, refusing to install"

tar -xzf "$tmp/$archive" -C "$tmp"

dest=${INSTALL_DIR:-$HOME/.local/bin}
mkdir -p "$dest"
if [ -d "$dest/aiquokka" ]; then
  die "cannot install: $dest/aiquokka is a directory"
fi
chmod +x "$tmp/aiquokka"
mv "$tmp/aiquokka" "$dest/aiquokka"
say "installed aiquokka $version to $dest/aiquokka"

case ":${PATH:-}:" in
  *":$dest:"*) ;;
  *) say "note: $dest is not on PATH, so 'aiquokka' will not be found until you add it" ;;
esac
