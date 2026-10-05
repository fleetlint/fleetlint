# release/signing

Release artifacts are signed

| | |
|---|---|
| Severity | error |
| Tiers | 1 |
| Stacks | any |
| Scope | root |
| Kind | outcome |
| Source | fleetlint-recommended@0.1.0 |

## Requirement

Every release signs its artifacts: cosign keyless (OIDC) on GitHub, key-based cosign on Gitea, GoReleaser `signs:`, sigstore-python for wheels, or npm provenance. Signatures are published with the release. A container image is signed by digest with `cosign sign`.

## Why

Checksums prove integrity; only a signature proves who built it.

## Applies when

```cel
release.exists && tagged_workflows().size() > 0
```

## Fix

Add a cosign step to the release workflow: `cosign sign-blob` for checksums.txt when the release is files, `cosign sign <image>@<digest>` when it is a container image (keyless on GitHub, with a key on Gitea).

Agent instruction: Add sigstore/cosign-installer and a signing step after the artifacts are built. Files: `cosign sign-blob --yes --bundle checksums.txt.sigstore.json checksums.txt` and upload the bundle. Container image: `cosign sign --yes <image>@<digest>` using the digest output of the build step, with `id-token: write` on the job.

See: docs/baseline.md#releases
