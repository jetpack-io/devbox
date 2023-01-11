package plugin

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/pkg/errors"
	"go.jetpack.io/devbox/internal/debug"
	"go.jetpack.io/devbox/internal/impl/shellcmd"
	"go.jetpack.io/devbox/internal/nix"
)

const (
	devboxDirName       = "devbox.d"
	devboxHiddenDirName = ".devbox"
	VirtenvBinPath      = ".devbox/virtenv/bin"
	VirtenvPath         = ".devbox/virtenv"
)

type config struct {
	Name        string            `json:"name"`
	Version     string            `json:"version"`
	Match       string            `json:"match"`
	CreateFiles map[string]string `json:"create_files"`
	Env         map[string]string `json:"env"`
	Readme      string            `json:"readme"`
	Services    Services          `json:"services"`

	Shell struct {
		// InitHook contains commands that will run at shell startup.
		InitHook shellcmd.Commands `json:"init_hook,omitempty"`
	} `json:"shell,omitempty"`
}

func (m *Manager) CreateFilesAndShowReadme(pkg, projectDir string) error {
	cfg, err := getConfigIfAny(pkg, projectDir)
	if err != nil {
		return err
	}
	if cfg == nil {
		return nil
	}

	debug.Log("Creating files for package %q create files", pkg)
	for filePath, contentPath := range cfg.CreateFiles {

		if !m.shouldCreateFile(filePath) {
			continue
		}

		dirPath := filepath.Dir(filePath)
		if contentPath == "" {
			dirPath = filePath
		}
		if err = createDir(dirPath); err != nil {
			return errors.WithStack(err)
		}

		if contentPath == "" {
			continue
		}

		debug.Log("Creating file %q", filePath)
		content, err := getFileContent(contentPath)
		if err != nil {
			return errors.WithStack(err)
		}
		t, err := template.New(filePath + "-template").Parse(string(content))
		if err != nil {
			return errors.WithStack(err)
		}
		var buf bytes.Buffer
		if err = t.Execute(&buf, map[string]string{
			"DevboxConfigDir":      projectDir,
			"DevboxDir":            filepath.Join(projectDir, devboxDirName, pkg),
			"DevboxDirRoot":        filepath.Join(projectDir, devboxDirName),
			"DevboxProfileDefault": filepath.Join(projectDir, nix.ProfilePath),
			"Virtenv":              filepath.Join(projectDir, devboxHiddenDirName, "virtenv", pkg),
		}); err != nil {
			return errors.WithStack(err)
		}
		var fileMode fs.FileMode = 0644
		if strings.Contains(filePath, "bin/") {
			fileMode = 0755
		}

		if err := os.WriteFile(filePath, buf.Bytes(), fileMode); err != nil {
			return errors.WithStack(err)
		}
		if fileMode == 0755 {
			if err := createSymlink(projectDir, filePath); err != nil {
				return err
			}
		}
	}
	return createEnvFile(pkg, projectDir)

}

func Env(pkgs []string, projectDir string) ([]string, error) {
	env := []string{}
	for _, pkg := range pkgs {
		cfg, err := getConfigIfAny(pkg, projectDir)
		if err != nil {
			return nil, err
		}
		if cfg == nil {
			continue
		}
		for k, v := range cfg.Env {
			env = append(env, fmt.Sprintf("%s=%s", k, v))
		}
	}
	return env, nil
}

func getEnvFilePathIfExist(pkg, projectDir string) string {
	filePath := filepath.Join(projectDir, VirtenvPath, pkg, "/env")
	if _, err := os.Stat(filePath); err != nil {
		return ""
	}
	return filePath
}

func createEnvFile(pkg, projectDir string) error {
	envVars, err := Env([]string{pkg}, projectDir)
	if err != nil {
		return err
	}
	env := ""
	for _, val := range envVars {
		parts := strings.SplitN(val, "=", 2)
		escaped, err := json.Marshal(parts[1])
		if err != nil {
			return errors.WithStack(err)
		}
		env += fmt.Sprintf("export %s=%s\n", parts[0], escaped)
	}
	filePath := filepath.Join(projectDir, VirtenvPath, pkg, "/env")
	if err = createDir(filepath.Dir(filePath)); err != nil {
		return err
	}
	if err := os.WriteFile(filePath, []byte(env), 0644); err != nil {
		return errors.WithStack(err)
	}
	return nil
}

func buildConfig(pkg, projectDir, content string) (*config, error) {
	cfg := &config{}
	t, err := template.New(pkg + "-template").Parse(content)
	if err != nil {
		return nil, errors.WithStack(err)
	}
	var buf bytes.Buffer
	if err = t.Execute(&buf, map[string]string{
		"DevboxProjectDir":     projectDir,
		"DevboxDir":            filepath.Join(projectDir, devboxDirName, pkg),
		"DevboxDirRoot":        filepath.Join(projectDir, devboxDirName),
		"DevboxProfileDefault": filepath.Join(projectDir, nix.ProfilePath),
		"Virtenv":              filepath.Join(projectDir, devboxHiddenDirName, "virtenv", pkg),
	}); err != nil {
		return nil, errors.WithStack(err)
	}
	if err = json.Unmarshal(buf.Bytes(), cfg); err != nil {
		return nil, errors.WithStack(err)
	}
	return cfg, nil
}

func createDir(path string) error {
	if path == "" {
		return nil
	}
	if err := os.MkdirAll(path, 0755); err != nil {
		return errors.WithStack(err)
	}
	return nil
}

func createSymlink(root, filePath string) error {
	name := filepath.Base(filePath)
	newname := filepath.Join(root, VirtenvPath, "bin", name)

	// Create bin path just in case it doesn't exist
	if err := os.MkdirAll(filepath.Join(root, VirtenvPath, "/bin"), 0755); err != nil {
		return errors.WithStack(err)
	}

	if _, err := os.Lstat(newname); err == nil {
		if err = os.Remove(newname); err != nil {
			return errors.WithStack(err)
		}
	}

	if err := os.Symlink(filePath, newname); err != nil {
		return errors.WithStack(err)
	}
	return nil
}

func (m *Manager) shouldCreateFile(filePath string) bool {
	// Only create devboxDir files in add mode.
	if strings.Contains(filePath, devboxDirName) && !m.addMode {
		return false
	}

	// Hidden .devbox files are always replaceable, so ok to recreate
	if strings.Contains(filePath, devboxHiddenDirName) {
		return true
	}
	_, err := os.Stat(filePath)
	// File doesn't exist, so we should create it.
	return os.IsNotExist(err)
}
