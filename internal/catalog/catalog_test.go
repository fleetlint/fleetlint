package catalog_test

import (
	"strings"
	"testing"

	"github.com/fleetlint/fleetlint/internal/catalog"
	"github.com/fleetlint/fleetlint/internal/model"
)

// Every embedded preset must parse and validate; a broken preset is a broken binary.
func TestPresetsParse(t *testing.T) {
	t.Parallel()
	for _, name := range catalog.Presets() {
		cats, err := catalog.Loader{}.Load("fleetlint:" + name)
		if err != nil {
			t.Fatalf("preset %s: %v", name, err)
		}
		if len(cats) == 0 {
			t.Fatalf("preset %s: no catalogs", name)
		}
		own := cats[len(cats)-1]
		if own.Ref != "fleetlint:"+name || (len(own.Rules) == 0 && len(own.Overrides) == 0) {
			t.Fatalf("preset %s: last catalog should be the preset itself with rules or overrides, got %s", name, own.Ref)
		}
		for _, r := range own.Rules {
			if r.Fix.Agent == "" {
				t.Errorf("preset %s: rule %s has no fix.agent", name, r.ID)
			}
			if r.Rationale == "" {
				t.Errorf("preset %s: rule %s has no rationale", name, r.ID)
			}
			if r.Kind == model.KindExpr && r.Message == "" {
				t.Errorf("preset %s: rule %s (expr) has no message", name, r.ID)
			}
		}
	}
}

func TestParseRejectsBadCatalogs(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"wrong apiVersion":         "apiVersion: x\nkind: Catalog\nmetadata: {name: a, version: 1}\nrules: []\n",
		"missing version":          "apiVersion: fleetlint.org/v1\nkind: Catalog\nmetadata: {name: a}\nrules: []\n",
		"rule without ns":          "apiVersion: fleetlint.org/v1\nkind: Catalog\nmetadata: {name: a, version: 1}\nrules:\n  - {id: nons, title: t, kind: expr, severity: error, expr: 'true', requirement: r, fix: {human: h}}\n",
		"expr without expr":        "apiVersion: fleetlint.org/v1\nkind: Catalog\nmetadata: {name: a, version: 1}\nrules:\n  - {id: a/b, title: t, kind: expr, severity: error, requirement: r, fix: {human: h}}\n",
		"duplicate id":             "apiVersion: fleetlint.org/v1\nkind: Catalog\nmetadata: {name: a, version: 1}\nrules:\n  - {id: a/b, title: t, kind: expr, severity: error, expr: 'true', requirement: r, fix: {human: h}}\n  - {id: a/b, title: t, kind: expr, severity: error, expr: 'true', requirement: r, fix: {human: h}}\n",
		"unknown key":              "apiVersion: fleetlint.org/v1\nkind: Catalog\nmetadata: {name: a, version: 1}\nbogus: 1\nrules: []\n",
		"rule without severity":    "apiVersion: fleetlint.org/v1\nkind: Catalog\nmetadata: {name: a, version: 1}\nrules:\n  - {id: a/b, title: t, kind: expr, expr: 'true', requirement: r, fix: {human: h}}\n",
		"partial without severity": "apiVersion: fleetlint.org/v1\nkind: Catalog\nmetadata: {name: a, version: 1}\nrules:\n  - {id: a/b, title: t, kind: outcome, severity: error, requirement: r, fix: {human: h}, satisfiers: [{name: s, expr: 'true'}], partials: [{name: p, expr: 'true', message: m}]}\n",
		"tier out of range":        "apiVersion: fleetlint.org/v1\nkind: Catalog\nmetadata: {name: a, version: 1}\nrules:\n  - {id: a/b, title: t, kind: expr, severity: error, expr: 'true', requirement: r, tiers: [4], fix: {human: h}}\n",
	}
	for name, body := range cases {
		if _, err := catalog.Parse([]byte(body)); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestRemoteRequiresDigest(t *testing.T) {
	t.Parallel()
	l := catalog.Loader{Fetch: func(string) ([]byte, error) { return []byte("x"), nil }}
	if _, err := l.Load("https://example.com/c.yaml"); err == nil || !strings.Contains(err.Error(), "pinned") {
		t.Fatalf("expected digest-required error, got %v", err)
	}
	if _, err := l.Load("http://example.com/c.yaml#sha256-00"); err == nil {
		t.Fatal("plain http must be rejected")
	}
}

func TestRemoteDigestVerified(t *testing.T) {
	t.Parallel()
	body := []byte("apiVersion: fleetlint.org/v1\nkind: Catalog\nmetadata: {name: remote, version: 1.0.0}\nrules: []\n")
	l := catalog.Loader{Fetch: func(string) ([]byte, error) { return body, nil }}
	good := "https://example.com/c.yaml#sha256-" + catalog.Digest(body)
	cats, err := l.Load(good)
	if err != nil || len(cats) != 1 || cats[0].Trusted {
		t.Fatalf("good digest: cats=%v err=%v", cats, err)
	}
	if _, err := l.Load("https://example.com/c.yaml#sha256-" + strings.Repeat("0", 64)); err == nil {
		t.Fatal("wrong digest must be rejected")
	}
}

func TestEngineLevel(t *testing.T) {
	t.Parallel()
	body := func(engine string) []byte {
		return []byte("apiVersion: fleetlint.org/v1\nkind: Catalog\nmetadata: {name: a, version: 1.0.0" + engine + "}\nrules: []\n")
	}
	for _, ok := range []string{"", ", engine: 1"} {
		if _, err := catalog.Parse(body(ok)); err != nil {
			t.Errorf("engine %q must load: %v", ok, err)
		}
	}
	if _, err := catalog.Parse(body(", engine: 99")); err == nil || !strings.Contains(err.Error(), "upgrade fleetlint") {
		t.Errorf("a catalog from the future must say what to do: %v", err)
	}
	// Every built-in preset states its level, so a mismatch is caught for them too.
	for _, name := range catalog.Presets() {
		cats, err := catalog.Loader{}.Load("fleetlint:" + name)
		if err != nil {
			t.Fatal(err)
		}
		if got := cats[len(cats)-1].Metadata.Engine; got != catalog.EngineLevel {
			t.Errorf("preset %s states engine level %d, this binary is %d", name, got, catalog.EngineLevel)
		}
	}
}
