# Security policy

## Supported versions

The latest minor release. Older releases do not receive fixes.

## Reporting a vulnerability

Please do not open a public issue. Use GitHub's private vulnerability reporting on this repository ("Report a vulnerability" under the Security tab). You will get an acknowledgement within 7 days and a fix or a decision within 30.

## Scope

fleetlint reads repository files and runs `git` in the repository it is pointed at. It executes nothing else unless a repository's own `.fleetlint.yaml` declares a `command` rule, which remote catalogs cannot do. Reports about the trust boundary between catalogs and repositories are especially welcome.
