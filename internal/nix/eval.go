package nix

import (
	"context"
	"encoding/json"
	"os"
	"strconv"
)

func EvalPackageName(path string) (string, error) {
	cmd := Command(context.TODO(), "eval", "--raw", path+".name")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// PackageIsInsecure is a fun little nix eval that maybe works.
func PackageIsInsecure(path string) bool {
	cmd := Command(context.TODO(), "eval", path+".meta.insecure")
	out, err := cmd.Output()
	if err != nil {
		// We can't know for sure, but probably not.
		return false
	}
	var insecure bool
	if err := json.Unmarshal(out, &insecure); err != nil {
		// We can't know for sure, but probably not.
		return false
	}
	return insecure
}

func PackageKnownVulnerabilities(path string) []string {
	cmd := Command(context.TODO(), "eval", path+".meta.knownVulnerabilities")
	out, err := cmd.Output()
	if err != nil {
		// We can't know for sure, but probably not.
		return nil
	}
	var vulnerabilities []string
	if err := json.Unmarshal(out, &vulnerabilities); err != nil {
		// We can't know for sure, but probably not.
		return nil
	}
	return vulnerabilities
}

// Eval is raw nix eval. Needs to be parsed. Useful for stuff like
// nix eval --raw nixpkgs/9ef09e06806e79e32e30d17aee6879d69c011037#fuse3
// to determine if a package if a package can be installed in system.
func Eval(path string) ([]byte, error) {
	cmd := Command(context.TODO(), "eval", "--raw", path)
	return cmd.CombinedOutput()
}

func AllowInsecurePackages() {
	os.Setenv("NIXPKGS_ALLOW_INSECURE", "1")
}

func IsInsecureAllowed() bool {
	allowed, _ := strconv.ParseBool(os.Getenv("NIXPKGS_ALLOW_INSECURE"))
	return allowed
}
