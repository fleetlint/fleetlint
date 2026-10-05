# release/sbom

Releases publish an SBOM

| | |
|---|---|
| Severity | error |
| Tiers | 1 |
| Stacks | any |
| Scope | root |
| Kind | outcome |
| Source | fleetlint-recommended@0.1.0 |

## Requirement

Every release produces an SPDX or CycloneDX SBOM for its artifacts, generated inside the release pipeline and published alongside them. Any recognised producer counts: GoReleaser `sboms:`, anchore/sbom-action or CycloneDX actions, syft/cyclonedx/trivy commands, a Makefile `sbom` target the release workflow calls, a build-tool plugin, or `docker/build-push-action` with `sbom: true`. Repositories add producers with `accept:`.

## Why

Consumers and scanners need to know what is inside an artifact; generating the SBOM in the pipeline ties it to the exact build.

## Applies when

```cel
release.exists
```

## Fix

Add an SBOM step to the tag-triggered release workflow (syft, SPDX JSON, one per artifact) or a `sboms:` section to .goreleaser.yaml, and upload the files with the release.

Agent instruction: In the tag-triggered release workflow add a step running syft on each artifact producing SPDX JSON files, and include them in the release upload step. Do not change other steps. If the repo uses GoReleaser, add a `sboms:` section instead.

See: docs/baseline.md#releases
