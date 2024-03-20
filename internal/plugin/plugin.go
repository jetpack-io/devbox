// Copyright 2023 Jetpack Technologies Inc and contributors. All rights reserved.
// Use of this source code is governed by the license in the LICENSE file.

package plugin

import (
	"bytes"
	"cmp"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/pkg/errors"
	"go.jetpack.io/devbox/internal/devconfig/configfile"
	"go.jetpack.io/devbox/internal/devpkg"

	"go.jetpack.io/devbox/internal/debug"
	"go.jetpack.io/devbox/internal/lock"
	"go.jetpack.io/devbox/internal/nix"
	"go.jetpack.io/devbox/internal/services"
)

const (
	devboxDirName       = "devbox.d"
	devboxHiddenDirName = ".devbox"
	pluginConfigName    = "plugin.json"
)

var (
	VirtenvPath    = filepath.Join(devboxHiddenDirName, "virtenv")
	VirtenvBinPath = filepath.Join(VirtenvPath, "bin")
)

type Config struct {
	configfile.ConfigFile
	PluginOnlyData
}

type PluginOnlyData struct {
	CreateFiles           map[string]string `json:"create_files"`
	DeprecatedDescription string            `json:"readme"`
	// If true, we remove the package that triggered this plugin from the environment
	// Useful when we want to replace with flake
	RemoveTriggerPackage bool   `json:"__remove_trigger_package,omitempty"`
	Version              string `json:"version"`
	// Source is the includable that triggered this plugin. There are two ways to include a plugin:
	// 1. Built-in plugins are triggered by packages (See plugins.builtInMap)
	// 2. Plugins can be added via the "include" field in devbox.json or plugin.json
	Source Includable
}

func (c *Config) ProcessComposeYaml() (string, string) {
	for file, contentPath := range c.CreateFiles {
		if strings.HasSuffix(file, "process-compose.yaml") || strings.HasSuffix(file, "process-compose.yml") {
			return file, contentPath
		}
	}
	return "", ""
}

func (c *Config) Services() (services.Services, error) {
	if file, _ := c.ProcessComposeYaml(); file != "" {
		return services.FromProcessCompose(file)
	}
	return nil, nil
}

func (m *Manager) CreateFilesForConfig(cfg *Config) error {
	virtenvPath := filepath.Join(m.ProjectDir(), VirtenvPath)
	pkg := cfg.Source
	locked := m.lockfile.Packages[pkg.LockfileKey()]

	name := pkg.CanonicalName()

	// Always create this dir because some plugins depend on it.
	if err := createDir(filepath.Join(virtenvPath, name)); err != nil {
		return err
	}

	debug.Log("Creating files for package %q create files", pkg)
	for filePath, contentPath := range cfg.CreateFiles {
		if !m.shouldCreateFile(locked, filePath) {
			continue
		}

		dirPath := filepath.Dir(filePath)
		if contentPath == "" {
			dirPath = filePath
		}
		if err := createDir(dirPath); err != nil {
			return errors.WithStack(err)
		}

		if contentPath == "" {
			continue
		}

		if err := m.createFile(pkg, filePath, contentPath, virtenvPath); err != nil {
			return err
		}

	}

	if locked != nil {
		locked.PluginVersion = cfg.Version
	}

	return m.lockfile.Save()
}

func (m *Manager) createFile(
	pkg Includable,
	filePath, contentPath, virtenvPath string,
) error {
	name := pkg.CanonicalName()
	debug.Log("Creating file %q from contentPath: %q", filePath, contentPath)
	content, err := pkg.FileContent(contentPath)
	if err != nil {
		return errors.WithStack(err)
	}
	tmpl, err := template.New(filePath + "-template").Parse(string(content))
	if err != nil {
		return errors.WithStack(err)
	}

	var urlForInput, attributePath string

	if pkg, ok := pkg.(*devpkg.Package); ok {
		attributePath, err = pkg.PackageAttributePath()
		if err != nil {
			return err
		}
		urlForInput = pkg.URLForFlakeInput()
	}

	var buf bytes.Buffer
	if err = tmpl.Execute(&buf, map[string]any{
		"DevboxDir":            filepath.Join(m.ProjectDir(), devboxDirName, name),
		"DevboxDirRoot":        filepath.Join(m.ProjectDir(), devboxDirName),
		"DevboxProfileDefault": filepath.Join(m.ProjectDir(), nix.ProfilePath),
		"PackageAttributePath": attributePath,
		"Packages":             m.PackageNames(),
		"System":               nix.System(),
		"URLForInput":          urlForInput,
		"Virtenv":              filepath.Join(virtenvPath, name),
	}); err != nil {
		return errors.WithStack(err)
	}
	var fileMode fs.FileMode = 0o644
	if strings.Contains(filePath, "bin/") {
		fileMode = 0o755
	}

	if err := os.WriteFile(filePath, buf.Bytes(), fileMode); err != nil {
		return errors.WithStack(err)
	}
	if fileMode == 0o755 {
		if err := createSymlink(m.ProjectDir(), filePath); err != nil {
			return err
		}
	}
	return nil
}

func buildConfig(pkg Includable, projectDir, content string) (*Config, error) {
	cfg := &Config{PluginOnlyData: PluginOnlyData{Source: pkg}}
	name := pkg.CanonicalName()
	t, err := template.New(name + "-template").Parse(content)
	if err != nil {
		return nil, errors.WithStack(err)
	}
	var buf bytes.Buffer
	if err = t.Execute(&buf, map[string]string{
		"DevboxProjectDir":     projectDir,
		"DevboxDir":            filepath.Join(projectDir, devboxDirName, name),
		"DevboxDirRoot":        filepath.Join(projectDir, devboxDirName),
		"DevboxProfileDefault": filepath.Join(projectDir, nix.ProfilePath),
		"Virtenv":              filepath.Join(projectDir, VirtenvPath, name),
	}); err != nil {
		return nil, errors.WithStack(err)
	}

	return cfg, errors.WithStack(json.Unmarshal(buf.Bytes(), cfg))
}

func createDir(path string) error {
	if path == "" {
		return nil
	}
	return errors.WithStack(os.MkdirAll(path, 0o755))
}

func createSymlink(root, filePath string) error {
	name := filepath.Base(filePath)
	newname := filepath.Join(root, VirtenvBinPath, name)

	// Create bin path just in case it doesn't exist
	if err := os.MkdirAll(filepath.Join(root, VirtenvBinPath), 0o755); err != nil {
		return errors.WithStack(err)
	}

	if _, err := os.Lstat(newname); err == nil {
		if err = os.Remove(newname); err != nil {
			return errors.WithStack(err)
		}
	}

	return errors.WithStack(os.Symlink(filePath, newname))
}

func (m *Manager) shouldCreateFile(
	pkg *lock.Package,
	filePath string,
) bool {
	// Only create files in devboxDir if they are not in the lockfile
	pluginInstalled := pkg != nil && pkg.PluginVersion != ""
	if strings.Contains(filePath, devboxDirName) && pluginInstalled {
		return false
	}

	// Hidden .devbox files are always replaceable, so ok to recreate
	devboxHiddenDir := filepath.FromSlash("/.devbox/")
	if strings.Contains(filePath, devboxHiddenDir) {
		return true
	}
	_, err := os.Stat(filePath)
	// File doesn't exist, so we should create it.
	return errors.Is(err, fs.ErrNotExist)
}

func (c *Config) Description() string {
	if c == nil {
		return ""
	}
	return cmp.Or(c.ConfigFile.Description, c.DeprecatedDescription)
}
