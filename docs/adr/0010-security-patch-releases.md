# 0010 — Security patch releases without review

Date: 2026-11-13. Status: accepted.

## Context
Renovate opens a pull request when a Go module fleetlint builds with, or the Go toolchain, has a published vulnerability (GitHub alerts and OSV). The fix reached users only when a maintainer merged the pull request, cut a release with `make release` and approved the release job, so the binaries, packages and the image kept the vulnerable version for as long as that took.

## Decision
- **Renovate merges its security pull requests** once `check` passes, at any time and without the release age wait (`vulnerabilityAlerts` in `renovate.json`).
- **A security update of `go.mod` or `go.sum` on main is released as a patch** by `security-release.yml`: when nothing since the last tag needs more than a patch (git-cliff, as in `make release`), it adds the updates and their advisories to the changelog's Security section, commits the release on main and pushes a tag signed by the release bot. When main has changes for a minor release, the run fails and a maintainer releases with `make release`.
- **The release bot is a deploy key**, the only key besides the admins' that may create `v*` tags. It is a secret of the `release-bot` environment, which only main can use; its public key is `.github/release-bot.allowed_signers`.
- **`release.yml` publishes a tag the release bot signed without review.** Every other tag waits for a maintainer in the `release-approval` environment, as before.
- Security updates of actions and CI tools are not released: they change nothing users install.

Rejected: approving security releases by hand (the fix waits for a person, which is what this removes); patch branches for security fixes while main has unreleased features (before 1.0 only the latest minor is supported, SECURITY.md; the run's failure asks for that minor instead).

## Consequences
A fixed dependency is released as soon as Renovate's pull request passes `check`, with no one in the loop. Whoever controls main controls the release bot, as they control the workflows. The integrations (ADR 0009) receive the release as an ordinary Renovate bump and release on their own.
