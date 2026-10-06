package engine_test

import (
	"context"
	"os/exec"
	"strings"
	"testing"

	"github.com/fleetlint/fleetlint/internal/catalog"
	"github.com/fleetlint/fleetlint/internal/config"
	"github.com/fleetlint/fleetlint/internal/engine"
	"github.com/fleetlint/fleetlint/internal/model"
	"github.com/fleetlint/fleetlint/internal/repo"
	"github.com/fleetlint/fleetlint/internal/testutil"
)

// Every rule added for 1.0 gets a passing and a failing fixture. The config
// pins tier 1 and public so tier/visibility guards do not hide a broken
// expression behind "not applicable".
const tier1Public = "version: 1\nextends: [fleetlint:recommended]\nfacts: {tier: 1, visibility: public}\n"

const releaseWF = ".github/workflows/release.yml"

func wf(trigger, jobs string) string { return "on:\n" + trigger + "jobs:\n" + jobs }

var tagPush = "  push:\n    tags: ['v*']\n"

func step(uses, run string) string {
	s := "      - "
	if uses != "" {
		s += "uses: " + uses + "\n"
		if run != "" {
			s += "        run: " + run + "\n"
		}
		return s
	}
	return s + "run: " + run + "\n"
}

func job(name, body string) string { return "  " + name + ":\n    steps:\n" + body }

type ruleCase struct {
	pass, fail  map[string]string
	wantMessage string // substring of the first finding when failing
}

