package testrunner

import (
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/pkg/errors"
	"github.com/rogpeppe/go-internal/testscript"

	"go.jetpack.io/devbox/internal/devconfig"
	"go.jetpack.io/devbox/internal/envir"
)

// xdgStateHomeDir is the home directory for devbox state. We store symlinks to
// virtenvs of devbox plugins in this directory. We need to use a custom
// path that is intentionally short, since some plugins store unix sockets in
// their virtenv and unix sockets require their paths to be short.
const xdgStateHomeDir = "/tmp/devbox-testscripts"

// RunDevboxTestscripts generates and runs a testscript test for each Devbox project in dir.
// For each project, runs `devbox run run_test` (if script exists) and asserts it succeeds.
func RunDevboxTestscripts(t *testing.T, dir string) {
	// ensure the state home dir for devbox exists
	err := os.MkdirAll(xdgStateHomeDir, 0o700)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		t.Error(err)
	}

	err = filepath.WalkDir(dir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if !entry.IsDir() {
			return nil
		}

		configPath := filepath.Join(path, "devbox.json")
		config, err := devconfig.Open(configPath)
		if err != nil {
			// skip directories that do not have a devbox.json defined
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return err
		}
		// skip configs that do not have a run_test defined
		if _, ok := config.Scripts()["run_test"]; !ok {
			t.Logf("skipping config due to missing run_test at: %s\n", path)
			return nil
		}

		if strings.Contains(path, "pipenv") {
			// pipenv takes 1100 seconds on CICD

			// CI env var is always true in GitHub Actions
			// https://docs.github.com/en/actions/learn-github-actions/variables#default-environment-variables
			isInCI := envir.IsCI()
			if isInCI && runtime.GOOS == "darwin" {
				t.Logf("skipping pipenv on darwin in CI. config at: %s\n", path)
				return nil
			}
		}

		if strings.Contains(path, "drupal") {
			// drupal has errors like: https://gist.github.com/savil/9c67ffa50a2c51d118f3a4ce29ab920d
			t.Logf("skipping drupal, config at: %s\n", path)
			return nil
		}

		t.Logf("running testscript for example: %s\n", path)
		runSingleDevboxTestscript(t, dir, path)
		return nil
	})
	if err != nil {
		t.Error(err)
	}
}

func runSingleDevboxTestscript(t *testing.T, dir, projectDir string) {
	testscriptDir, err := generateTestscript(t, dir, projectDir)
	if err != nil {
		t.Error(err)
	}

	params := getTestscriptParams(t, testscriptDir)

	// save a reference to the original params.Setup so that we can wrap it below
	setup := params.Setup
	params.Setup = func(envs *testscript.Env) error {
		// We set a custom XDG_STATE_HOME to an intentionally short path.
		// Reason: devbox plugins like postgres store unix socket files in their state dir.
		envs.Setenv(envir.XDGStateHome, xdgStateHomeDir)

		// setup the devbox testscript environment
		if err := setup(envs); err != nil {
			return errors.WithStack(err)
		}

		// copy all the files and folders of the devbox-project being tested to the workdir
		slog.Debug("copying projectDir: %s to env.WorkDir: %s\n", projectDir, envs.WorkDir)
		// implementation detail: the period at the end of the projectDir/.
		// is important to ensure this works for both mac and linux.
		// Ref.https://dev.to/ackshaey/macos-vs-linux-the-cp-command-will-trip-you-up-2p00

		cmd := exec.Command("rm", "-rf", projectDir+"/.devbox")
		err = cmd.Run()
		if err != nil {
			slog.Error("failed %s before doing cp", "cmd", cmd, "err", err)
			return errors.WithStack(err)
		}

		cmd = exec.Command("cp", "-r", projectDir+"/.", envs.WorkDir)
		slog.Debug("running cmd", "cmd", cmd)
		err = cmd.Run()
		return errors.WithStack(err)
	}

	testscript.Run(t, params)
}

// generateTestscript will create a temp-directory and place the generic
// testscript file (.test.txt) for all devbox-projects in the dir.
// It returns the directory containing the testscript file.
func generateTestscript(t *testing.T, dir, projectDir string) (string, error) {
	testPath, err := filepath.Rel(dir, projectDir)
	if err != nil {
		return "", errors.WithStack(err)
	}

	// scriptName is the generic script file used for all devbox projects
	const scriptName = "run_test.test.txt"

	// scriptNameForProject prefixes the project's path (with underscores) to the scriptName
	// so that the golang testing.T provides nice readable names for the test run
	// for each Example devbox-project.
	scriptNameForProject := fmt.Sprintf(
		"%s_%s",
		strings.ReplaceAll(testPath, "/", "_"),
		scriptName,
	)

	// create a temp-dir to place the testscript file
	testscriptDir := t.TempDir()

	// Copy the testscript file to the temp-dir
	runTestScriptPath := filepath.Join("testrunner", scriptName)
	slog.Debug("copying run_test.test.txt from %s to %s\n", runTestScriptPath, testscriptDir)
	// Using os's cp command for expediency.
	err = exec.Command("cp", runTestScriptPath, testscriptDir+"/"+scriptNameForProject).Run()
	return testscriptDir, errors.WithStack(err)
}
