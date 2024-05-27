package nix

import (
	"slices"
	"testing"

	"golang.org/x/exp/maps"
)

func TestParseStorePathFromInstallableOutput(t *testing.T) {
	testCases := []struct {
		name     string
		input    string
		expected []string
	}{
		{
			name: "go-basic-nix-2-20-1",
			// snipped the actual output for brevity. We mainly care about the first key in the JSON.
			input:    `{"/nix/store/fgkl3qk8p5hnd07b0dhzfky3ys5gxjmq-go-1.22.0":{"deriver":"/nix/store/clr3bm8njqysvyw4r4x4xmldhz4knrff-go-1.22.0.drv"}}`,
			expected: []string{"/nix/store/fgkl3qk8p5hnd07b0dhzfky3ys5gxjmq-go-1.22.0"},
		},
		{
			name:     "go-basic-nix-2-17-0",
			input:    `[{"path":"/nix/store/fgkl3qk8p5hnd07b0dhzfky3ys5gxjmq-go-1.22.0","valid":true}]`,
			expected: []string{"/nix/store/fgkl3qk8p5hnd07b0dhzfky3ys5gxjmq-go-1.22.0"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			actual, err := parseStorePathFromInstallableOutput([]byte(tc.input))
			if err != nil {
				t.Errorf("Expected no error but got error: %s", err)
			}
			if !slices.Equal(tc.expected, maps.Keys(actual)) {
				t.Errorf("Expected store path %s but got %s", tc.expected, actual)
			}
		})
	}
}
