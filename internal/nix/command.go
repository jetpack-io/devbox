package nix

import (
	"context"
	"os"
	"os/exec"
	"time"

	"go.jetpack.io/devbox/internal/boxcli/featureflag"
)

func Command(ctx context.Context, arg ...string) *exec.Cmd {
	exp := "ca-derivations flakes nix-command"
	if featureflag.RemoveNixpkgs.Enabled() {
		exp += " fetch-closure"
	}
	cmd := exec.CommandContext(ctx, "nix", append([]string{"--extra-experimental-features", exp}, arg...)...)
	cmd.Cancel = func() error { return cmd.Process.Signal(os.Interrupt) }
	cmd.WaitDelay = 5 * time.Second
	return cmd
}

func allowUnfreeEnv(curEnv []string) []string {
	return append(curEnv, "NIXPKGS_ALLOW_UNFREE=1")
}

func allowInsecureEnv(curEnv []string) []string {
	return append(curEnv, "NIXPKGS_ALLOW_INSECURE=1")
}
