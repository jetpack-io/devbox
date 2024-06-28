package devconfig

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"net/http"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/pkg/errors"
	"github.com/samber/lo"
	"go.jetpack.io/devbox/internal/build"
	"go.jetpack.io/devbox/internal/cachehash"
	"go.jetpack.io/devbox/internal/devbox/shellcmd"
	"go.jetpack.io/devbox/internal/devconfig/configfile"
	"go.jetpack.io/devbox/internal/lock"
	"go.jetpack.io/devbox/internal/plugin"
)

// ErrNotFound occurs when [Open] or [Find] cannot find a devbox config file
// after searching a directory (and possibly its parent directories).
var ErrNotFound = errors.New("no devbox config file found")

// errIsDirectory indicates that a file can't be opened because it's a
// directory.
const errIsDirectory = syscall.EISDIR

// errNotDirectory indicates that a file can't be opened because the directory
// portion of its path is not a directory.
const errNotDirectory = syscall.ENOTDIR

// Config represents a base devbox.json as well as any included plugins it may have.
type Config struct {
	Root configfile.ConfigFile

	pluginData *plugin.PluginOnlyData // pointer by design, to allow for nil

	included []*Config
}

const defaultInitHook = "echo 'Welcome to devbox!' > /dev/null"

func DefaultConfig() *Config {
	cfg, err := loadBytes([]byte(fmt.Sprintf(`{
		"$schema": "https://raw.githubusercontent.com/jetify-com/devbox/%s/.schema/devbox.schema.json",
		"packages": [],
		"shell": {
			"init_hook": [
				"%s"
			],
			"scripts": {
				"test": [
					"echo \"Error: no test specified\" && exit 1"
				]
			}
		}
	}
	`,
		lo.Ternary(build.IsDev, "main", build.Version),
		defaultInitHook,
	)))
	if err != nil {
		panic("default devbox.json is invalid: " + err.Error())
	}
	return cfg
}

func IsDefault(path string) bool {
	cfg, err := readFromFile(path)
	if err != nil {
		return false
	}
	return cfg.Root.Equals(&DefaultConfig().Root)
}

// Open loads a Devbox config from a file or project directory. If path is a
// directory, Open looks for a well-known config name (such as devbox.json)
// within it. The error will be [ErrNotFound] if path is a valid directory
// without a config file.
//
// Open does not recursively search outside of path. See [Find] to load a config
// by walking up the directory tree.
func Open(path string) (*Config, error) {
	start := time.Now()
	slog.Debug("searching for config file (excluding parent directories)", "path", path)

	cfg, err := open(path)

	if err == nil {
		slog.Debug("config file found", "path", cfg.Root.AbsRootPath, "dur", time.Since(start))
	} else {
		slog.Error("config file search error", "err", err.Error(), "dur", time.Since(start))
	}
	return cfg, err
}

func open(path string) (*Config, error) {
	// First try the happy path by assuming that path is a directory
	// containing a devbox.json.
	cfg, err := searchDir(path)
	if errors.Is(err, ErrNotFound) || errors.Is(err, errNotDirectory) {
		// Try reading path directly as a config file.
		slog.Debug("trying config file", "path", path)
		cfg, err = readFromFile(path)
		if errors.Is(err, errIsDirectory) {
			return nil, ErrNotFound
		}
	}
	return cfg, err
}

// Find is like [Open] except it recursively searches up the directory tree,
// starting in path. It returns [ErrNotFound] if path is a valid directory and
// neither it nor any of its parents contain a config file.
//
// Find stops searching as soon as it encounters a file with a well-known config
// name (such as devbox.json), even if that config fails to load.
func Find(path string) (*Config, error) {
	start := time.Now()
	slog.Debug("searching for config file (including parent directories)", "path", path)

	cfg, err := open(path)
	if errors.Is(err, ErrNotFound) {
		cfg, err = searchParentDirs(path)
	}

	if err == nil {
		slog.Debug("config file found", "path", cfg.Root.AbsRootPath, "dur", time.Since(start))
	} else {
		slog.Error("config file search error", "err", err.Error(), "dur", time.Since(start))
	}
	return cfg, err
}

