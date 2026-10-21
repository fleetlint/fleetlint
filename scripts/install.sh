#!/bin/sh
# Installs a fleetlint release: curl --proto '=https' --tlsv1.2 -fsSL https://fleetlint.org/install.sh | sh
#   sh install.sh [version] [directory]   version: v0.2.0, 0.2.0 or latest (default),
#                                          directory: default ~/.local/bin
# Verification: cosign checks the signature of checksums.txt, else an authenticated gh
# the build provenance, else only the checksum is checked; FLEETLINT_VERIFY=cosign requires cosign.
set -eu

repo=https://github.com/fleetlint/fleetlint
version=${1:-latest}
dir=${2:-$HOME/.local/bin}

fail() {
  echo "install.sh: $*" >&2
  exit 1
}

# https only, TLS 1.2 or later, no redirect to another scheme.
fetch() {
  curl --proto '=https' --tlsv1.2 -fsSL "$@"
}

if [ "$version" = latest ]; then
  url=$(fetch -I -o /dev/null -w '%{url_effective}' "$repo/releases/latest") || fail "cannot reach $repo"
  version=${url##*/}
fi
case $version in
  v[0-9]*.[0-9]*.[0-9]*) ;;
  [0-9]*.[0-9]*.[0-9]*) ;;
  *) fail "version must be vX.Y.Z, X.Y.Z or latest (got '$version')" ;;
esac
v=${version#v}
case $v in
  *[!0-9.]*) fail "version must be vX.Y.Z, X.Y.Z or latest (got '$version')" ;;
esac

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
fetch -O "$base/$archive" || fail "no $archive in release v$v"
fetch -O "$base/checksums.txt" || fail "no checksums.txt in release v$v"

if [ "${FLEETLINT_VERIFY:-}" = cosign ] && ! command -v cosign >/dev/null 2>&1; then
  fail "FLEETLINT_VERIFY=cosign but cosign is not installed"
fi
if command -v cosign >/dev/null 2>&1; then
  fetch -O "$base/checksums.txt.sigstore.json" || fail "no checksums.txt.sigstore.json in release v$v"
  cosign verify-blob --bundle checksums.txt.sigstore.json \
    --certificate-identity "$repo/.github/workflows/release.yml@refs/tags/v$v" \
    --certificate-oidc-issuer https://token.actions.githubusercontent.com \
    checksums.txt >/dev/null 2>&1 || fail "the signature of checksums.txt does not verify"
  echo "signature of checksums.txt verified"
elif command -v gh >/dev/null 2>&1 && { [ -n "${GH_TOKEN:-}${GITHUB_TOKEN:-}" ] || gh auth status >/dev/null 2>&1; }; then
  # Without cosign, gh checks the build provenance GitHub attested for the archive.
  gh attestation verify "$archive" --repo fleetlint/fleetlint >/dev/null 2>&1 || fail "the build provenance of $archive does not verify"
  echo "build provenance of $archive verified"
else
  echo "neither cosign nor an authenticated gh: only the checksum is checked"
fi

# The exact file name: checksums.txt also lists $archive.spdx.json.
awk -v a="$archive" '$2 == a' checksums.txt >sum
[ -s sum ] || fail "$archive is not in checksums.txt"
[ "$(wc -l <sum)" -eq 1 ] || fail "$archive is listed more than once in checksums.txt"
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