func TestOneDotZeroRules(t *testing.T) {
	t.Parallel()
	goreleaser := func(extra string) string { return "builds: [{main: .}]\n" + extra }
	cases := map[string]ruleCase{
		"repo/codeowners": {
			pass: map[string]string{".github/CODEOWNERS": "* @me\n"},
			fail: map[string]string{},
		},
		"lint/go-linters-enabled": {
			pass:        map[string]string{"go.mod": "module x\n", ".golangci.yml": "version: \"2\"\nlinters:\n  enable: [errcheck, govet, staticcheck, errorlint, gosec, gocognit, funlen]\n"},
			fail:        map[string]string{"go.mod": "module x\n", ".golangci.yml": "version: \"2\"\nlinters:\n  default: standard\n"},
			wantMessage: "required linter is not enabled (errorlint)",
		},
		"quality/no-slop-markers": {
			pass:        map[string]string{"README.md": "# x\n\nParses go.mod.\n", "CHANGELOG.md": "A robust release\n"},
			fail:        map[string]string{"README.md": "# x\n\nA robust, blazing-fast tool.\n"},
			wantMessage: "marketing filler",
		},
		"quality/coverage-threshold": {
			pass: map[string]string{"Makefile": "cover:\n\tdiff-cover coverage.xml --fail-under=80\n"},
			fail: map[string]string{"Makefile": "cover:\n\tgo test -cover ./...\n"},
		},
		"docs/architecture": {
			pass: map[string]string{"README.md": "# x\n\n## Architecture\n\nboxes\n"},
			fail: map[string]string{"README.md": "# x\n"},
		},
		"docs/adr-directory": {
			pass: map[string]string{"docs/adr/0001-record.md": "# 1\n"},
			fail: map[string]string{},
		},
		"deps/update-automation": {
			pass: map[string]string{"renovate.json": "{}"},
			fail: map[string]string{},
		},
		"ci/job-timeouts": {
			pass: map[string]string{".github/workflows/ci.yml": wf("  push:\n", "  a:\n    timeout-minutes: 10\n    steps:\n"+step("", "true")+"  b:\n    uses: org/repo/.github/workflows/x.yml@main\n")},
			fail: map[string]string{".github/workflows/ci.yml": wf("  push:\n", job("a", step("", "true")))},
		},
		"ci/concurrency-cancel": {
			pass: map[string]string{".github/workflows/pr.yml": "on: [pull_request]\nconcurrency: {group: x, cancel-in-progress: true}\n" + "jobs:\n" + job("a", step("", "true"))},
			fail: map[string]string{".github/workflows/pr.yml": wf("  pull_request:\n", job("a", step("", "true")))},
		},
		"ci/no-pull-request-target": {
			pass:        map[string]string{".github/workflows/label.yml": wf("  pull_request_target:\n", job("a", step("actions/labeler@v5", "")))},
			fail:        map[string]string{".github/workflows/bad.yml": wf("  pull_request_target:\n", job("a", "      - uses: actions/checkout@v4\n        with: {ref: '${{ github.event.pull_request.head.sha }}'}\n"))},
			wantMessage: "checks out the pull request head",
		},
		"release/signing": {
			pass: map[string]string{".goreleaser.yaml": goreleaser("signs: [{cmd: cosign}]\n"), releaseWF: wf(tagPush, job("r", step("goreleaser/goreleaser-action@v6", "")))},
			fail: map[string]string{".goreleaser.yaml": goreleaser(""), releaseWF: wf(tagPush, job("r", step("goreleaser/goreleaser-action@v6", "")))},
		},
		"release/provenance": {
			pass: map[string]string{".goreleaser.yaml": goreleaser(""), releaseWF: wf(tagPush, job("r", step("actions/attest-build-provenance@v2", "")))},
			fail: map[string]string{".goreleaser.yaml": goreleaser(""), releaseWF: wf(tagPush, job("r", step("", "make dist")))},
		},
		"release/tag-triggered": {
			pass: map[string]string{".goreleaser.yaml": goreleaser(""), releaseWF: wf(tagPush, job("r", step("", "goreleaser release")))},
			fail: map[string]string{".goreleaser.yaml": goreleaser("")},
		},
		"release/checksums": {
			pass: map[string]string{".goreleaser.yaml": goreleaser(""), releaseWF: wf(tagPush, job("r", step("", "goreleaser release")))},
			fail: map[string]string{".goreleaser.yaml": goreleaser("checksum: {disable: true}\n"), releaseWF: wf(tagPush, job("r", step("", "goreleaser release")))},
		},
		"release/changelog-in-release": {
			pass: map[string]string{"cliff.toml": "[changelog]\n", releaseWF: wf(tagPush, job("r", step("", "git-cliff --latest")))},
			fail: map[string]string{"cliff.toml": "[changelog]\n", releaseWF: wf(tagPush, job("r", step("", "gh release create")))},
		},
		"public/contributing": {
			pass: map[string]string{"CONTRIBUTING.md": "run make check\n"},
			fail: map[string]string{},
		},
		"public/code-of-conduct": {
			pass: map[string]string{"CODE_OF_CONDUCT.md": "be kind\n"},
			fail: map[string]string{},
		},
		"repo/toolchain-pinned": {
			pass:        map[string]string{"go.mod": "module x\n\ngo 1.24.0\n", "package.json": "{}", ".nvmrc": "22\n"},
			fail:        map[string]string{"go.mod": "module x\n\ngo 1.24\n", "package.json": "{}", ".nvmrc": "22\n"},
			wantMessage: "not pinned",
		},
		"deps/vulnerability-scan": {
			pass: map[string]string{"Makefile": "audit:\n\tgovulncheck ./...\n"},
			fail: map[string]string{"Makefile": "audit:\n\tgitleaks git .\n"},
		},
		"deps/license-check": {
			pass: map[string]string{".github/workflows/ci.yml": wf("  push:\n", job("a", step("", "go-licenses check ./...")))},
			fail: map[string]string{"Makefile": "audit:\n\tgovulncheck ./...\n"},
		},
		"deps/install-scripts-disabled": {
			pass: map[string]string{"package.json": "{}", ".npmrc": "ignore-scripts=true\n"},
			fail: map[string]string{"package.json": "{}", ".npmrc": "save-exact=true\n"},
		},
		"lint/shell": {
			pass: map[string]string{"scripts/run.sh": "#!/bin/sh\n", ".pre-commit-config.yaml": "repos:\n  - repo: https://github.com/shellcheck-py/shellcheck-py\n    rev: v0.10.0.1\n    hooks: [{id: shellcheck}]\n"},
			fail: map[string]string{"scripts/run.sh": "#!/bin/sh\n"},
		},
		"containers/dockerignore": {
			pass: map[string]string{"Dockerfile": "FROM scratch\n", ".dockerignore": ".git\n"},
			fail: map[string]string{"deploy/Dockerfile.api": "FROM scratch\n"},
		},
		"containers/non-root-user": {
			pass:        map[string]string{"Dockerfile": "FROM alpine:3\nUSER root\nRUN true\nUSER app\n", "b/Dockerfile": "FROM gcr.io/distroless/static:nonroot\n", ".dockerignore": ".git\n"},
			fail:        map[string]string{"Dockerfile": "FROM alpine:3\nUSER 0\n"},
			wantMessage: "runs as root",
		},
		"containers/base-image-pinned": {
			pass:        map[string]string{"Dockerfile": "ARG BASE=x\nFROM golang:1.24@sha256:abc AS build\nFROM $BASE AS mid\nFROM --platform=linux/amd64 build AS again\nFROM scratch\n"},
			fail:        map[string]string{"Dockerfile": "FROM golang:1.24@sha256:abc AS build\nFROM alpine:3.20\n"},
			wantMessage: "without a digest",
		},
		"public/manifest-license": {
			pass: map[string]string{"package.json": "{\"license\": \"MIT\"}", "Cargo.toml": "[package]\nname = \"x\"\nlicense = \"MIT\"\n"},
			fail: map[string]string{"package.json": "{\"license\": \"MIT\"}", "Cargo.toml": "[package]\nname = \"x\"\n"},
		},
		"lint/python-rules-enabled": {
			pass:        map[string]string{"pyproject.toml": "[tool.ruff.lint]\nselect = [\"ALL\"]\n"},
			fail:        map[string]string{"pyproject.toml": "[tool.ruff.lint]\nselect = [\"E\", \"F\", \"I\", \"B\", \"UP\", \"SIM\", \"BLE\", \"S\", \"C90\"]\n"},
			wantMessage: "(T20)",
		},
		"lint/node-strict-flags": {
			pass:        map[string]string{"package.json": "{}", "tsconfig.json": `{"compilerOptions": {"strict": true, "noUncheckedIndexedAccess": true, "exactOptionalPropertyTypes": true, "noImplicitOverride": true, "noFallthroughCasesInSwitch": true}}`},
			fail:        map[string]string{"package.json": "{}", "tsconfig.json": `{"compilerOptions": {"strict": true, "noUncheckedIndexedAccess": false, "exactOptionalPropertyTypes": true, "noImplicitOverride": true, "noFallthroughCasesInSwitch": true}}`},
			wantMessage: "(noUncheckedIndexedAccess)",
		},
		"lint/rust-lints-denied": {
			pass:        map[string]string{"Cargo.toml": "[package]\nname = \"x\"\n[lints.clippy]\nunwrap_used = \"deny\"\nexpect_used = \"warn\"\npanic = \"deny\"\ntodo = \"deny\"\ndbg_macro = { level = \"deny\" }\n"},
			fail:        map[string]string{"Cargo.toml": "[package]\nname = \"x\"\n[lints.clippy]\nunwrap_used = \"deny\"\nexpect_used = \"warn\"\npanic = \"allow\"\ntodo = \"deny\"\ndbg_macro = \"deny\"\n"},
			wantMessage: "(panic)",
		},
		"lint/flutter-strict-modes": {
			pass:        map[string]string{"pubspec.yaml": "name: x\n", "analysis_options.yaml": "analyzer:\n  language:\n    strict-casts: true\n    strict-inference: true\n    strict-raw-types: true\n"},
			fail:        map[string]string{"pubspec.yaml": "name: x\n", "analysis_options.yaml": "analyzer:\n  language:\n    strict-casts: true\n"},
			wantMessage: "(strict-inference)",
		},
		"repo/dev-environment": {
			pass: map[string]string{"Makefile": "tools:\n\tgo install example.com/lint@v1.2.3\n"},
			fail: map[string]string{"Makefile": "tools:\n\t@echo install the tools\n"},
		},
		"taskrunner/targets": {
			pass:        map[string]string{"justfile": "fmt:\n    gofmt -w .\nlint:\n    go vet ./...\ntest:\n    go test ./...\ncover:\n    go test -cover ./...\naudit:\n    govulncheck ./...\ncheck: lint test\ncheck-fast: lint\n"},
			fail:        map[string]string{"Makefile": "fmt:\n\tgofmt -w .\nlint:\n\t@echo \"TODO: linter\"\ntest:\n\tgo test ./...\ncover:\n\tgo test -cover ./...\naudit:\n\tgovulncheck ./...\ncheck: lint test\n"},
			wantMessage: "(lint)",
		},
		"taskrunner/container": {
			pass: map[string]string{".devcontainer/devcontainer.json": "{}", "Taskfile.yml": "version: '3'\ntasks:\n  lint:\n    cmds: ['podman compose exec dev golangci-lint run']\n"},
			fail: map[string]string{".devcontainer/devcontainer.json": "{}", "Makefile": "lint:\n\tgolangci-lint run\n"},
		},
		"quality/policy-enforced": {
			pass: map[string]string{"Makefile": "check:\n\tfleetlint check --fail-on error\n"},
			fail: map[string]string{"Makefile": "check:\n\tgo test ./...\n"},
		},
		"public/issue-templates": {
			pass: map[string]string{".github/ISSUE_TEMPLATE/bug.md": "steps\n"},
			fail: map[string]string{},
		},
		"lint/hugo-strict-build": {
			pass: map[string]string{"hugo.toml": "baseURL = 'https://example.org/'\n", "Makefile": "lint:\n\thugo --quiet --panicOnWarning --destination /tmp/site\n"},
			fail: map[string]string{"hugo.toml": "baseURL = 'https://example.org/'\n", "Makefile": "build:\n\thugo --minify\n"},
		},
		"lint/kotlin-detekt-strict": {
			pass:        map[string]string{"build.gradle.kts": "plugins {}\n", "config/detekt/detekt.yml": "exceptions:\n  SwallowedException:\n    active: true\n  TooGenericExceptionCaught:\n    active: true\nempty-blocks:\n  EmptyCatchBlock:\n    active: true\ncomplexity:\n  LongMethod:\n    active: true\n    threshold: 50\n  CognitiveComplexMethod:\n    active: true\n"},
			fail:        map[string]string{"build.gradle.kts": "plugins {}\n", "config/detekt/detekt.yml": "exceptions:\n  SwallowedException:\n    active: false\n  TooGenericExceptionCaught:\n    active: true\nempty-blocks:\n  EmptyCatchBlock:\n    active: true\ncomplexity:\n  LongMethod:\n    active: true\n"},
			wantMessage: "(exceptions.SwallowedException)",
		},
		"quality/duplication-gate": {
			pass: map[string]string{".golangci.yml": "version: \"2\"\nlinters:\n  enable:\n    - dupl\n"},
			fail: map[string]string{".golangci.yml": "version: \"2\"\nlinters:\n  enable:\n    - errcheck\n", "Makefile": "lint:\n\tgolangci-lint run\n"},
		},
		"quality/mutation-gate": {
			pass: map[string]string{"Makefile": "mutate:\n\tgremlins unleash ./internal/...\n"},
			fail: map[string]string{"Makefile": "test:\n\tgo test ./...\n"},
		},
		"release/api-compatibility": {
			pass: map[string]string{"pkg/lib.go": "package pkg\n", ".github/workflows/release.yml": wf(tagPush, job("a", step("", "gorelease -base=$(git describe --tags --abbrev=0 HEAD^)")))},
			fail: map[string]string{"pkg/lib.go": "package pkg\n", "Makefile": "release:\n\tgoreleaser release\n"},
		},
		"deps/forks-documented": {
			pass: map[string]string{"go.mod": "module x\n\nreplace example.com/dep => github.com/me/dep v1.2.3 // upstream #42: nil deref in parser, drop at v1.3\n"},
			fail: map[string]string{"go.mod": "module x\n\nreplace example.com/dep => github.com/me/dep v1.2.3\n"},
		},
	}
	for id, tc := range cases {
		t.Run(id, func(t *testing.T) {
			t.Parallel()
			pass := run(t, withDefaults(tc.pass), tier1Public)
			if s := status(t, pass, id); s.Status != model.StatusPass {
				t.Errorf("pass fixture: %s %q %+v", s.Status, s.Err, s.Findings)
			}
			fail := run(t, withDefaults(tc.fail), tier1Public)
			s := status(t, fail, id)
			if s.Status != model.StatusFail {
				t.Fatalf("fail fixture: %s %q evidence=%q", s.Status, s.Err, s.Evidence)
			}
			if tc.wantMessage != "" && !strings.Contains(s.Findings[0].Message, tc.wantMessage) {
				t.Errorf("message %q lacks %q", s.Findings[0].Message, tc.wantMessage)
			}
		})
	}
}