// searchDir looks for a config file in dir. It does not search parent
// directories.
func searchDir(dir string) (*Config, error) {
	try := []string{configfile.DefaultName}
	for _, name := range try {
		path := filepath.Join(dir, name)
		slog.Debug("trying config file", "path", path)

		cfg, err := readFromFile(path)
		if err == nil {
			return cfg, nil
		}

		// Keep searching for other valid config filenames.
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		// Ignore directories named devbox.json.
		if errors.Is(err, errIsDirectory) {
			continue
		}
		// Stop if we found a config but couldn't load it.
		return cfg, err
	}
	return nil, ErrNotFound
}

// searchParentDirs recursively searches parent directories for a config. It
// starts with filepath.Dir(path) and does not search path itself.
func searchParentDirs(path string) (cfg *Config, err error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("devconfig: search parent directories: %v", err)
	}

	err = ErrNotFound
	for abs != "/" && errors.Is(err, ErrNotFound) {
		abs = filepath.Dir(abs)
		cfg, err = searchDir(abs)
	}
	return cfg, err
}

func readFromFile(path string) (*Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	config, err := loadBytes(b)
	if err != nil {
		return nil, err
	}
	config.Root.AbsRootPath, err = filepath.Abs(path)
	return config, err
}

func LoadConfigFromURL(ctx context.Context, url string) (*Config, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, errors.WithStack(err)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	data, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, errors.WithStack(err)
	}
	return loadBytes(data)
}

func loadBytes(b []byte) (*Config, error) {
	root, err := configfile.LoadBytes(b)
	if err != nil {
		return nil, err
	}

	return &Config{
		Root: *root,
	}, nil
}

func (c *Config) LoadRecursive(lockfile *lock.File) error {
	return c.loadRecursive(lockfile, map[string]bool{}, "" /*cyclePath*/)
}

// loadRecursive loads all the included plugins and their included plugins, etc.
// seen should be a cloned map because loading plugins twice is allowed if they
// are in different paths.
func (c *Config) loadRecursive(
	lockfile *lock.File,
	seen map[string]bool,
	cyclePath string,
) error {
	included := make([]*Config, 0, len(c.Root.Include))

	for _, includeRef := range c.Root.Include {
		pluginConfig, err := plugin.LoadConfigFromInclude(
			includeRef, lockfile, filepath.Dir(c.Root.AbsRootPath))
		if err != nil {
			return errors.WithStack(err)
		}

		newCyclePath := fmt.Sprintf("%s -> %s", cyclePath, includeRef)
		if seen[pluginConfig.Source.Hash()] {
			// Note that duplicate includes are allowed if they are in different paths
			// e.g. 2 different plugins can include the same plugin.
			// We do not allow a single plugin to include duplicates.
			return errors.Errorf(
				"circular or duplicate include detected:\n%s", newCyclePath)
		}
		seen[pluginConfig.Source.Hash()] = true

		includable := createIncludableFromPluginConfig(pluginConfig)

		if err := includable.loadRecursive(
			lockfile, maps.Clone(seen), newCyclePath); err != nil {
			return errors.WithStack(err)
		}

		included = append(included, includable)
	}

	builtIns, err := plugin.GetBuiltinsForPackages(
		c.Root.TopLevelPackages(),
		lockfile,
	)
	if err != nil {
		return errors.WithStack(err)
	}

	for _, builtIn := range builtIns {
		includable := &Config{
			Root:       builtIn.ConfigFile,
			pluginData: &builtIn.PluginOnlyData,
		}
		newCyclePath := fmt.Sprintf("%s -> %s", cyclePath, builtIn.Source.LockfileKey())
		if err := includable.loadRecursive(
			lockfile, maps.Clone(seen), newCyclePath); err != nil {
			return errors.WithStack(err)
		}
		included = append(included, includable)
	}

	c.included = included
	return nil
}

