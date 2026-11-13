#!/usr/bin/env bash
# Release commit and signed tag for VERSION: dates [Unreleased] in CHANGELOG.md and points ci/gitlab/fleetlint.yml at it.
# --security first adds the security updates of go.mod/go.sum since the last tag to [Unreleased] (advisories via gh).
# Usage: scripts/release.sh [--security] vX.Y.Z
set -euo pipefail

security=false
if [[ ${1:-} == --security ]]; then
  security=true
  shift
fi
v=${1:?usage: scripts/release.sh [--security] vX.Y.Z}
[[ $v =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]] || {
  echo "not a version: $v" >&2
  exit 2
}

if $security; then
  last=$(git describe --tags --abbrev=0 --match 'v*')
  entries=""
  while read -r sha subject; do
    subject=$(sed -E 's/^[a-z]+(\([^)]*\))?!?: //; s/ \(#[0-9]+\)$//; s/ \[security\]$//I' <<<"$subject")
    # Renovate puts each advisory's IDs on the line after its heading: [CVE-...](...) / [GO-...](...)
    ids=$(gh api "repos/{owner}/{repo}/commits/$sha/pulls" --jq '.[0].body // ""' 2>/dev/null |
      grep -E '^\[(CVE|GHSA|GO)-' | grep -oE 'CVE-[0-9]{4}-[0-9]+|GHSA(-[a-z0-9]{4}){3}|GO-[0-9]{4}-[0-9]+' |
      sort -u | paste -sd, - | sed 's/,/, /g') || ids=""
    entries+="- ${subject^}${ids:+ ($ids)}"$'\n'
  done < <(git log --reverse -i --grep='\[security\]' --format='%H %s' "$last"..HEAD -- go.mod go.sum)
  [[ -n $entries ]] || {
    echo "no security updates of go.mod or go.sum since $last" >&2
    exit 1
  }
  # under an existing "### Security" of [Unreleased], else as that section's last block
  ENTRIES=$entries awk '
    /^## \[Unreleased\]$/ { print; inu = 1; next }
    inu && /^## \[/ { if (!done) printf "### Security\n\n%s\n", ENVIRON["ENTRIES"]; inu = 0 }
    inu && /^### Security$/ { print; getline; print; printf "%s", ENVIRON["ENTRIES"]; done = 1; next }
    { print }
  ' CHANGELOG.md >CHANGELOG.md.new
  mv CHANGELOG.md.new CHANGELOG.md
fi

sed -i "s/^## \[Unreleased\]$/## [Unreleased]\n\n## [${v#v}] - $(date +%F)/" CHANGELOG.md
sed -i -E "s#(fleetlint/fleetlint/|FLEETLINT_VERSION: )v[0-9]+\.[0-9]+\.[0-9]+#\1$v#g" ci/gitlab/fleetlint.yml
git add CHANGELOG.md ci/gitlab/fleetlint.yml
SKIP=no-commit-to-branch git commit -q -m "chore(release): $v"
git tag -s "$v" -m "$v"