// withDefaults adds a manifest so the fixture has a stack, without
// disturbing files the case defines.
func withDefaults(files map[string]string) map[string]string {
	out := map[string]string{"go.mod": "module x\n"}
	for k, v := range files {
		out[k] = v
	}
	return out
}

// A release that is a container image: build-push-action without options
// must fail the SBOM and provenance rules cleanly (not error on the missing
// keys), pass them once the options are set, and needs no checksums file.
func TestContainerImageReleases(t *testing.T) {
	t.Parallel()
	build := func(with string) map[string]string {
		return map[string]string{releaseWF: wf(tagPush, job("image", "      - uses: docker/build-push-action@v6\n        with: {push: true"+with+"}\n"))}
	}
	bare := run(t, withDefaults(build("")), tier1Public)
	for id, want := range map[string]model.Status{"release/sbom": model.StatusFail, "release/provenance": model.StatusFail, "release/checksums": model.StatusPass} {
		if s := status(t, bare, id); s.Status != want {
			t.Errorf("without options, %s: %s %q, want %s", id, s.Status, s.Err, want)
		}
	}
	attested := run(t, withDefaults(build(", sbom: true, provenance: mode=max")), tier1Public)
	for _, id := range []string{"release/sbom", "release/provenance", "release/checksums"} {
		if s := status(t, attested, id); s.Status != model.StatusPass {
			t.Errorf("with sbom and provenance, %s: %s %q %+v", id, s.Status, s.Err, s.Findings)
		}
	}
	off := run(t, withDefaults(build(", provenance: false")), tier1Public)
	if s := status(t, off, "release/provenance"); s.Status != model.StatusFail {
		t.Errorf("provenance: false must not satisfy the rule: %s", s.Status)
	}
}

