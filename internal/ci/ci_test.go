package ci_test

import (
	"reflect"
	"testing"

	"github.com/fleetlint/fleetlint/internal/ci"
	"github.com/fleetlint/fleetlint/internal/testutil"
)

const pipeline = `include:
  - local: .gitlab/ci/*.yml
  - project: group/templates
    ref: main
  - component: gitlab.com/components/sast/sast@1.2.3
  - remote: https://example.org/ci.yml
stages: [test, release]
default:
  before_script: [make tools]
.base:
  timeout: 1h 30m
  interruptible: true
test:
  extends: .base
  stage: test
  script:
    - make check
  rules:
    - if: $CI_PIPELINE_SOURCE == "merge_request_event"
release:
  stage: release
  script: [make dist]
  rules:
    - if: $CI_COMMIT_TAG =~ /^v\d+/
`

func TestGitLabPipeline(t *testing.T) {
	t.Parallel()
	r := testutil.Fixture(t, map[string]string{
		".gitlab-ci.yml":          pipeline,
		".gitlab/ci/lint.yml":     "lint:\n  script: [make lint]\n",
		".github/workflows/x.yml": "on: [push]\njobs:\n  a:\n    timeout-minutes: 5\n    steps:\n      - run: make check\n",
	})
	if got := ci.Files(r); !reflect.DeepEqual(got, []string{".github/workflows/x.yml", ".gitlab-ci.yml", ".gitlab/ci/lint.yml"}) {
		t.Fatalf("files: %v", got)
	}
	if ci.Kind(".gitlab-ci.yml") != ci.GitLab || ci.Kind(".github/workflows/x.yml") != ci.GitHub {
		t.Fatal("kinds")
	}
	jobs := ci.Jobs(r, ".gitlab-ci.yml")
	if len(jobs) != 2 {
		t.Fatalf("hidden templates are not jobs: %v", jobs)
	}
	test := jobs[1].(map[string]any)
	if test["name"] != "test" || test["timeout_minutes"] != int64(90) || test["interruptible"] != true {
		t.Fatalf("extends resolves timeout and interruptible: %v", test)
	}
	steps := ci.Steps(r, ".gitlab-ci.yml")
	if run := steps[1].(map[string]any)["run"]; run != "make tools\nmake check" {
		t.Fatalf("a job is one step with default before_script: %q", run)
	}
	if got := ci.Triggers(r, ".gitlab-ci.yml"); !reflect.DeepEqual(got, []string{"push", "pull_request"}) {
		t.Fatalf("triggers: %v", got)
	}
	if got := ci.TagPatterns(r, ".gitlab-ci.yml"); !reflect.DeepEqual(got, []string{`^v\d+`}) {
		t.Fatalf("tag patterns: %v", got)
	}
	inc := ci.Includes(r, ".gitlab-ci.yml")
	if len(inc) != 4 || inc[1]["ref"] != "main" || inc[2]["target"] != "gitlab.com/components/sast/sast" || inc[2]["ref"] != "1.2.3" || inc[3]["type"] != "remote" {
		t.Fatalf("includes: %v", inc)
	}
	if got := ci.Jobs(r, ".github/workflows/x.yml"); got[0].(map[string]any)["timeout_minutes"] != int64(5) {
		t.Fatalf("actions jobs keep their shape: %v", got)
	}
	if got := ci.TagPatterns(r, ".gitlab/ci/lint.yml"); len(got) != 0 {
		t.Fatalf("a pipeline without tag rules has no tag patterns: %v", got)
	}
}
