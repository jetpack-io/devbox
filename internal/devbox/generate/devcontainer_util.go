// Copyright 2024 Jetify Inc. and contributors. All rights reserved.
// Use of this source code is governed by the license in the LICENSE file.

package generate

// package generate has functionality to implement the `devbox generate` command

import (
	"cmp"
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime/trace"
	"strings"
	"text/template"

	"github.com/samber/lo"
	"go.jetpack.io/devbox/internal/boxcli/usererr"
	"go.jetpack.io/devbox/internal/debug"
	"go.jetpack.io/devbox/internal/devbox/devopt"
)

//go:embed tmpl/*
var tmplFS embed.FS

type Options struct {
	Path           string
	RootUser       bool
	IsDevcontainer bool
	Pkgs           []string
	LocalFlakeDirs []string
}

type devcontainerObject struct {
	Name           string          `json:"name"`
	Build          *build          `json:"build"`
	Customizations *customizations `json:"customizations"`
	RemoteUser     string          `json:"remoteUser"`
}

type build struct {
	Dockerfile string `json:"dockerfile"`
	Context    string `json:"context"`
}

type customizations struct {
	Vscode *vscode `json:"vscode"`
}

type vscode struct {
	Settings   any      `json:"settings"`
	Extensions []string `json:"extensions"`
}

type CreateDockerfileOptions struct {
	ForType    string
	HasInstall bool
	HasBuild   bool
	HasStart   bool
	// Ideally we also support process-compose services as the dockerfile
	// CMD, but I'm currently having trouble getting that to work. Will revisit.
	// HasServices bool
}

func (opts CreateDockerfileOptions) Type() string {
	return cmp.Or(opts.ForType, "dev")
}

func (opts CreateDockerfileOptions) validate() error {
	if opts.Type() == "dev" {
		return nil
	} else if opts.Type() == "prod" {
		if opts.HasStart {
			return nil
		}
		return usererr.New(
			"To generate a prod Dockerfile you must have either 'start' script in " +
				"devbox.json",
		)
	}
	return usererr.New(
		"invalid Dockerfile type. Only 'dev' and 'prod' are supported")
}

// CreateDockerfile creates a Dockerfile in path.
func (g *Options) CreateDockerfile(
	ctx context.Context,
	opts CreateDockerfileOptions,
) error {
	defer trace.StartRegion(ctx, "createDockerfile").End()

	if err := opts.validate(); err != nil {
		return err
	}

	// create dockerfile
	file, err := os.Create(filepath.Join(g.Path, "Dockerfile"))
	if err != nil {
		return err
	}
	defer file.Close()
	path := fmt.Sprintf("tmpl/%s.Dockerfile.tmpl", opts.Type())
	t := template.Must(template.ParseFS(tmplFS, path))
	// write content into file
	return t.Execute(file, map[string]any{
		"IsDevcontainer": g.IsDevcontainer,
		"RootUser":       g.RootUser,
		"LocalFlakeDirs": g.LocalFlakeDirs,

		// The following are only used for prod Dockerfile
		"DevboxRunInstall": lo.Ternary(opts.HasInstall, "devbox run install", "echo 'No install script found, skipping'"),
		"DevboxRunBuild":   lo.Ternary(opts.HasBuild, "devbox run build", "echo 'No build script found, skipping'"),
		"Cmd":              fmt.Sprintf("%q, %q, %q", "devbox", "run", "start"),
	})
}

// CreateDevcontainer creates a devcontainer.json in path and writes getDevcontainerContent's output into it
func (g *Options) CreateDevcontainer(ctx context.Context) error {
	defer trace.StartRegion(ctx, "createDevcontainer").End()

	// create devcontainer.json file
	file, err := os.Create(filepath.Join(g.Path, "devcontainer.json"))
	if err != nil {
		return err
	}
	defer file.Close()
	// get devcontainer.json's content
	devcontainerContent := g.getDevcontainerContent()
	devcontainerFileBytes, err := json.MarshalIndent(devcontainerContent, "", "  ")
	if err != nil {
		return err
	}
	// writing devcontainer's content into json file
	_, err = file.Write(devcontainerFileBytes)
	return err
}

func CreateEnvrc(ctx context.Context, path string, envFlags devopt.EnvFlags) error {
	defer trace.StartRegion(ctx, "createEnvrc").End()

	// create .envrc file
	file, err := os.Create(filepath.Join(path, ".envrc"))
	if err != nil {
		return err
	}
	defer file.Close()

	flags := []string{}

	if len(envFlags.EnvMap) > 0 {
		for k, v := range envFlags.EnvMap {
			flags = append(flags, fmt.Sprintf("--env %s=%s", k, v))
		}
	}
	if envFlags.EnvFile != "" {
		flags = append(flags, fmt.Sprintf("--env-file %s", envFlags.EnvFile))
	}

	t := template.Must(template.ParseFS(tmplFS, "tmpl/envrc.tmpl"))

	// write content into file
	return t.Execute(file, map[string]string{
		"Flags": strings.Join(flags, " "),
	})
}

func (g *Options) getDevcontainerContent() *devcontainerObject {
	// object that gets written in devcontainer.json
	devcontainerContent := &devcontainerObject{
		// For format details, see https://aka.ms/devcontainer.json. For config options, see the README at:
		// https://github.com/microsoft/vscode-dev-containers/tree/v0.245.2/containers/debian
		Name: "Devbox Remote Container",
		Build: &build{
			Dockerfile: "./Dockerfile",
			Context:    "..",
		},
		Customizations: &customizations{
			Vscode: &vscode{
				Settings: map[string]any{
					// Add custom vscode settings for remote environment here
				},
				Extensions: []string{
					"jetpack-io.devbox",
					// Add custom vscode extensions for remote environment here
				},
			},
		},
		RemoteUser: "devbox",
	}
	if g.RootUser {
		devcontainerContent.RemoteUser = "root"
	}

	// match only python3 or python3xx as package names
	py3pattern, err := regexp.Compile(`(python3)$|(python3[0-9]{1,2})$`)
	if err != nil {
		debug.Log("Failed to compile regex")
		return nil
	}
	for _, pkg := range g.Pkgs {
		if py3pattern.MatchString(pkg) {
			// Setup python3 interpreter path to devbox in the container
			devcontainerContent.Customizations.Vscode.Settings = map[string]any{
				"python.defaultInterpreterPath": "/code/.devbox/nix/profile/default/bin/python3",
			}
			// add python extension if a python3 package is installed
			devcontainerContent.Customizations.Vscode.Extensions = append(devcontainerContent.Customizations.Vscode.Extensions, "ms-python.python")
		}
		if strings.Contains(pkg, "go_1_") || pkg == "go" {
			devcontainerContent.Customizations.Vscode.Extensions = append(devcontainerContent.Customizations.Vscode.Extensions, "golang.go")
		}
		// TODO: add support for other common languages
	}
	return devcontainerContent
}

func EnvrcContent(w io.Writer, envFlags devopt.EnvFlags) error {
	tmplName := "envrcContent.tmpl"
	t := template.Must(template.ParseFS(tmplFS, "tmpl/"+tmplName))
	envFlag := ""
	if len(envFlags.EnvMap) > 0 {
		for k, v := range envFlags.EnvMap {
			envFlag += fmt.Sprintf("--env %s=%s ", k, v)
		}
	}
	return t.Execute(w, map[string]string{
		"EnvFlag": envFlag,
		"EnvFile": envFlags.EnvFile,
	})
}
