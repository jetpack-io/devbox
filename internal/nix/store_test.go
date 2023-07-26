package nix

import (
	"fmt"
	"strings"
	"testing"

	"golang.org/x/exp/slices"
)

func TestContentAddressedPath(t *testing.T) {
	testCases := []struct {
		storePath string
		expected  []string
	}{
		{
			"/nix/store/r2jd6ygnmirm2g803mksqqjm4y39yi6i-git-2.33.1",
			[]string{
				"/nix/store/ldbhlwhh39wha58rm61bkiiwm6j7211j-git-2.33.1",
				"/nix/store/d49wyvsz5nkqa23qp4p0ikr04mw9n4h9-git-2.33.1",
			},
		},
	}

	for index, testCase := range testCases {
		t.Run(fmt.Sprintf("%d", index), func(t *testing.T) {
			out, err := ContentAddressedStorePath(testCase.storePath)
			if err != nil {
				t.Errorf("got error: %v", err)
			}
			if !slices.Contains(testCase.expected, out) {
				t.Errorf("got %q, want any of:\n%s", out, strings.Join(testCase.expected, "\n"))
			}
		})
	}
}
