#!/bin/sh
# Installs a fleetlint release: curl -fsSL https://fleetlint.org/install.sh | sh
#   sh install.sh [version] [directory]   version: v0.2.0 or latest (default),
#                                          directory: default ~/.local/bin
# The archive is checked against checksums.txt; when cosign is installed, the
# signature of checksums.txt is verified first. FLEETLINT_VERIFY=cosign makes
# the signature check required.
set -eu

repo=https://github.com/fleetlint/fleetlint
version=${1:-latest}
dir=${2:-$HOME/.local/bin}

fail() {
  echo "install.sh: $*" >&2
  exit 1
}

if [ "$version" = latest ]; then
  url=$(curl -fsSLI -o /dev/null -w '%{url_effective}' "$repo/releases/latest") || fail "cannot reach $repo"
  version=${url##*/}
fi
v=${version#v}

case $(uname -s) in
  Linux) os=linux ;;
  Darwin) os=darwin ;;
  *) fail "no build for $(uname -s); on Windows use scoop or the zip from $repo/releases" ;;
esac
case $(uname -m) in
  x86_64 | amd64) arch=amd64 ;;
  aarch64 | arm64) arch=arm64 ;;
  *) fail "no build for $(uname -m)" ;;
esac

archive="fleetlint_${v}_${os}_${arch}.tar.gz"
base="$repo/releases/download/v$v"
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
cd "$tmp"
curl -fsSLO "$base/$archive" || fail "no $archive in release v$v"
curl -fsSLO "$base/checksums.txt"

if [ "${FLEETLINT_VERIFY:-}" = cosign ] && ! command -v cosign >/dev/null 2>&1; then
  fail "FLEETLINT_VERIFY=cosign but cosign is not installed"
fi
if command -v cosign >/dev/null 2>&1; then
  curl -fsSLO "$base/checksums.txt.sigstore.json"
  cosign verify-blob --bundle checksums.txt.sigstore.json \
    --certificate-identity "$repo/.github/workflows/release.yml@refs/tags/v$v" \
    --certificate-oidc-issuer https://token.actions.githubusercontent.com \
    checksums.txt >/dev/null 2>&1 || fail "the signature of checksums.txt does not verify"
  echo "signature of checksums.txt verified"
fi

grep " $archive\$" checksums.txt >sum || fail "$archive is not in checksums.txt"
if command -v sha256sum >/dev/null 2>&1; then
  sha256sum -c sum >/dev/null || fail "checksum mismatch for $archive"
else
  shasum -a 256 -c sum >/dev/null || fail "checksum mismatch for $archive"
fi

tar -xzf "$archive" fleetlint
mkdir -p "$dir"
mv fleetlint "$dir/fleetlint"
echo "installed fleetlint v$v to $dir/fleetlint"
case ":$PATH:" in
  *":$dir:"*) ;;
  *) echo "add $dir to PATH" ;;
esac
