#!/bin/sh
# Tests for install.sh, with no network: fixture releases are served over
# file://, platforms are faked with a uname shim, and a curl guard refuses
# any http(s) URL. Run from the repo root: sh install_test.sh
set -eu

root=$(dirname -- "$0")
case "$root" in
  /*) ;;
  *) root=$PWD/$root ;;
esac

n=0
pass() { n=$((n + 1)); printf 'ok: %s\n' "$*"; }
fail() { printf 'FAIL: %s\n' "$*" >&2; exit 1; }

command -v curl >/dev/null 2>&1 || { echo "need curl" >&2; exit 1; }
command -v tar >/dev/null 2>&1 || { echo "need tar" >&2; exit 1; }
real_curl=$(command -v curl)

if command -v sha256sum >/dev/null 2>&1; then
  hash_of() { sha256sum "$1"; }
elif command -v shasum >/dev/null 2>&1; then
  hash_of() { shasum -a 256 "$1"; }
else
  echo "need sha256sum or shasum" >&2; exit 1
fi

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT INT TERM

# make_fixture dir version os arch: a fake release tree holding one archive
# plus its checksums.txt, shaped like the published release.
make_fixture() {
  mkdir -p "$1/$2" "$1/root"
  printf '#!/bin/sh\necho "aiquokka-stub %s %s %s"\n' "$2" "$3" "$4" > "$1/root/aiquokka"
  chmod +x "$1/root/aiquokka"
  tar -czf "$1/$2/aiquokka-$2-$3-$4.tar.gz" -C "$1/root" aiquokka
  sum=$(hash_of "$1/$2/aiquokka-$2-$3-$4.tar.gz")
  printf '%s  aiquokka-%s-%s-%s.tar.gz\n' "${sum%% *}" "$2" "$3" "$4" > "$1/$2/checksums.txt"
}

# make_uname dir s m: a shim reporting s for -s and m for -m.
make_uname() {
  cat > "$1/uname" <<EOF
#!/bin/sh
case "\$1" in
  -s) echo "$2" ;;
  -m) echo "$3" ;;
  *) echo "unexpected uname args" >&2; exit 1 ;;
esac
EOF
  chmod +x "$1/uname"
}

# make_curl_guard dir: a curl that refuses any http(s) URL, proving the
# install below touched no network.
make_curl_guard() {
  cat > "$1/curl" <<EOF
#!/bin/sh
for a in "\$@"; do
  case "\$a" in
    http://*|https://*) echo "network URL refused in test: \$a" >&2; exit 9 ;;
  esac
done
exec "$real_curl" "\$@"
EOF
  chmod +x "$1/curl"
}

case $(uname -s) in
  Darwin) native_os=darwin ;;
  *) native_os=linux ;;
esac
case $(uname -m) in
  arm64 | aarch64) native_arch=arm64 ;;
  *) native_arch=amd64 ;;
esac
raw_s=$(uname -s)
v=v9.9.9-test

# The archive for this platform installs and runs.
fix=$tmp/fix1; dest=$tmp/bin1; shim=$tmp/shim1
mkdir -p "$shim"
make_fixture "$fix" "$v" "$native_os" "$native_arch"
make_curl_guard "$shim"
if AIQUOKKA_VERSION=$v AIQUOKKA_DOWNLOAD_BASE="file://$fix" INSTALL_DIR="$dest" PATH="$shim:$PATH" \
    sh "$root/install.sh" >"$tmp/out1" 2>&1; then
  [ -x "$dest/aiquokka" ] || fail "native install: binary not installed"
  got=$("$dest/aiquokka")
  [ "$got" = "aiquokka-stub $v $native_os $native_arch" ] || fail "native install: ran '$got'"
  grep -F -q "not on PATH" "$tmp/out1" || fail "native install: no PATH warning"
  pass "installs the archive for this platform ($native_os/$native_arch)"
else
  fail "native install exited $?: $(cat "$tmp/out1")"
fi

# uname spellings map to the release target names.
i=0
for raw_m in x86_64 aarch64; do
  i=$((i + 1))
  case $raw_m in x86_64) want_arch=amd64 ;; *) want_arch=arm64 ;; esac
  fix=$tmp/fixmap$i; dest=$tmp/binmap$i; shim=$tmp/shimmap$i
  mkdir -p "$shim"
  make_fixture "$fix" "$v" "$native_os" "$want_arch"
  make_uname "$shim" "$raw_s" "$raw_m"
  make_curl_guard "$shim"
  if AIQUOKKA_VERSION=$v AIQUOKKA_DOWNLOAD_BASE="file://$fix" INSTALL_DIR="$dest" PATH="$shim:$PATH" \
      sh "$root/install.sh" >"$tmp/outmap$i" 2>&1; then
    got=$("$dest/aiquokka")
    [ "$got" = "aiquokka-stub $v $native_os $want_arch" ] || fail "$raw_m: ran '$got'"
    pass "maps $raw_m to $want_arch"
  else
    fail "$raw_m install exited $?: $(cat "$tmp/outmap$i")"
  fi
done

# A corrupted archive is refused and nothing is installed.
fix=$tmp/fixbad; dest=$tmp/binbad; shim=$tmp/shimbad
mkdir -p "$dest" "$shim"
make_fixture "$fix" "$v" "$native_os" "$native_arch"
printf 'corrupt' >> "$fix/$v/aiquokka-$v-$native_os-$native_arch.tar.gz"
make_curl_guard "$shim"
if AIQUOKKA_VERSION=$v AIQUOKKA_DOWNLOAD_BASE="file://$fix" INSTALL_DIR="$dest" PATH="$shim:$PATH" \
    sh "$root/install.sh" >"$tmp/outbad" 2>&1; then
  fail "bad checksum: install succeeded"
else
  grep -F -q "checksum" "$tmp/outbad" || fail "bad checksum: no checksum message: $(cat "$tmp/outbad")"
  if [ -e "$dest/aiquokka" ]; then fail "bad checksum: binary left half-installed"; fi
  pass "refuses an archive with a bad checksum and installs nothing"
fi

# A directory at the binary path is refused before the binary can be moved
# inside it and reported as installed at the wrong location.
fix=$tmp/fixdir; dest=$tmp/bindir; shim=$tmp/shimdir
mkdir -p "$dest/aiquokka" "$shim"
make_fixture "$fix" "$v" "$native_os" "$native_arch"
make_curl_guard "$shim"
if AIQUOKKA_VERSION=$v AIQUOKKA_DOWNLOAD_BASE="file://$fix" INSTALL_DIR="$dest" PATH="$shim:$PATH" \
    sh "$root/install.sh" >"$tmp/outdir" 2>&1; then
  fail "directory at binary path: install succeeded"
else
  grep -F -q "is a directory" "$tmp/outdir" || fail "directory at binary path: no message: $(cat "$tmp/outdir")"
  if [ -e "$dest/aiquokka/aiquokka" ]; then fail "directory at binary path: binary moved inside it"; fi
  pass "refuses a directory at the binary path without installing inside it"
fi

# A checksums.txt without the entry is refused too.
fix=$tmp/fixnone; dest=$tmp/binnone; shim=$tmp/shimnone
mkdir -p "$dest" "$shim"
make_fixture "$fix" "$v" "$native_os" "$native_arch"
printf 'deadbeef  some-other-file.tar.gz\n' > "$fix/$v/checksums.txt"
make_curl_guard "$shim"
if AIQUOKKA_VERSION=$v AIQUOKKA_DOWNLOAD_BASE="file://$fix" INSTALL_DIR="$dest" PATH="$shim:$PATH" \
    sh "$root/install.sh" >"$tmp/outnone" 2>&1; then
  fail "missing checksum: install succeeded"
else
  grep -F -q "no checksum" "$tmp/outnone" || fail "missing checksum: no message: $(cat "$tmp/outnone")"
  if [ -e "$dest/aiquokka" ]; then fail "missing checksum: binary left half-installed"; fi
  pass "refuses an archive with no checksum entry and installs nothing"
fi

# Platforms without a release stop with a plain message.
shim=$tmp/shimos; dest=$tmp/binos
mkdir -p "$shim" "$dest"
make_uname "$shim" "FreeBSD" "$(uname -m)"
make_curl_guard "$shim"
if AIQUOKKA_VERSION=$v AIQUOKKA_DOWNLOAD_BASE="file://$tmp/none" INSTALL_DIR="$dest" PATH="$shim:$PATH" \
    sh "$root/install.sh" >"$tmp/outos" 2>&1; then
  fail "unsupported OS: install succeeded"
else
  grep -F -q "unsupported OS" "$tmp/outos" || fail "unsupported OS: no message: $(cat "$tmp/outos")"
  pass "refuses an unsupported OS"
fi

shim=$tmp/shimarch; dest=$tmp/binarch
mkdir -p "$shim" "$dest"
make_uname "$shim" "$raw_s" "mips"
make_curl_guard "$shim"
if AIQUOKKA_VERSION=$v AIQUOKKA_DOWNLOAD_BASE="file://$tmp/none" INSTALL_DIR="$dest" PATH="$shim:$PATH" \
    sh "$root/install.sh" >"$tmp/outarch" 2>&1; then
  fail "unsupported arch: install succeeded"
else
  grep -F -q "unsupported architecture" "$tmp/outarch" || fail "unsupported arch: no message: $(cat "$tmp/outarch")"
  pass "refuses an unsupported architecture"
fi

printf '%d install checks passed\n' "$n"
