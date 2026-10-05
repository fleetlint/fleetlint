# Fleet mode

`fleetlint fleet` checks many repositories and writes one report.

```sh
fleetlint fleet ~/src/api ~/src/web                 # local paths
fleetlint fleet --repos fleet.yaml --out reports    # a fleet file
fleetlint fleet --repos fleet.yaml --update --deep  # pull clones, run make check in each
fleetlint fleet --from github:acme                  # every repository of an owner
```

## Fleet file

```yaml
version: 1
cache: ~/.cache/fleetlint/repos      # where url entries are cloned (shallow)
repos:
  - path: ../api                       # relative to this file
    team: platform
  - url: https://github.com/example/absorb.git
    name: absorb                       # optional; derived from the URL otherwise
    team: mobile
```

An entry may also carry `visibility: public` or `private`: what the forge says about the repository, used when the repository does not say itself (through `facts.visibility` or the `agent.public` git setting).

Each entry has exactly one of `path` or `url`. Clones are shallow and reused; `--update` pulls before checking. Each repository is checked with its own `.fleetlint.yaml` (or the defaults), so the fleet view reflects what each repo actually enforces.

## Discovering repositories on a forge

```sh
fleetlint fleet --from github:acme                      # an organization or a user
fleetlint fleet --from gitea:git.example.com/platform   # a Gitea or Forgejo instance
fleetlint fleet --repos fleet.yaml --from github:acme   # in addition to a fleet file
```

`--from` asks the forge's REST API for the owner's repositories and adds each as a `url` entry with the visibility the forge reports. Archived repositories and forks are skipped. Without a token only public repositories are listed; `GITHUB_TOKEN` (or `GH_TOKEN`) and `GITEA_TOKEN` are read from the environment and sent to that forge only. `GITHUB_API_URL` selects a GitHub Enterprise Server instance. Clones use git's own credentials and go to `--cache`, or to the user cache directory when none is given. fleetlint only reads: it never writes to the forge.

## Output directory

| File | Contents |
|---|---|
| `fleet.html` | self-contained page: repositories with compliance, errors, warnings, excepted and disabled counts; the repo × rule matrix; policy notes. No scripts, works from a file share. |
| `fleet.md` | the same as Markdown, for a wiki or an issue |
| `fleet.json` | machine-readable, including every result, all facts and the team summary |
| `fleet.csv` | one row per repository (team, visibility, tier, compliance, counts), for a spreadsheet or existing reporting |
| `actions/<repo>.md` | the ordered task list for that repository, ready to hand to a coding agent |
| `history/<timestamp>.json` | a copy of `fleet.json` per run; keep the directory in git or a bucket to see drift over time |

Compliance is the share of applicable rules that pass or are excepted. A repository that could not be checked (missing path, clone failure, invalid config) is listed with its error and never counted as compliant.

## Teams

When entries carry `team:`, the report adds a table per team: repositories, how many could not be checked, mean compliance of the checked ones, and the totals of errors, warnings, excepted findings, disabled rules, lowered severities and exceptions that have expired or will within 30 days. Repositories without a team are grouped as `(none)`. Together with the layer column in the policy notes this answers which team weakened what.

## Policy notes

The report lists every disabled rule, every lowered severity and every exception across the fleet, with the reason each repository gave and the deadline where there is one. Expired exceptions are marked, and those due within 30 days show the days left, each with its team. This is how weakening stays visible: it is allowed, but never silent.

## Exit code

`--fail` makes the command exit 1 when any repository has error-severity findings, a rule error, or could not be checked, for use in a scheduled job. Command rules in the repositories run only when the fleet run is given `--deep`.

## Onboarding many repositories

The output directory is a complete static site: `index.html` duplicates `fleet.html`, and every action list exists as both `.md` (for an agent) and `.html` (for a browser), so pointing GitHub or Gitea Pages at the directory publishes the report. `deploy/demo/` holds the fleet file and workflow behind demo.fleetlint.org.

In each repository, `fleetlint init --pr` writes the configuration, commits it on the branch `fleetlint/onboarding` as you, and opens a pull request with the Markdown report as its description using `gh` (GitHub) or `tea` (Gitea). Without either, the branch and commit remain and the command says so.
