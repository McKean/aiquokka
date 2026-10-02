#!/bin/sh
set -eu

repo="${AIQUOKKA_REPO:-McKean/aiquokka}"
version="${VERSION:-latest}"
bin_dir="${BIN_DIR:-$HOME/.local/bin}"

die() {
  printf 'install: %s\n' "$*" >&2
  exit 1
}

case "$repo" in
  ''|*[!A-Za-z0-9_./-]*|/*|*/|*/*/*) die "invalid repository: $repo" ;;
  */*) ;;
  *) die "repository must be owner/name" ;;
esac

case "$(uname -s)" in
  Darwin) goos=darwin ;;
  Linux) goos=linux ;;
  *) die "unsupported operating system; on Windows, use install.ps1" ;;
esac

case "$(uname -m)" in
  x86_64|amd64) goarch=amd64 ;;
  arm64|aarch64) goarch=arm64 ;;
  *) die "unsupported architecture: $(uname -m)" ;;
esac

for dependency in curl tar awk mktemp install mv; do
  command -v "$dependency" >/dev/null 2>&1 || die "$dependency is required"
done

if command -v sha256sum >/dev/null 2>&1; then
  hash_command=sha256sum
elif command -v shasum >/dev/null 2>&1; then
  hash_command=shasum
else
  die "sha256sum or shasum is required"
fi

download() {
  curl --fail --silent --show-error --location --retry 3 \
    --connect-timeout 10 --max-time 120 --proto '=https' --tlsv1.2 "$@"
}

releases_url="https://github.com/$repo/releases"
if [ "$version" = latest ]; then
  latest_url="$(download --output /dev/null --write-out '%{url_effective}' "$releases_url/latest")" ||
    die "could not find the latest release in $repo; a published release is required"
  case "$latest_url" in
    "$releases_url/tag/"*) version="${latest_url##*/}" ;;
    *) die "could not determine the latest release in $repo" ;;
  esac
fi

case "$version" in
  ''|[!A-Za-z0-9]*|*[!A-Za-z0-9._+-]*) die "invalid release version: $version" ;;
esac

tmp="$(mktemp -d "${TMPDIR:-/tmp}/aiquokka-install.XXXXXXXX")"
staged=""
cleanup() {
  if [ -n "$staged" ]; then
    rm -f "$staged"
  fi
  rm -rf "$tmp"
}
trap cleanup 0
trap 'exit 1' HUP INT TERM

asset="aiquokka_${goos}_${goarch}.tar.gz"
download_url="$releases_url/download/$version"
printf 'Downloading aiquokka %s for %s/%s\n' "$version" "$goos" "$goarch"
download --output "$tmp/$asset" "$download_url/$asset" || die "could not download $asset"
download --output "$tmp/checksums.txt" "$download_url/checksums.txt" || die "could not download checksums.txt"

expected="$(awk -v asset="$asset" '$2 == asset || $2 == "*" asset { print $1 }' "$tmp/checksums.txt")"
case "$expected" in
  ''|*[!a-fA-F0-9]*) die "missing or invalid checksum for $asset" ;;
esac
[ "${#expected}" -eq 64 ] || die "invalid checksum for $asset"

if [ "$hash_command" = sha256sum ]; then
  actual="$(sha256sum "$tmp/$asset")"
else
  actual="$(shasum -a 256 "$tmp/$asset")"
fi
actual="${actual%% *}"
[ "$actual" = "$expected" ] || die "SHA-256 verification failed for $asset"

tar -xzf "$tmp/$asset" -C "$tmp" aiquokka || die "could not extract aiquokka"
[ -f "$tmp/aiquokka" ] && [ ! -L "$tmp/aiquokka" ] || die "archive does not contain a regular aiquokka binary"
mkdir -p "$bin_dir" || die "cannot create $bin_dir; set BIN_DIR to a writable directory"
[ -w "$bin_dir" ] || die "$bin_dir is not writable; set BIN_DIR to a writable directory"
staged="$(mktemp "$bin_dir/.aiquokka.XXXXXXXX")"
install -m 0755 "$tmp/aiquokka" "$staged"
mv -f "$staged" "$bin_dir/aiquokka"
staged=""
printf 'Installed %s\n' "$bin_dir/aiquokka"

case ":$PATH:" in
  *":$bin_dir:"*) ;;
  *) printf 'Add this directory to PATH:\n  export PATH="%s:%s"\n' "$bin_dir" "\$PATH" ;;
esac
if resolved="$(command -v aiquokka)" && [ "$resolved" != "$bin_dir/aiquokka" ]; then
  printf 'Another aiquokka appears first on PATH: %s\n' "$resolved" >&2
fi