func TestNoLargeFilesRespectsLFS(t *testing.T) {
	t.Parallel()
	big := strings.Repeat("x", 3000)
	cfg := "version: 1\nextends: [fleetlint:recommended]\nfacts: {tier: 2}\noverrides:\n  repo/no-large-files: {params: {max_kb: 2}}\n"
	out := run(t, map[string]string{"go.mod": "module x\n", "model.bin": big, "fine.txt": "x"}, cfg)
	s := status(t, out, "repo/no-large-files")
	if s.Status != model.StatusFail || len(s.Findings) != 1 || s.Findings[0].Path != "model.bin" {
		t.Fatalf("large tracked file must be flagged by path: %+v", s)
	}
	out = run(t, map[string]string{"go.mod": "module x\n", "model.bin": big, ".gitattributes": "*.bin filter=lfs diff=lfs merge=lfs -text\n"}, cfg)
	if s := status(t, out, "repo/no-large-files"); s.Status != model.StatusPass {
		t.Fatalf("lfs-routed pattern must pass: %+v", s)
	}
}

func TestSemverTags(t *testing.T) {
	t.Parallel()
	r := testutil.GitFixture(t, map[string]string{"go.mod": "module x\n", ".fleetlint.yaml": tier1Public})
	for _, tag := range []string{"v1.0.0", "api/v2.3.4-rc.1", "1.2.3", "release-2", "v1.0"} {
		tagIt(t, r.Root, tag)
	}
	out := evaluate(t, r)
	s := status(t, out, "release/semver-tags")
	if s.Status != model.StatusFail || len(s.Findings) != 2 {
		t.Fatalf("exactly release-2 and v1.0 should fail: %+v", s.Findings)
	}
	for _, f := range s.Findings {
		if !strings.Contains(f.Message, "release-2") && !strings.Contains(f.Message, "v1.0)") {
			t.Errorf("unexpected finding %q", f.Message)
		}
	}
}

