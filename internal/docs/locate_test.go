package docs

import "testing"

// A computed locate reads as words; a shape the generator does not know is
// left to the caller, which prints it as CEL.
func TestLocateText(t *testing.T) {
	t.Parallel()
	params := map[string]any{"renovate": []any{"renovate.json", ".github/renovate.json"}}
	for expr, want := range map[string]string{
		`"README.md"`:     "`README.md`",
		`taskrunner.file`: "the task runner's file",
		`taskrunner.file != "" ? taskrunner.file : "Makefile"`:                                            "the task runner's file, or `Makefile` when there is none",
		`artifact.workflows.size() > 0 ? artifact.workflows[0] : ".github/workflows/release.yml"`:         "the first workflow that publishes the artifact, or `.github/workflows/release.yml` when there is none",
		`tagged_workflows().size() > 0 ? tagged_workflows()[0] : ".github/workflows/release.yml"`:         "the first workflow a tag push starts, or `.github/workflows/release.yml` when there is none",
		`file("vcpkg.json") ? "vcpkg.json" : "conan.lock"`:                                                "`vcpkg.json`, or `conan.lock` when there is none",
		`(["pom.xml", "build.gradle"].filter(f, file(f)) + ["pom.xml"])[0]`:                               "the first existing file of `pom.xml` or `build.gradle`, else `pom.xml`",
		`(glob("vitest.config.*") + glob("jest.config.*") + ["package.json"])[0]`:                         "the first existing file of `vitest.config.*` or `jest.config.*`, else `package.json`",
		`(glob("*.gemspec") + [""])[0]`:                                                                   "the first existing file of `*.gemspec`, else no file",
		`(params.renovate.filter(f, file(f)) + [".github/dependabot.yml"].filter(g, file(g)) + ["x"])[0]`: "the first existing file of `renovate.json`, `.github/renovate.json` or `.github/dependabot.yml`, else `x`",
	} {
		got, ok := locateText(expr, params)
		if !ok || got != want {
			t.Errorf("%s:\n got %q (%t)\nwant %q", expr, got, ok, want)
		}
	}
	for _, expr := range []string{
		`repo.public ? "LICENSE" : "README.md"`,               // the condition is not about the value
		`repo.name`,                                           // no phrase for it
		`(["a"].filter(f, file(x)) + ["b"])[0]`,               // not an existence filter
		`(["a"].filter(f, file(f)) + ["b"])[1]`,               // not the first
		`(["a"] + ["b"])[0]`,                                  // a candidate without an existence check
		`(params.missing.filter(f, file(f)) + ["b"])[0]`,      // no such parameter
		`taskrunner.file != "" ? taskrunner.file : repo.name`, // the fallback is not a path
		`(`,
	} {
		if got, ok := locateText(expr, params); ok {
			t.Errorf("%s: want no description, got %q", expr, got)
		}
	}
}
