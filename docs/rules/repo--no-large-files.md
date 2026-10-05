# repo/no-large-files

No large files are tracked outside git-lfs

| | |
|---|---|
| Severity | warning |
| Tiers | all |
| Stacks | any |
| Scope | root |
| Kind | expr |
| Source | fleetlint-recommended@0.1.0 |

## Requirement

No tracked file exceeds `max_kb` (default 2 MiB) unless its pattern is routed to git-lfs in .gitattributes.

## Why

Binaries in history make every clone slower forever; lfs or a release asset is the right home.

## Check

For each `item` in:

```cel
tracked()
```

```cel
filesize(item) <= params.max_kb * 1024 || lines(".gitattributes").exists(l, l.contains("filter=lfs") && globmatch(l.split(" ")[0], item))
```

## Parameters

- `max_kb`: `2048`

## Fix

Move the file to git-lfs (`git lfs track <pattern>`) or publish it as a release asset and remove it from history.

Agent instruction: Report the file; do not rewrite history. Suggest `git lfs track` for the pattern or an external home for the file.

See: docs/baseline.md#hygiene
