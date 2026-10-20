# 0006 — Catalogs apply in import order; labels are free

Date: 2026-10-20. Status: accepted. Replaces the fixed layers of 2026-10-07.

## Context
Since 0.2 a catalog belonged to one of four layers (preset, org, team, repo) and the loader sorted catalogs by layer whatever `extends` said. Real organisations have business units, several org-wide catalogs owned by different teams (security, platform, compliance) and teams under teams. Every such shape would have needed a new layer vocabulary, and the vocabulary was the only thing the layers added: the include graph already orders catalogs (an include loads before the catalog that includes it, a catalog loads once per digest), and policy only tightens (a lock set earlier holds against everything later).

## Decision
Catalogs apply in the order `includes` and `extends` list them: the sources file's baseline first, then the repository's `extends` as written, nothing reordered. The repository's own `.fleetlint.yaml` is still the last catalog. Whoever lists a catalog first binds the ones after it; a team catalog that must come after the organisation's includes it. `metadata.layer` becomes `metadata.label`, a free name (`org`, `team/mobile`, `security-team`) used for attribution in reports and for `--require`, defaulting to the sources-file name the catalog is loaded under, else its own name; sources-file names are free too. Two versions of one catalog name in a run are an error. `fleetlint hierarchy` and every report list the catalogs in the order they applied, with their pins, so a rule can be followed to the content that defined it.

Rejected: a declared ordered list of layers in the sources file (a superset of the four layers, but a vocabulary every catalog author has to agree on); keeping files of the repository behind everything fetched (the repository owner who writes `extends` could drop the organisation's catalog anyway; the sources-file baseline and `--require` are the real guarantees).

## Consequences
The guarantee "extends can be in any order" becomes "the order is the include graph, then the baseline, then the repository's list"; still deterministic, still pinned. A repository without a sources file can list its own catalog in front of a remote one and lock what the remote one defines, which the report then attributes to the repository's file. Organisations that want to go first everywhere put their catalogs in the sources file's `extends`. Fleet files follow the same shape: a fleet file includes fleet files, and groups nest by path.
