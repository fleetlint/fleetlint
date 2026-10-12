// Package proc starts child processes the same way everywhere: on timeout
// the whole process group is killed and waiting for its output is bounded,
// so a background process left behind cannot hang a run; repository code
// gets an environment without credentials, and git one without variables
// that redirect it to another repository.
package proc

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"time"
)

// waitDelay bounds how long Wait waits for output pipes after the process
// was killed or exited, when a grandchild still holds them.
const waitDelay = 5 * time.Second

// Command is exec.CommandContext that kills the process group on cancel
// and bounds the wait for its pipes.
func Command(ctx context.Context, name string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, name, args...) //nolint:gosec // callers pass a fixed binary or a validated path, never through a shell
	cmd.WaitDelay = waitDelay
	ownGroup(cmd)
	return cmd
}

// keep are variables repository code may see: what a shell and the common
// toolchains need to run, never credentials.
var keep = map[string]bool{
	"PATH": true, "HOME": true, "USER": true, "LOGNAME": true, "SHELL": true, "LANG": true, "LC_ALL": true,
	"TMPDIR": true, "TERM": true, "CI": true, "JAVA_HOME": true, "VIRTUAL_ENV": true,
	"SYSTEMROOT": true, "SYSTEMDRIVE": true, "USERPROFILE": true, "APPDATA": true, "LOCALAPPDATA": true,
	"PATHEXT": true, "COMSPEC": true, "TEMP": true, "TMP": true, "WINDIR": true, "PROGRAMFILES": true,
}

var keepPrefixes = []string{"GO", "CARGO_", "RUSTUP_", "XDG_", "UV_", "PYTHON", "GRADLE_", "NVM_", "LC_"}

// secretish names are dropped even under a kept prefix.
var secretish = []string{"TOKEN", "SECRET", "PASSWORD", "PASSWD", "AUTH", "CREDENTIAL", "_KEY", "PRIVATE"}

// SafeEnv is the environment for repository code: the variables a build
// needs, without tokens, cloud credentials or CI secrets, plus extra.
func SafeEnv(extra ...string) []string {
	var env []string
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		if allowed(strings.ToUpper(name)) {
			env = append(env, kv)
		}
	}
	return append(env, extra...)
}

func allowed(name string) bool {
	for _, s := range secretish {
		if strings.Contains(name, s) {
			return false
		}
	}
	if keep[name] {
		return true
	}
	for _, p := range keepPrefixes {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}

// redirecting are git variables that point a command at another
// repository, work tree or index; git hooks export them, so a fleetlint
// started from a hook would otherwise act on the hook's repository.
var redirecting = []string{
	"GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_OBJECT_DIRECTORY", "GIT_ALTERNATE_OBJECT_DIRECTORIES",
	"GIT_COMMON_DIR", "GIT_NAMESPACE", "GIT_PREFIX", "GIT_CEILING_DIRECTORIES", "GIT_DISCOVERY_ACROSS_FILESYSTEM",
	"GIT_CONFIG", "GIT_CONFIG_PARAMETERS", "GIT_CONFIG_COUNT", "GIT_QUARANTINE_PATH",
}

// GitEnv is the caller's environment without variables that redirect git,
// with prompts off, plus extra (credentials go there, never in arguments).
func GitEnv(extra ...string) []string {
	env := make([]string, 0, len(os.Environ())+len(extra)+1)
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		if !redirects(name) {
			env = append(env, kv)
		}
	}
	env = append(env, "GIT_TERMINAL_PROMPT=0")
	return append(env, extra...)
}

func redirects(name string) bool {
	for _, r := range redirecting {
		if name == r {
			return true
		}
	}
	return strings.HasPrefix(name, "GIT_CONFIG_KEY_") || strings.HasPrefix(name, "GIT_CONFIG_VALUE_")
}