// A tools target pinned through *_VERSION variables counts only when the
// variables hold versions or commits; a branch name is not a pin.
func TestDevEnvironmentVersionVariables(t *testing.T) {
	t.Parallel()
	mk := func(v string) map[string]string {
		return map[string]string{"go.mod": "module x\n", "Makefile": "GOLANGCI_VERSION ?= " + v + "\ntools:\n\tgo install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_VERSION)\n"}
	}
	for _, v := range []string{"v2.14.0", "2.14.0-rc.1", "04dfe7ad9a81d8d7380ddd207e472c83948d800c   # the commit the pages come from", "\"1.5.2\""} {
		if s := status(t, run(t, mk(v), tier1Public), "repo/dev-environment"); s.Status != model.StatusPass {
			t.Errorf("%q is a pin: %s %+v", v, s.Status, s.Findings)
		}
	}
	for _, v := range []string{"main", "latest", "release-2.x"} {
		s := status(t, run(t, mk(v), tier1Public), "repo/dev-environment")
		if s.Status != model.StatusFail || len(s.Findings) == 0 || !strings.Contains(s.Findings[0].Message, "not a version") {
			t.Errorf("%q is not a pin: %s %+v", v, s.Status, s.Findings)
		}
	}
}

// A Go module with a main package is an application: the API rule does not
// apply. A library without the check fails; one with a documented fork via
// docs/FORKS.md passes the forks rule however the replace line looks.
func TestLibraryHeuristicAndForksDoc(t *testing.T) {
	t.Parallel()
	app := run(t, map[string]string{"go.mod": "module x\n", "cmd/x/main.go": "package main\n"}, tier1Public)
	if s := status(t, app, "release/api-compatibility"); s.Status != model.StatusNotApplicable {
		t.Errorf("an application is not held to API compatibility: %s %+v", s.Status, s.Findings)
	}
	forks := run(t, map[string]string{"go.mod": "module x\n\nreplace example.com/dep => ../dep\n", "docs/FORKS.md": "# Forks\n\n- dep: local patch\n"}, tier1Public)
	if s := status(t, forks, "deps/forks-documented"); s.Status != model.StatusPass {
		t.Errorf("docs/FORKS.md documents the fork: %s %+v", s.Status, s.Findings)
	}
	none := run(t, map[string]string{"go.mod": "module x\n"}, tier1Public)
	if s := status(t, none, "deps/forks-documented"); s.Status != model.StatusNotApplicable {
		t.Errorf("no forks, nothing to document: %s", s.Status)
	}
}

