# 0009 — Versions of packages and integrations

Date: 2026-10-24. Status: accepted.

## Context
fleetlint reaches users through channels that only ship the binary (the GitHub release, Homebrew, Scoop, the Nix flake, the Linux packages, the image, `install.sh`) and through integrations with code of their own (the action, the GitLab component, later the fleet app). The GitLab component started with the binary's version number (`fleetlint@0.8.1` installed v0.8.1). Its first bugs, a spec description over GitLab's 256-character limit and a folded `jq` command, were bugs of the component alone; with one number for both, a fix needed a fleetlint release or a tag that no longer said which release it installs, and every fleetlint patch would have forced empty component releases.

## Decision
- **Packages are lockstep.** A channel that ships the binary has the binary's version, published by the release workflow of the tag (`release.yml` writes the tap, the bucket and the flake). No channel is released by hand or numbered on its own.
- **Integrations have their own semantic version.** The action and the GitLab component each have a changelog and tags of their own, independent of fleetlint's: a fix to an integration is a patch of the integration only.
- **An integration pins a fleetlint release.** Its `version` input defaults to one exact release, never `latest`; Renovate bumps that default, and an integration release follows the bump. Users pin the integration and may override `version`.
- **Compatibility is written down.** An integration's README names the oldest fleetlint release each of its features needs (`pr` needs fleetlint 0.9.0); its release notes name the default it installs.
- **Services are deployed, not versioned for users.** A hosted integration (the fleet app) has no version to pin; its changelog says which fleetlint release it runs.
- **The catalog is versioned on its own** and pinned by fleetlint's go.mod, as before (engine levels say which fleetlint reads it).

Rejected: one number for everything (wrapper fixes need fleetlint releases, fleetlint patches need empty wrapper releases); integrations defaulting to `latest` (a run changes without a commit, and Renovate has nothing to propose).

## Consequences
The GitLab component and the action start at 0.1.0 with fleetlint 0.9.0 as their default; `fleetlint/components/fleetlint@0.1.0` installs v0.9.0. The component's tag check (default equals the tag) is gone. 1.0 of an integration is its own decision, after fleetlint's.
