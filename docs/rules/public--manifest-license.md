# public/manifest-license

Package manifest declares the license

| | |
|---|---|
| Severity | info |
| Tiers | 1 |
| Stacks | any |
| Scope | each |
| Kind | expr |
| Source | fleetlint-recommended@0.1.0 |

## Requirement

The package manifest names the license with an SPDX identifier: `license` in package.json, `project.license` in pyproject.toml, `package.license` in Cargo.toml. Stacks without such a field are not checked.

## Why

Registries, SBOM tools and license checkers read the manifest, not the LICENSE file.

## Applies when

```cel
repo.public
```

## Check

For each `item` in:

```cel
repo.stacks
```

```cel
(item != "node" || scan("package.json", "\"license\"\\s*:").size() > 0) && (item != "python" || !file("pyproject.toml") || scan("pyproject.toml", "^\\s*license(-files)?\\s*=|License ::").size() > 0) && (item != "rust" || scan("Cargo.toml", "^\\s*license(-file)?(\\.workspace)?\\s*=").size() > 0)
```

## Fix

Add the SPDX identifier of the LICENSE file to the manifest; `fleetlint fix --apply` does it for package.json and Cargo.toml when the LICENSE text is one recognised license. GNU licenses are left to you: write `-only` or `-or-later`, which the license text does not decide.

Agent instruction: Set the manifest's license field to the SPDX identifier matching the LICENSE file; ask if the LICENSE text is not a recognised license.

`fleetlint fix` can apply this rule's fix automatically (2 action(s)).

See: docs/baseline.md#public-repositories