// Only the newest params.recent version tags are checked, lightweight tags
// fail, fabricated signature blocks pass, non-version tags are ignored.
func TestSignedTags(t *testing.T) {
	t.Parallel()
	r := testutil.GitFixture(t, map[string]string{"go.mod": "module x\n", ".fleetlint.yaml": tier1Public})
	tagIt(t, r.Root, "v0.1.0")
	tagIt(t, r.Root, "release-2")
	signedTag(t, r.Root, "v0.2.0")
	signedTag(t, r.Root, "v0.3.0")
	tagIt(t, r.Root, "v0.4.0")
	s := status(t, evaluate(t, r), "release/signed-tags")
	if s.Status != model.StatusFail || len(s.Findings) != 1 || !strings.Contains(s.Findings[0].Message, "v0.4.0") {
		t.Fatalf("v0.4.0 alone is unsigned among the newest three: %s %+v", s.Status, s.Findings)
	}
	untagged := testutil.GitFixture(t, map[string]string{"go.mod": "module x\n", ".fleetlint.yaml": tier1Public})
	if s := status(t, evaluate(t, untagged), "release/signed-tags"); s.Status != model.StatusNotApplicable {
		t.Errorf("without version tags the rule does not apply: %s", s.Status)
	}
}

// signedTag writes an annotated tag carrying an SSH signature block, as
// `git tag -s` would; git stores it without checking.
func signedTag(t *testing.T, dir, name string) {
	t.Helper()
	head := exec.CommandContext(context.Background(), "git", "-C", dir, "rev-parse", "HEAD")
	head.Env = testutil.GitEnv()
	sha, err := head.Output()
	if err != nil {
		t.Fatal(err)
	}
	mk := exec.CommandContext(context.Background(), "git", "-C", dir, "mktag")
	mk.Env = testutil.GitEnv()
	mk.Stdin = strings.NewReader("object " + strings.TrimSpace(string(sha)) + "\ntype commit\ntag " + name + "\ntagger t <t@t> 0 +0000\n\n" + name +
		"\n-----BEGIN SSH SIGNATURE-----\nU1NIU0lHAAAAAQ==\n-----END SSH SIGNATURE-----\n")
	obj, err := mk.Output()
	if err != nil {
		t.Fatalf("mktag: %v", err)
	}
	gitCmd(t, dir, "update-ref", "refs/tags/"+name, strings.TrimSpace(string(obj)))
}

