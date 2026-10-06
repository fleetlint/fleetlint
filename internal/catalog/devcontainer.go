package catalog

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/fleetlint/fleetlint/internal/fix"
	"github.com/fleetlint/fleetlint/internal/repo"
)

const devcontainerTemplate = "devcontainer.json"

// devcontainerFeatures maps a stack to the dev container feature that
// installs its toolchain. Flutter has no feature and uses an image instead.
var devcontainerFeatures = map[string]string{
	"go":     "ghcr.io/devcontainers/features/go:1",
	"node":   "ghcr.io/devcontainers/features/node:1",
	"python": "ghcr.io/devcontainers/features/python:1",
	"rust":   "ghcr.io/devcontainers/features/rust:1",
	"kotlin": "ghcr.io/devcontainers/features/java:1",
	"hugo":   "ghcr.io/devcontainers/features/hugo:1",
	"java":   "ghcr.io/devcontainers/features/java:1",
	"dotnet": "ghcr.io/devcontainers/features/dotnet:1",
	"ruby":   "ghcr.io/devcontainers/features/ruby:1",
	"php":    "ghcr.io/devcontainers/features/php:1",
}

const (
	devcontainerBase    = "mcr.microsoft.com/devcontainers/base:ubuntu"
	devcontainerFlutter = "ghcr.io/cirruslabs/flutter:stable"
	// The environment is a second layer over the task runner: it provides
	// the toolchains and then asks the Makefile for the project's tools.
	devcontainerHooks = "if command -v prek >/dev/null 2>&1; then prek install; fi"
)

type devcontainer struct {
	Name              string                    `json:"name"`
	Image             string                    `json:"image"`
	Features          map[string]map[string]any `json:"features,omitempty"`
	ContainerEnv      map[string]string         `json:"containerEnv"`
	PostCreateCommand string                    `json:"postCreateCommand"`
}

// composeDevcontainer writes .devcontainer/devcontainer.json for the root
// stack and every further stack in the repository.
// devcontainerSetup hands over to the task runner after the container is
// created: its `tools` target when there is one, then the git hooks.
// devcontainerCpp is the image for C/C++ projects: no feature installs a compiler.
const devcontainerCpp = "mcr.microsoft.com/devcontainers/cpp:1"

func devcontainerSetup(p fix.Project) string {
	probe := map[string]string{
		repo.RunnerJust: "just --summary 2>/dev/null | tr ' ' '\\n' | grep -qx tools",
		repo.RunnerTask: "task --list-all 2>/dev/null | grep -q '^\\* tools:'",
	}[p.Runner]
	if probe == "" {
		probe = "make -n tools >/dev/null 2>&1"
	}
	return "if " + probe + "; then " + runnerCmd(p) + " tools; fi; " + devcontainerHooks
}

func composeDevcontainer(stack string, p fix.Project) ([]byte, error) {
	nested := p.Nested
	dc := devcontainer{
		Name: "dev", Image: devcontainerBase, Features: map[string]map[string]any{},
		// A runner file with the container switch runs its commands directly when it sees this.
		ContainerEnv:      map[string]string{"IN_CONTAINER": "1"},
		PostCreateCommand: devcontainerSetup(p),
	}
	stacks := make([]string, 0, 1+len(nested))
	stacks = append(stacks, stack)
	for _, n := range nested {
		stacks = append(stacks, n.Stack)
	}
	for _, s := range stacks {
		if s == "" {
			continue // no stack: the base image is enough
		}
		switch feature, ok := devcontainerFeatures[s]; {
		case ok && s == "kotlin":
			dc.Features[feature] = map[string]any{"installGradle": true}
		case ok:
			dc.Features[feature] = map[string]any{}
		case s == "flutter":
			dc.Image = devcontainerFlutter
		case s == "cpp":
			dc.Image = devcontainerCpp
		default:
			return nil, fmt.Errorf("no dev container template for stack %q", s)
		}
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(dc); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
