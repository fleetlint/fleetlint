package stacks_test

import (
	"reflect"
	"testing"
	"testing/fstest"

	data "github.com/fleetlint/catalog"

	"github.com/fleetlint/fleetlint/internal/stacks"
	"github.com/fleetlint/fleetlint/internal/testutil"
)

func TestDetectWithTheCatalogStacks(t *testing.T) {
	t.Parallel()
	defs, err := stacks.Load(data.Stacks)
	if err != nil || len(defs) == 0 {
		t.Fatalf("catalog stacks: %v (%d)", err, len(defs))
	}
	cases := map[string]struct {
		files map[string]string
		want  []string
	}{
		"go":                    {map[string]string{"go.mod": "module x\n"}, []string{"go"}},
		"kotlin gradle":         {map[string]string{"build.gradle.kts": "plugins { kotlin(\"jvm\") }\n"}, []string{"kotlin"}},
		"java groovy gradle":    {map[string]string{"build.gradle": "plugins { id 'java' }\n"}, []string{"java"}},
		"android thin shell":    {map[string]string{"settings.gradle.kts": "include(\":androidApp\")\n", "androidApp/build.gradle.kts": "plugins { alias(libs.plugins.androidApplication) }\n"}, []string{"kotlin"}},
		"java modules":          {map[string]string{"settings.gradle": "include 'app'\n", "app/build.gradle": "plugins { id 'application' }\n"}, []string{"java"}},
		"kotlin module":         {map[string]string{"settings.gradle.kts": "include(\":app\")\n", "app/build.gradle.kts": "plugins { id(\"org.jetbrains.kotlin.jvm\") }\n"}, []string{"kotlin"}},
		"flutter android shell": {map[string]string{"pubspec.yaml": "name: a\n", "android/app/build.gradle.kts": "plugins { id(\"kotlin-android\") }\n"}, []string{"flutter"}},
		"hugo legacy":           {map[string]string{"config.toml": "baseURL = 'https://x'\n", "content/_index.md": "x"}, []string{"hugo"}},
		"hugo modules":          {map[string]string{"hugo.toml": "x", "go.mod": "module site\n"}, []string{"hugo"}},
		"hugo with go code":     {map[string]string{"hugo.toml": "x", "go.mod": "module site\n", "main.go": "package main\n"}, []string{"go", "hugo"}},
		"toml without content":  {map[string]string{"config.toml": "baseURL = 'https://x'\n"}, []string{}},
		"dotnet project":        {map[string]string{"src/App/App.csproj": "<Project/>"}, []string{"dotnet"}},
		"dotnet fixture only":   {map[string]string{"testdata/x/App.csproj": "<Project/>"}, []string{}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got := stacks.Detect(testutil.Fixture(t, tc.files), defs)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestLegacyDetectionIsUnchanged(t *testing.T) {
	t.Parallel()
	defs := stacks.Legacy()
	r := testutil.Fixture(t, map[string]string{"pubspec.yaml": "name: a\n", "build.gradle": "x"})
	if got := stacks.Detect(r, defs); !reflect.DeepEqual(got, []string{"flutter"}) {
		t.Fatalf("a root Groovy build file next to a pubspec stays Flutter: %v", got)
	}
	r = testutil.Fixture(t, map[string]string{"pubspec.yaml": "name: a\n", "build.gradle.kts": "x"})
	if got := stacks.Detect(r, defs); !reflect.DeepEqual(got, []string{"flutter", "kotlin"}) {
		t.Fatalf("a root build.gradle.kts is a real Gradle project: %v", got)
	}
}

func TestLoadRefusesBrokenDefinitions(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"id must match the file": "id: other\ntitle: X\ndetect: [{file: x}]\n",
		"needs a marker":         "id: x\ntitle: X\ndetect: []\n",
		"file or glob, not both": "id: x\ntitle: X\ndetect: [{file: a, glob: b}]\n",
		"bad regex":              "id: x\ntitle: X\ndetect: [{file: a, contains: \"(\"}]\n",
		"unknown key":            "id: x\ntitle: X\ndetect: [{file: a}]\nlanguage: x\n",
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			fsys := fstest.MapFS{"stacks/x.yaml": {Data: []byte(body)}}
			if _, err := stacks.Load(fsys); err == nil {
				t.Fatal("want an error")
			}
		})
	}
	if defs, err := stacks.Load(fstest.MapFS{}); err != nil || defs != nil {
		t.Fatalf("no stacks/ directory is no definitions and no error: %v %v", defs, err)
	}
}
