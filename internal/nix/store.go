package nix

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"

	"github.com/pkg/errors"
)

func StorePath(hash, name, version string) string {
	storeDirParts := []string{hash, name}
	if version != "" {
		storeDirParts = append(storeDirParts, version)
	}
	storeDir := strings.Join(storeDirParts, "-")
	return filepath.Join("/nix/store", storeDir)
}

// ContentAddressedStorePath takes a store path and returns the content-addressed store path.
func ContentAddressedStorePath(storePath string) (string, error) {
	cmd := Command(context.TODO(), "store", "make-content-addressed", "--json", storePath)
	out, err := cmd.Output()
	if err != nil {
		return "", errors.WithStack(err)
	}
	// Example Output:
	// > nix store make-content-addressed /nix/store/r2jd6ygnmirm2g803mksqqjm4y39yi6i-git-2.33.1 --json
	// {"rewrites":{"/nix/store/r2jd6ygnmirm2g803mksqqjm4y39yi6i-git-2.33.1":"/nix/store/ldbhlwhh39wha58rm61bkiiwm6j7211j-git-2.33.1"}}

	type ContentAddressed struct {
		Rewrites map[string]string `json:"rewrites"`
	}
	caOutput := ContentAddressed{}
	if err := json.Unmarshal(out, &caOutput); err != nil {
		return "", errors.WithStack(err)
	}

	caStorePath, ok := caOutput.Rewrites[storePath]
	if !ok {
		return "", errors.Errorf("could not find content-addressed store path for %s", storePath)
	}
	return caStorePath, nil
}
