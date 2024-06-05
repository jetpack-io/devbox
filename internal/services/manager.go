// Copyright 2024 Jetify Inc. and contributors. All rights reserved.
// Use of this source code is governed by the license in the LICENSE file.

package services

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/pkg/errors"

	"go.jetpack.io/devbox/internal/boxcli/usererr"
	"go.jetpack.io/devbox/internal/cuecfg"
	"go.jetpack.io/devbox/internal/xdg"
)

const (
	processComposeLogfile = ".devbox/compose.log"
	fileLockTimeout       = 5 * time.Second
)

type instance struct {
	Pid  int `json:"pid"`
	Port int `json:"port"`
}

type instanceMap = map[string]instance

type globalProcessComposeConfig struct {
	Instances instanceMap
	Path      string   `json:"-"`
	File      *os.File `json:"-"`
}

type ProcessComposeOpts struct {
	BinPath    string
	ExtraFlags []string
	Background bool
}

func newGlobalProcessComposeConfig() *globalProcessComposeConfig {
	return &globalProcessComposeConfig{Instances: map[string]instance{}}
}

func globalProcessComposeJSONPath() (string, error) {
	path := xdg.DataSubpath(filepath.Join("devbox", "global"))
	return filepath.Join(path, "process-compose.json"), errors.WithStack(os.MkdirAll(path, 0o755))
}

func readGlobalProcessComposeJSON(file *os.File) *globalProcessComposeConfig {
	config := newGlobalProcessComposeConfig()

	err := errors.WithStack(cuecfg.ParseFile(file.Name(), &config.Instances))
	if err != nil {
		return config
	}
	config.Path = file.Name()
	return config
}

func writeGlobalProcessComposeJSON(config *globalProcessComposeConfig, file *os.File) error {
	// convert config to json using cue
	json, err := cuecfg.MarshalJSON(config.Instances)
	if err != nil {
		return fmt.Errorf("failed to convert config to json: %w", err)
	}

	if err := file.Truncate(0); err != nil {
		return fmt.Errorf("failed to truncate global config file: %w", err)
	}

	if _, err := file.Write(json); err != nil {
		return fmt.Errorf("failed to write global config file: %w", err)
	}

	return nil
}

func openGlobalConfigFile() (*os.File, error) {
	configPath, err := globalProcessComposeJSONPath()
	if err != nil {
		return nil, fmt.Errorf("failed to get config path: %w", err)
	}

	globalConfigFile, err := os.OpenFile(configPath, os.O_WRONLY|os.O_CREATE, 0o664)
	if err != nil {
		return nil, fmt.Errorf("failed to open config file: %w", err)
	}

	err = lockFile(globalConfigFile)
	if err != nil {
		return nil, fmt.Errorf("failed to lock file: %w", err)
	}

	return globalConfigFile, nil
}

func StartProcessManager(
	w io.Writer,
	requestedServices []string,
	availableServices Services,
	projectDir string,
	processComposeConfig ProcessComposeOpts,
) error {
	// Check if process-compose is already running
	if ProcessManagerIsRunning(projectDir) {
		return fmt.Errorf("process-compose is already running. To stop it, run `devbox services stop`")
	}

	// Get the file and lock it right at the start

	configFile, err := openGlobalConfigFile()
	if err != nil {
		return err
	}

	defer configFile.Close()

	// Read the global config file
	config := readGlobalProcessComposeJSON(configFile)
	config.File = configFile

	// Get the port to use for this project
	port, err := getAvailablePort()
	if err != nil {
		return err
	}

	// Start building the process-compose command
	flags := []string{"-p", strconv.Itoa(port)}
	upCommand := []string{"up"}

	if len(requestedServices) > 0 {
		flags = append(requestedServices, flags...)
		flags = append(upCommand, flags...)
		fmt.Fprintf(w, "Starting services: %s \n", strings.Join(requestedServices, ", "))
	} else {
		services := []string{}
		for k := range availableServices {
			services = append(services, k)
		}
		fmt.Fprintf(w, "Starting all services: %s \n", strings.Join(services, ", "))
	}

	for _, s := range availableServices {
		flags = append(flags, "-f", s.ProcessComposePath)
	}

	flags = append(flags, processComposeConfig.ExtraFlags...)

	if processComposeConfig.Background {
		flags = append(flags, "-t=false")
		cmd := exec.Command(processComposeConfig.BinPath, flags...)
		return runProcessManagerInBackground(cmd, config, port, projectDir)
	}

	cmd := exec.Command(processComposeConfig.BinPath, flags...)
	return runProcessManagerInForeground(cmd, config, port, projectDir, w)
}

