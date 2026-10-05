package catalog

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/fleetlint/fleetlint/internal/fix"
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
}

const (
	devcontainerBase    = "mcr.microsoft.com/devcontainers/base:ubuntu"
	devcontainerFlutter = "ghcr.io/cirruslabs/flutter:stable"
	// The environment is a second layer over the task runner: it provides
	// the toolchains and then asks the Makefile for the project's tools.
	devcontainerSetup = "if make -n tools >/dev/null 2>&1; then make tools; fi; if command -v prek >/dev/null 2>&1; then prek install; fi"
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
func composeDevcontainer(stack string, nested []fix.Nested) ([]byte, error) {
	dc := devcontainer{
		Name: "dev", Image: devcontainerBase, Features: map[string]map[string]any{},
		// A Makefile with the dev container switch runs its recipes directly when it sees this.
		ContainerEnv:      map[string]string{"IN_DEVCONTAINER": "1"},
		PostCreateCommand: devcontainerSetup,
	}
	stacks := make([]string, 0, 1+len(nested))
	stacks = append(stacks, stack)
	for _, n := range nested {
		stacks = append(stacks, n.Stack)
	}
	for _, s := range stacks {
		switch feature, ok := devcontainerFeatures[s]; {
		case ok && s == "kotlin":
			dc.Features[feature] = map[string]any{"installGradle": true}
		case ok:
			dc.Features[feature] = map[string]any{}
		case s == "flutter":
			dc.Image = devcontainerFlutter
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
