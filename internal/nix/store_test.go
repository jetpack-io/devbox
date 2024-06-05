package nix

import (
	"testing"

	"golang.org/x/exp/maps"
)

func TestParseStorePathFromInstallableOutput(t *testing.T) {
	testCases := []struct {
		name     string
		input    string
		expected map[string]bool
	}{
		{
			name: "go-basic-nix-2-20-1",
			// snipped the actual output for brevity. We mainly care about the first key in the JSON.
			input: `{"/nix/store/fgkl3qk8p5hnd07b0dhzfky3ys5gxjmq-go-1.22.0":{"deriver":"/nix/store/clr3bm8njqysvyw4r4x4xmldhz4knrff-go-1.22.0.drv"}}`,
			expected: map[string]bool{
				"/nix/store/fgkl3qk8p5hnd07b0dhzfky3ys5gxjmq-go-1.22.0": true,
			},
		},
		{
			name: "go-basic-nix-2-20-1",
			// snipped the actual output for brevity. We mainly care about the first key in the JSON.
			input: `{"/nix/store/fgkl3qk8p5hnd07b0dhzfky3ys5gxjmq-go-1.22.0":null}`,
			expected: map[string]bool{
				"/nix/store/fgkl3qk8p5hnd07b0dhzfky3ys5gxjmq-go-1.22.0": false,
			},
		},
		{
			name:  "go-basic-nix-2-17-0",
			input: `[{"path":"/nix/store/fgkl3qk8p5hnd07b0dhzfky3ys5gxjmq-go-1.22.0"}]`,
			expected: map[string]bool{
				"/nix/store/fgkl3qk8p5hnd07b0dhzfky3ys5gxjmq-go-1.22.0": false,
			},
		},
		{
			name:  "go-basic-nix-2-17-0",
			input: `[{"path":"/nix/store/fgkl3qk8p5hnd07b0dhzfky3ys5gxjmq-go-1.22.0", "valid": true}]`,
			expected: map[string]bool{
				"/nix/store/fgkl3qk8p5hnd07b0dhzfky3ys5gxjmq-go-1.22.0": true,
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			actual, err := parseStorePathFromInstallableOutput([]byte(tc.input))
			if err != nil {
				t.Errorf("Expected no error but got error: %s", err)
			}
			if !maps.Equal(tc.expected, actual) {
				t.Errorf("Expected store path %v but got %v", tc.expected, actual)
			}
		})
	}
}
