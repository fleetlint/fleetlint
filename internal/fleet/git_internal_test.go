package fleet

import (
	"encoding/base64"
	"strings"
	"testing"
)

// A discovered entry clones with the forge token as an Authorization header
// scoped to its host, through the environment; a fleet-file entry or an ssh
// URL gets nothing.
func TestCloneAuth(t *testing.T) {
	t.Parallel()
	env := cloneAuth(Entry{URL: "https://github.com/acme/x.git", Token: "tok"})
	if len(env) != 5 || env[3] != "GIT_CONFIG_KEY_1=http.https://github.com/.extraheader" {
		t.Fatalf("env: %q", env)
	}
	if !strings.HasPrefix(env[4], "GIT_CONFIG_VALUE_1=AUTHORIZATION: basic ") || strings.Contains(env[4], "tok") {
		t.Errorf("the token is sent base64-encoded, never raw: %q", env[4])
	}
	gl := cloneAuth(Entry{URL: "https://gitlab.com/acme/x.git", Token: "tok", TokenUser: "oauth2"})
	if want := base64.StdEncoding.EncodeToString([]byte("oauth2:tok")); len(gl) != 5 || !strings.HasSuffix(gl[4], want) {
		t.Errorf("GitLab takes the token with the user oauth2: %q", gl)
	}
	for _, e := range []Entry{{URL: "https://github.com/acme/x.git"}, {URL: "git@github.com:acme/x.git", Token: "tok"}, {Path: "/tmp/x", Token: "tok"}} {
		if got := cloneAuth(e); got != nil {
			t.Errorf("%+v: no auth expected, got %q", e, got)
		}
	}
}
