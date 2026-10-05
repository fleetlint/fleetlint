# release/provenance

Releases attest build provenance

| | |
|---|---|
| Severity | warning |
| Tiers | 1 |
| Stacks | any |
| Scope | root |
| Kind | outcome |
| Source | fleetlint-recommended@0.1.0 |

## Requirement

The release pipeline produces a SLSA provenance attestation (actions/attest-build-provenance, the SLSA generator, cosign attest or npm provenance).

## Why

Provenance ties the artifact to the exact source and workflow that built it, which a signature alone does not.

## Applies when

```cel
release.exists && tagged_workflows().size() > 0
```

## Fix

Add actions/attest-build-provenance (GitHub) or `cosign attest --predicate` (Gitea) to the release workflow.

Agent instruction: Add a step `uses: actions/attest-build-provenance` with `subject-path` covering the release artifacts; grant `id-token: write` and `attestations: write` to that job only.
