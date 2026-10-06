package fleet

import (
	"strings"
	"testing"
)

// A discovered entry clones with the forge token as an Authorization header
// scoped to its host, through the environment; a fleet-file entry or an ssh
// URL gets nothing.
func TestCloneAuth(t *testing.T) {
	t.Parallel()
	env := cloneAuth(Entry{URL: "https://github.com/acme/x.git", Token: "tok"})
	if len(env) != 3 || env[1] != "GIT_CONFIG_KEY_0=http.https://github.com/.extraheader" {
		t.Fatalf("env: %q", env)
	}
	if !strings.HasPrefix(env[2], "GIT_CONFIG_VALUE_0=AUTHORIZATION: basic ") || strings.Contains(env[2], "tok") {
		t.Errorf("the token is sent base64-encoded, never raw: %q", env[2])
	}
	for _, e := range []Entry{{URL: "https://github.com/acme/x.git"}, {URL: "git@github.com:acme/x.git", Token: "tok"}, {Path: "/tmp/x", Token: "tok"}} {
		if got := cloneAuth(e); got != nil {
			t.Errorf("%+v: no auth expected, got %q", e, got)
		}
	}
}
