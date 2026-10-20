# 0007 — Release checks per published artifact, artifacts as catalog data

Date: 2026-10-20. Status: accepted.

## Context
`release/signing`, `release/sbom`, `release/provenance` and `release/trusted-publishing` were capabilities with one implementation per stack and `combine: any`: the stack that publishes decided, and one recognised producer anywhere passed the repository. That could not tell "binaries signed, npm package not" from "everything signed", and the stack was the wrong axis anyway: a Go repository can publish an image and a Homebrew formula, a Node repository only an image. What needs signing, an SBOM or provenance is each artifact the release publishes, and what counts differs per artifact: npm's provenance is its signature, a crate has no signature at all, an image is signed by digest.

## Decision
An artifact kind is catalog data, like a stack: `artifacts/<id>.yaml` with a CEL `detect` expression that returns the workflows publishing it (the publish steps and the release tooling's declarations), evaluated once per repository at the root. The result is the fact `artifacts`; `facts: {artifacts: [...]}` pins it when detection misses a publish step, and an empty list says nothing is published. A capability with `by: artifact` has one implementation per kind (`rules/release/<name>/<artifact>.yaml`), checked for every kind the release publishes, each of which must pass; a kind where the check means nothing says so with `when: "false"` and reports not applicable; a kind without an implementation is a reported gap; a repository that publishes nothing detectable gets a not-applicable note on the capability instead of a silent pass. There is no `any` implementation for such a capability, since a generic cosign step on checksums does not sign an npm package. Images pushed from a push- or schedule-triggered workflow count as published too: `:latest` deserves a signature.

Rejected: keeping `combine: any` (a security verdict that passes when half the artifacts are unsigned); one `foreach` rule over publish steps (no per-artifact identity for overrides, exceptions and documentation); detection in Go (a new registry or publish action would be a release instead of a catalog change).

## Consequences
Engine level 5. The fleetlint catalog implements the four capabilities for twelve kinds (release-files, image, helm-chart, npm, pypi, crate, gem, maven, nuget, pub, android-app, apple-app); the stack-keyed ids are aliases. Step-level `if` guards in non-tag workflows and reusable workflows in other repositories are invisible to detection and are answered by the pin. Two images in one repository are one kind, not two instances. `combine:` stays in the format without a user in the catalog.
