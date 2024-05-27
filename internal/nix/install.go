// Copyright 2024 Jetify Inc. and contributors. All rights reserved.
// Use of this source code is governed by the license in the LICENSE file.

package nix

import (
	"bytes"
	_ "embed"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/fatih/color"
	"github.com/mattn/go-isatty"
	"github.com/pkg/errors"

	"go.jetpack.io/devbox/internal/boxcli/usererr"
	"go.jetpack.io/devbox/internal/build"
	"go.jetpack.io/devbox/internal/cmdutil"
	"go.jetpack.io/devbox/internal/fileutil"
	"go.jetpack.io/devbox/internal/redact"
	"go.jetpack.io/devbox/internal/ux"
)

const rootError = "warning: installing Nix as root is not supported by this script!"

// Install runs the install script for Nix. daemon has 3 states
// nil is unset. false is --no-daemon. true is --daemon.
func Install(writer io.Writer, daemon *bool) error {
	if isRoot() && build.OS() == build.OSWSL {
		return usererr.New("Nix cannot be installed as root on WSL. Please run as a normal user with sudo access.")
	}
	r, w, err := os.Pipe()
	if err != nil {
		return errors.WithStack(err)
	}
	defer r.Close()

	installScript := "curl -L https://releases.nixos.org/nix/nix-2.18.1/install | sh -s"
	if daemon != nil {
		if *daemon {
			installScript += " -- --daemon"
		} else {
			installScript += " -- --no-daemon"
		}
	}

	fmt.Fprintf(writer, "Installing nix with: %s\nThis may require sudo access.\n", installScript)

	cmd := exec.Command("sh", "-c", installScript)
	// Attach stdout but no stdin. This makes the command run in non-TTY mode
	// which skips the interactive prompts.
	// We could attach stderr? but the stdout prompt is pretty useful.
	cmd.Stdin = nil
	cmd.Stdout = w
	cmd.Stderr = w

	err = cmd.Start()
	w.Close()
	if err != nil {
		return errors.WithStack(err)
	}

	done := make(chan struct{})
	go func() {
		var buf bytes.Buffer
		_, err := io.Copy(io.MultiWriter(&buf, os.Stdout), r)
		if err != nil {
			fmt.Fprintln(writer, err)
		}

		if strings.Contains(buf.String(), rootError) {
			ux.Finfo(
				writer,
				"If installing nix as root, consider using the --daemon flag to install in multi-user mode.\n",
			)
		}
		close(done)
	}()

	<-done
	return errors.WithStack(cmd.Wait())
}

func BinaryInstalled() bool {
	return cmdutil.Exists("nix")
}

func dirExists() bool {
	return fileutil.Exists("/nix")
}

func isRoot() bool {
	return os.Geteuid() == 0
}

var ensured = false

func Ensured() bool {
	return ensured
}

func EnsureNixInstalled(writer io.Writer, withDaemonFunc func() *bool) (err error) {
	ensured = true
	defer func() {
		if err != nil {
			return
		}

		var version VersionInfo
		version, err = Version()
		if err != nil {
			err = redact.Errorf("nix: ensure install: get version: %w", err)
			return
		}

		// ensure minimum nix version installed
		if !version.AtLeast(MinVersion) {
			err = usererr.New(
				"Devbox requires nix of version >= %s. Your version is %s. "+
					"Please upgrade nix and try again.\n",
				MinVersion,
				version,
			)
			return
		}
		// call ComputeSystem to ensure its value is internally cached so other
		// callers can rely on just calling System
		err = ComputeSystem()
	}()

	if BinaryInstalled() {
		return nil
	}
	if dirExists() {
		if err = SourceNixEnv(); err != nil {
			return err
		} else if BinaryInstalled() {
			return nil
		}

		return usererr.New(
			"We found a /nix directory but nix binary is not in your PATH and we " +
				"were not able to find it in the usual locations. Your nix installation " +
				"might be broken. If restarting your terminal or reinstalling nix " +
				"doesn't work, please create an issue at " +
				"https://github.com/jetify-com/devbox/issues",
		)
	}

	color.Yellow("\nNix is not installed. Devbox will attempt to install it.\n\n")

	if isatty.IsTerminal(os.Stdout.Fd()) {
		color.Yellow("Press enter to continue or ctrl-c to exit.\n")
		fmt.Scanln()
	}

	if err = Install(writer, withDaemonFunc()); err != nil {
		return err
	}

	// Source again
	if err = SourceNixEnv(); err != nil {
		return err
	}

	fmt.Fprintln(writer, "Nix installed successfully. Devbox is ready to use!")
	return nil
}
