// Copyright 2022 Jetpack Technologies Inc and contributors. All rights reserved.
// Use of this source code is governed by the license in the LICENSE file.

package impl

import (
	"embed"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/pkg/errors"
	"go.jetpack.io/devbox/internal/boxcli/featureflag"
	"go.jetpack.io/devbox/internal/debug"
	"go.jetpack.io/devbox/internal/planner/plansdk"
	"go.jetpack.io/devbox/internal/plugin"
)

//go:embed tmpl/* tmpl/.*
var tmplFS embed.FS

var shellFiles = []string{"development.nix", "shell.nix"}

func generateForShell(rootPath string, plan *plansdk.ShellPlan, pluginManager *plugin.Manager) error {
	outPath := filepath.Join(rootPath, ".devbox/gen")

	for _, file := range shellFiles {
		err := writeFromTemplate(outPath, plan, file)
		if err != nil {
			return errors.WithStack(err)
		}
	}

	// Gitignore file is added to the .devbox directory
	// TODO savil. Remove this hardcode from here, so this function can be generically defined again
	//    by accepting the files list parameter.
	err := writeFromTemplate(filepath.Join(rootPath, ".devbox"), plan, ".gitignore")
	if err != nil {
		return errors.WithStack(err)
	}

	err = makeFlakeFile(outPath, plan)
	if err != nil {
		return errors.WithStack(err)
	}

	for name, content := range plan.GeneratedFiles {
		filePath := filepath.Join(outPath, name)
		if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
			return errors.WithStack(err)
		}
	}

	for _, pkg := range plan.DevPackages {
		if err := pluginManager.CreateFilesAndShowReadme(pkg, rootPath); err != nil {
			return err
		}
	}

	return nil
}

func writeFromTemplate(path string, plan interface{}, tmplName string) error {
	embeddedPath := fmt.Sprintf("tmpl/%s.tmpl", tmplName)

	// Should we clear the directory so we start "fresh"?
	outPath := filepath.Join(path, tmplName)
	outDir := filepath.Dir(outPath)
	err := os.MkdirAll(outDir, 0755) // Ensure directory exists.
	if err != nil {
		return errors.WithStack(err)
	}

	f, err := os.Create(outPath)
	defer func() {
		_ = f.Close()
	}()
	if err != nil {
		return errors.WithStack(err)
	}
	t := template.Must(template.New(tmplName+".tmpl").Funcs(templateFuncs).ParseFS(tmplFS, embeddedPath))
	return errors.WithStack(t.Execute(f, plan))
}

func toJSON(a any) string {
	data, _ := json.Marshal(a)
	return string(data)
}

var templateFuncs = template.FuncMap{
	"json":     toJSON,
	"contains": strings.Contains,
	"debug":    debug.IsEnabled,
}

func makeFlakeFile(outPath string, plan *plansdk.ShellPlan) error {

	if featureflag.Flakes.Disabled() {
		return nil
	}

	flakeDir := filepath.Join(outPath, "flake")
	err := writeFromTemplate(flakeDir, plan, "flake.nix")
	if err != nil {
		return errors.WithStack(err)
	}

	// make an empty git repo
	// Alternatively consider: git add intent-to-add path/to/flake.nix, and
	// git update-index --assume-unchanged path/to/flake.nix
	// https://nixos.wiki/wiki/Flakes#How_to_add_a_file_locally_in_git_but_not_include_it_in_commits
	cmd := exec.Command("git", "-C", flakeDir, "init")
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err = cmd.Run()
	if err != nil {
		return errors.WithStack(err)
	}

	// add the flake.nix file to git
	cmd = exec.Command("git", "-C", flakeDir, "add", "flake.nix")
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err = cmd.Run()
	if err != nil {
		return errors.WithStack(err)
	}
	return nil
}
