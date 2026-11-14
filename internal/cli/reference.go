package cli

import (
	"io"

	"github.com/fleetlint/fleetlint/internal/docs"
)

// exitCodes are the exit codes as the CLI reference states them.
var exitCodes = []docs.ExitCode{
	{Code: ExitOK, Meaning: "`check` found nothing at or above `--fail-on`; any other command finished."},
	{Code: ExitFindings, Meaning: "`check` found a failing rule at or above `--fail-on` (default `error`), or a rule could not be evaluated, whatever `--fail-on` says. `fleet --fail`: a repository has error findings or rule errors, or could not be checked."},
	{Code: ExitConfig, Meaning: "Configuration or usage error: an invalid `.fleetlint.yaml` or catalog, a catalog that cannot be fetched, a catalog `--require` names that the configuration lacks, a path that is not a repository, an unknown flag or wrong arguments, `fix --apply` in a working tree with uncommitted changes (with `--pr`: no remote, or a default branch whose configuration fails in one of these ways), pages that differ under `docs --check`."},
	{Code: ExitInternal, Meaning: "Any other error: a push or pull request that failed, a git command that failed, output that could not be written."},
}

// environment lists every variable fleetlint reads, and the install
// script's; a test keeps it complete.
var environment = []docs.EnvVar{
	{Names: []string{"FLEETLINT_TOKEN"}, Doc: "Default for `--token` (`fix --apply --pr`, `init --pr`, `fleet`): the token for the forge's API and for pushing. Keeps the token out of the process list."},
	{Names: []string{"FLEETLINT_FORGE"}, Doc: "Default for `--forge`: `github`, `github:<host>`, `gitea:<host>`, `gitlab:<host>` or an https URL, for a self-hosted forge."},
	{Names: []string{"FLEETLINT_CACHE_DIR"}, Doc: "Directory for pinned catalogs (in `catalogs/` below it) instead of the user cache directory; CI sets it to keep the cache between runs."},
	{Names: []string{"NO_COLOR"}, Doc: "Any value turns colored output off unless `--color` is given."},
	{Names: []string{"GITHUB_TOKEN", "GH_TOKEN"}, Doc: "Token for GitHub (`GH_TOKEN` when `GITHUB_TOKEN` is unset): repository visibility, `fleet --from github:`, pull requests. In a Gitea or Forgejo Actions run, `GITHUB_TOKEN` is also the token for that instance when `GITEA_TOKEN` is unset."},
	{Names: []string{"GITEA_TOKEN"}, Doc: "Token for a Gitea or Forgejo host fleetlint may contact."},
	{Names: []string{"GITLAB_TOKEN"}, Doc: "Token for a GitLab host fleetlint may contact."},
	{Names: []string{"GITEA_HOST", "GITLAB_HOST"}, Doc: "Comma-separated self-hosted Gitea, Forgejo or GitLab hosts fleetlint may contact and send their token to. Not needed for gitlab.com, a host named by `--forge` or `--from`, the remote of a `--pr`, or the instance running the CI job."},
	{Names: []string{"GITHUB_API_URL"}, Doc: "Set by GitHub Actions: on GitHub Enterprise Server, the API fleetlint uses for GitHub. In Gitea and Forgejo Actions, the API of that instance."},
	{Names: []string{"GITHUB_ACTIONS"}, Doc: "Set by GitHub Actions: with `GITHUB_SERVER_URL`, the GitHub Enterprise Server running the job, which fleetlint then treats as a known GitHub host."},
	{Names: []string{"GITEA_ACTIONS", "FORGEJO_ACTIONS", "GITHUB_SERVER_URL"}, Doc: "Set by Gitea and Forgejo Actions: the instance running the job, which fleetlint then treats as a known Gitea host."},
	{Names: []string{"GITLAB_CI", "CI_SERVER_HOST", "CI_API_V4_URL"}, Doc: "Set by GitLab CI: the instance running the job and its API, which fleetlint then treats as a known GitLab host."},
	{Names: []string{"FLEETLINT_VERIFY"}, Doc: "`cosign` makes `install.sh` fail unless cosign is installed and verifies the signature of the release's `checksums.txt` (`curl … | FLEETLINT_VERIFY=cosign sh`).", InstallOnly: true},
}

// reference is what cli-reference.md documents: a fresh command tree,
// with cobra's completion command, which it adds only at execution.
func reference() *docs.CLI {
	root := newRoot(&globals{stderr: io.Discard}, new(int), io.Discard, io.Discard)
	root.InitDefaultCompletionCmd()
	return &docs.CLI{Root: root, ExitCodes: exitCodes, Environment: environment}
}
