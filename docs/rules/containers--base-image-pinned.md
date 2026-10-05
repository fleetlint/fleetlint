# containers/base-image-pinned

Base images are pinned by digest

| | |
|---|---|
| Severity | info |
| Tiers | 1 |
| Stacks | any |
| Scope | root |
| Kind | expr |
| Source | fleetlint-recommended@0.1.0 |

## Requirement

Every `FROM` names its image with `@sha256:<digest>`; `scratch`, build arguments and earlier stages are exempt.

## Why

A tag can be moved to a different image at any time; a digest cannot, and Renovate keeps it current.

## Applies when

```cel
tracked().exists(p, p.matches("(^|/)(Dockerfile|Containerfile)[^/]*$"))
```

## Check

For each `item` in:

```cel
tracked().filter(p, p.matches("(^|/)(Dockerfile|Containerfile)[^/]*$") && !p.endsWith(".dockerignore"))
```

```cel
lines(item).filter(l, l.matches("(?i)^\\s*FROM\\s+")).all(l,
  l.contains("@sha256:")
  || l.matches("(?i)^\\s*FROM\\s+(--platform=\\S+\\s+)?(scratch|\\$\\S+)(\\s|$)")
  || lines(item).exists(s, s.matches("(?i)^\\s*FROM\\s+.+\\s+AS\\s+[\\w.-]+\\s*$")
       && l.matches("(?i)^\\s*FROM\\s+(--platform=\\S+\\s+)?" + s.trim().substring(s.trim().lastIndexOf(" ") + 1) + "(\\s|$)")))
```

## Fix

Append the digest to each base image (`image:tag@sha256:…`); Renovate updates both.

Agent instruction: Resolve each base image tag to its current digest and write it as image:tag@sha256:<digest>; do not change the tag.

See: docs/baseline.md#robustness
