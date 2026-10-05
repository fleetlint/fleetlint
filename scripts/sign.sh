#!/usr/bin/env bash
# Sign one release artifact with cosign. Keyless (OIDC) when no key is present,
# which is the GitHub path; key-based when COSIGN_PRIVATE_KEY is set, which is
# the Gitea path. Called by goreleaser (see .goreleaser.yaml, signs:).
set -euo pipefail
artifact=${1:?artifact path}
args=(sign-blob --yes "--bundle=${artifact}.sigstore.json")
if [[ -n "${COSIGN_PRIVATE_KEY:-}" ]]; then
  args+=(--key env://COSIGN_PRIVATE_KEY)
fi
exec cosign "${args[@]}" "$artifact"