func TestNoAIAttribution(t *testing.T) {
	t.Parallel()
	r := testutil.GitFixture(t, map[string]string{"go.mod": "module x\n", ".fleetlint.yaml": "version: 1\nextends: [fleetlint:minimal]\nfacts: {visibility: public}\n"})
	if s := status(t, evaluate(t, r), "public/no-ai-attribution"); s.Status != model.StatusPass {
		t.Fatalf("clean history must pass: %+v", s)
	}
	commit(t, r.Root, "feat: a\n\nCo-Authored-By: Claude <noreply@anthropic.com>")
	commit(t, r.Root, "fix: b\n\n\U0001F916 Generated with [Claude Code](https://claude.com/claude-code)")
	commit(t, r.Root, "docs: c")
	s := status(t, evaluate(t, r), "public/no-ai-attribution")
	if s.Status != model.StatusFail || len(s.Findings) != 2 {
		t.Fatalf("two attributed commits expected: %+v", s.Findings)
	}
	if !strings.Contains(s.Findings[0].Message, "fix: b") || !strings.Contains(s.Findings[1].Message, "feat: a") {
		t.Errorf("findings should name the commit subjects: %+v", s.Findings)
	}
	private := testutil.GitFixture(t, map[string]string{"go.mod": "module x\n"})
	commit(t, private.Root, "feat: a\n\nCo-Authored-By: Claude <noreply@anthropic.com>")
	if s := status(t, evaluate(t, private), "public/no-ai-attribution"); s.Status != model.StatusNotApplicable {
		t.Fatalf("private repositories are not checked: %+v", s)
	}
}

func evaluate(t *testing.T, r *repo.Repo) *engine.Run {
	t.Helper()
	eff, err := config.Load(r.Root, config.Options{Loader: catalog.Loader{}})
	if err != nil {
		t.Fatal(err)
	}
	out, err := engine.Evaluate(context.Background(), r, eff)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func tagIt(t *testing.T, dir, tag string) {
	t.Helper()
	gitCmd(t, dir, "tag", tag)
}

func commit(t *testing.T, dir, msg string) {
	t.Helper()
	gitCmd(t, dir, "commit", "--allow-empty", "-q", "-m", msg)
}

func gitCmd(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.CommandContext(context.Background(), "git", append([]string{"-C", dir}, args...)...)
	cmd.Env = testutil.GitEnv()
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}
