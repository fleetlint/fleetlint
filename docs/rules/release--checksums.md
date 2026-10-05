# release/checksums

Releases publish checksums

| | |
|---|---|
| Severity | warning |
| Tiers | 1 |
| Stacks | any |
| Scope | root |
| Kind | outcome |
| Source | fleetlint-recommended@0.1.0 |

## Requirement

A checksums.txt (sha256) covering every artifact is uploaded with the release, and is what gets signed. A container image pushed to a registry is addressed by its digest and needs no separate file.

## Why

Checksums let a downloader verify integrity even without signature tooling.

## Applies when

```cel
release.exists && tagged_workflows().size() > 0
```

## Fix

Generate checksums.txt with sha256sum in the release job and upload it.

Agent instruction: Add `sha256sum dist/* > dist/checksums.txt` before the upload step and include dist/checksums.txt in the release assets.