func (c *Config) PackageMutator() *configfile.PackagesMutator {
	return &c.Root.PackagesMutator
}

func (c *Config) IncludedPluginConfigs() []*plugin.Config {
	configs := []*plugin.Config{}
	for _, i := range c.included {
		configs = append(configs, i.IncludedPluginConfigs()...)
	}
	if c.pluginData != nil {
		configs = append(configs, &plugin.Config{
			ConfigFile:     c.Root,
			PluginOnlyData: *c.pluginData,
		})
	}
	return configs
}

// Returns all packages including those from included plugins.
// If includeRemovedTriggerPackages is true, then trigger packages that have
// been removed will also be returned. These are only used for built-ins
// (e.g. php) when the plugin creates a flake that is meant to replace the
// original package.
func (c *Config) Packages(
	includeRemovedTriggerPackages bool,
) []configfile.Package {
	packages := []configfile.Package{}
	packagesToRemove := map[string]bool{}

	for _, i := range c.included {
		packages = append(packages, i.Packages(includeRemovedTriggerPackages)...)
		if i.pluginData.RemoveTriggerPackage && !includeRemovedTriggerPackages {
			packagesToRemove[i.pluginData.Source.LockfileKey()] = true
		}
	}

	// Packages to remove in built ins only affect the devbox.json where they are defined.
	// They should not remove packages that are part of other imports.
	for _, pkg := range c.Root.TopLevelPackages() {
		if !packagesToRemove[pkg.VersionedName()] {
			packages = append(packages, pkg)
		}
	}

	// Keep only the last occurrence of each package (by name).
	return lo.Reverse(lo.UniqBy(
		lo.Reverse(packages),
		func(p configfile.Package) string { return p.Name },
	))
}

func (c *Config) NixPkgsCommitHash() string {
	return c.Root.NixPkgsCommitHash()
}

func (c *Config) Env() map[string]string {
	env := map[string]string{}
	for _, i := range c.included {
		maps.Copy(env, i.Env())
	}
	maps.Copy(env, c.Root.Env)
	return env
}

func (c *Config) InitHook() *shellcmd.Commands {
	commands := shellcmd.Commands{}
	for _, i := range c.included {
		commands.Cmds = append(commands.Cmds, i.InitHook().Cmds...)
	}
	commands.Cmds = append(commands.Cmds, c.Root.InitHook().Cmds...)
	return &commands
}

func (c *Config) Scripts() configfile.Scripts {
	scripts := configfile.Scripts{}
	for _, i := range c.included {
		maps.Copy(scripts, i.Scripts())
	}
	maps.Copy(scripts, c.Root.Scripts())
	return scripts
}

func (c *Config) Hash() (string, error) {
	data := []byte{}
	for _, i := range c.included {
		hash, err := i.Hash()
		if err != nil {
			return "", err
		}
		data = append(data, hash...)
	}
	hash, err := c.Root.Hash()
	if err != nil {
		return "", err
	}
	data = append(data, hash...)
	return cachehash.Bytes(data), nil
}

func (c *Config) IsEnvsecEnabled() bool {
	for _, i := range c.included {
		if i.IsEnvsecEnabled() {
			return true
		}
	}
	return c.Root.IsEnvsecEnabled()
}

func createIncludableFromPluginConfig(pluginConfig *plugin.Config) *Config {
	includable := &Config{
		Root:       pluginConfig.ConfigFile,
		pluginData: &pluginConfig.PluginOnlyData,
	}
	if localPlugin, ok := pluginConfig.Source.(*plugin.LocalPlugin); ok {
		includable.Root.AbsRootPath = localPlugin.Path()
	}
	return includable
}