func runProcessManagerInForeground(cmd *exec.Cmd, config *globalProcessComposeConfig, port int, projectDir string, w io.Writer) error {
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start process-compose: %w", err)
	}

	projectConfig := instance{
		Pid:  cmd.Process.Pid,
		Port: port,
	}

	config.Instances[projectDir] = projectConfig

	err := writeGlobalProcessComposeJSON(config, config.File)
	if err != nil {
		return err
	}

	// We're waiting now, so we can unlock the file
	config.File.Close()

	err = cmd.Wait()
	if err != nil {
		if err.Error() == "exit status 1" {
			fmt.Fprintf(w, "Process-compose was terminated remotely, %s\n", err.Error())
			return nil
		}
		return err
	}

	configFile, err := openGlobalConfigFile()
	if err != nil {
		return err
	}

	config = readGlobalProcessComposeJSON(configFile)

	delete(config.Instances, projectDir)
	return writeGlobalProcessComposeJSON(config, configFile)
}

func runProcessManagerInBackground(cmd *exec.Cmd, config *globalProcessComposeConfig, port int, projectDir string) error {
	logdir := filepath.Join(projectDir, processComposeLogfile)
	logfile, err := os.OpenFile(logdir, os.O_CREATE|os.O_WRONLY|os.O_APPEND|os.O_TRUNC, 0o664)
	if err != nil {
		return fmt.Errorf("failed to open process-compose log file: %w", err)
	}

	cmd.Stdout = logfile
	cmd.Stderr = logfile

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start process-compose: %w", err)
	}

	projectConfig := instance{
		Pid:  cmd.Process.Pid,
		Port: port,
	}

	config.Instances[projectDir] = projectConfig

	err = writeGlobalProcessComposeJSON(config, config.File)
	if err != nil {
		return fmt.Errorf("failed to write global process-compose config: %w", err)
	}

	return nil
}

func StopProcessManager(ctx context.Context, projectDir string, w io.Writer) error {
	configFile, err := openGlobalConfigFile()
	if err != nil {
		return err
	}

	defer configFile.Close()

	config := readGlobalProcessComposeJSON(configFile)

	project, ok := config.Instances[projectDir]
	if !ok {
		return fmt.Errorf("process-compose is not running or it's config is missing. To start it, run `devbox services up`")
	}

	defer func() {
		delete(config.Instances, projectDir)
		err = writeGlobalProcessComposeJSON(config, configFile)
	}()

	pid, _ := os.FindProcess(project.Pid)
	err = pid.Signal(os.Interrupt)
	if err != nil {
		return fmt.Errorf("process-compose is not running. To start it, run `devbox services up`")
	}

	fmt.Fprintf(w, "Process-compose stopped successfully.\n")
	return nil
}

func StopAllProcessManagers(ctx context.Context, w io.Writer) error {
	configFile, err := openGlobalConfigFile()
	if err != nil {
		return err
	}

	defer configFile.Close()

	config := readGlobalProcessComposeJSON(configFile)

	for _, project := range config.Instances {
		pid, _ := os.FindProcess(project.Pid)
		err := pid.Signal(os.Interrupt)
		if err != nil {
			fmt.Printf("process-compose is not running. To start it, run `devbox services up`")
		}
	}

	config.Instances = make(map[string]instance)

	err = writeGlobalProcessComposeJSON(config, configFile)
	if err != nil {
		return fmt.Errorf("failed to write global process-compose config: %w", err)
	}

	return nil
}

func ProcessManagerIsRunning(projectDir string) bool {
	configFile, err := openGlobalConfigFile()
	if err != nil {
		return false
	}

	defer configFile.Close()

	config := readGlobalProcessComposeJSON(configFile)

	project, ok := config.Instances[projectDir]
	if !ok {
		return false
	}

	process, _ := os.FindProcess(project.Pid)

	err = process.Signal(syscall.Signal(0))
	if err != nil {
		delete(config.Instances, projectDir)
		_ = writeGlobalProcessComposeJSON(config, configFile)
		return false
	}
	return true
}

func GetProcessManagerPort(projectDir string) (int, error) {
	configFile, err := openGlobalConfigFile()
	if err != nil {
		return 0, err
	}

	config := readGlobalProcessComposeJSON(configFile)

	project, ok := config.Instances[projectDir]
	if !ok {
		return 0, usererr.WithUserMessage(fmt.Errorf("failed to find projectDir %s in config.Instances", projectDir), "process-compose is not running or it's config is missing. To start it, run `devbox services up`")
	}

	return project.Port, nil
}

func lockFile(file *os.File) error {
	lockResult := make(chan error)

	go func() {
		err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX)
		lockResult <- err
	}()

	select {
	case err := <-lockResult:
		if err != nil {
			file.Close()
			return fmt.Errorf("failed to lock file: %w", err)
		}
		return nil

	case <-time.After(fileLockTimeout):
		file.Close()
		return fmt.Errorf("process-compose file lock timed out after %d seconds", fileLockTimeout/time.Second)
	}
}
