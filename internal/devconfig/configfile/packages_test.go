package configfile

import (
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/tailscale/hujson"
	"go.jetpack.io/devbox/nix/flake"
	"go.jetpack.io/pkg/runx/impl/types"
)

// TestJsonifyConfigPackages tests the jsonMarshal and jsonUnmarshal of the Config.Packages field
func TestJsonifyConfigPackages(t *testing.T) {
	testCases := []struct {
		name       string
		jsonConfig string
		expected   PackagesMutator
	}{
		{
			name:       "empty-list",
			jsonConfig: `{"packages":[]}`,
			expected: PackagesMutator{
				collection: []Package{},
			},
		},
		{
			name:       "empty-map",
			jsonConfig: `{"packages":{}}`,
			expected: PackagesMutator{
				collection: []Package{},
			},
		},
		{
			name:       "flat-list",
			jsonConfig: `{"packages":["python","hello@latest","go@1.20"]}`,
			expected: PackagesMutator{
				collection: packagesFromLegacyList([]string{"python", "hello@latest", "go@1.20"}),
			},
		},
		{
			name:       "map-with-string-value",
			jsonConfig: `{"packages":{"python":"latest","go":"1.20"}}`,
			expected: PackagesMutator{
				collection: []Package{
					NewVersionOnlyPackage("python", "latest"),
					NewVersionOnlyPackage("go", "1.20"),
				},
			},
		},

		{
			name:       "map-with-struct-value",
			jsonConfig: `{"packages":{"python":{"version":"latest"}}}`,
			expected: PackagesMutator{
				collection: []Package{
					{
						Name:    "python",
						Version: "latest",
					},
				},
			},
		},
		{
			name:       "map-with-string-and-struct-values",
			jsonConfig: `{"packages":{"go":"1.20","emacs":"latest","python":{"version":"latest"}}}`,
			expected: PackagesMutator{
				collection: []Package{
					NewVersionOnlyPackage("go", "1.20"),
					NewVersionOnlyPackage("emacs", "latest"),
					{
						Name:    "python",
						Version: "latest",
					},
				},
			},
		},
		{
			name: "map-with-platforms",
			jsonConfig: `{"packages":{"python":{"version":"latest",` +
				`"platforms":["x86_64-darwin","aarch64-linux"]}}}`,
			expected: PackagesMutator{
				collection: []Package{
					{
						Name:      "python",
						Version:   "latest",
						Platforms: []string{"x86_64-darwin", "aarch64-linux"},
					},
				},
			},
		},
		{
			name: "map-with-excluded-platforms",
			jsonConfig: `{"packages":{"python":{"version":"latest",` +
				`"excluded_platforms":["x86_64-linux"]}}}`,
			expected: PackagesMutator{
				collection: []Package{
					{
						Name:              "python",
						Version:           "latest",
						ExcludedPlatforms: []string{"x86_64-linux"},
					},
				},
			},
		},
		{
			name: "map-with-platforms-and-excluded-platforms",
			jsonConfig: `{"packages":{"python":{"version":"latest",` +
				`"platforms":["x86_64-darwin","aarch64-linux"],` +
				`"excluded_platforms":["x86_64-linux"]}}}`,
			expected: PackagesMutator{
				collection: []Package{
					{
						Name:              "python",
						Version:           "latest",
						Platforms:         []string{"x86_64-darwin", "aarch64-linux"},
						ExcludedPlatforms: []string{"x86_64-linux"},
					},
				},
			},
		},
		{
			name: "map-with-platforms-and-excluded-platforms-local-flake",
			jsonConfig: `{"packages":{"path:my-php-flake#hello":{"version":"latest",` +
				`"platforms":["x86_64-darwin","aarch64-linux"],` +
				`"excluded_platforms":["x86_64-linux"]}}}`,
			expected: PackagesMutator{
				collection: []Package{
					{
						Name:              "path:my-php-flake#hello",
						Version:           "latest",
						Platforms:         []string{"x86_64-darwin", "aarch64-linux"},
						ExcludedPlatforms: []string{"x86_64-linux"},
					},
				},
			},
		},
		{
			name: "map-with-platforms-and-excluded-platforms-remote-flake",
			jsonConfig: `{"packages":{"github:F1bonacc1/process-compose/v0.43.1":` +
				`{"version":"latest",` +
				`"platforms":["x86_64-darwin","aarch64-linux"],` +
				`"excluded_platforms":["x86_64-linux"]}}}`,
			expected: PackagesMutator{
				collection: []Package{
					{
						Name:              "github:F1bonacc1/process-compose/v0.43.1",
						Version:           "latest",
						Platforms:         []string{"x86_64-darwin", "aarch64-linux"},
						ExcludedPlatforms: []string{"x86_64-linux"},
					},
				},
			},
		},
		{
			name: "map-with-platforms-and-excluded-platforms-nixpkgs-reference",
			jsonConfig: `{"packages":{"github:nixos/nixpkgs/5233fd2ba76a3accb5aaa999c00509a11fd0793c#hello":` +
				`{"version":"latest",` +
				`"platforms":["x86_64-darwin","aarch64-linux"],` +
				`"excluded_platforms":["x86_64-linux"]}}}`,
			expected: PackagesMutator{
				collection: []Package{
					{
						Name:              "github:nixos/nixpkgs/5233fd2ba76a3accb5aaa999c00509a11fd0793c#hello",
						Version:           "latest",
						Platforms:         []string{"x86_64-darwin", "aarch64-linux"},
						ExcludedPlatforms: []string{"x86_64-linux"},
					},
				},
			},
		},
		{
			name: "map-with-platforms-and-excluded-platforms-and-outputs-nixpkgs-reference",
			jsonConfig: `{"packages":{"github:nixos/nixpkgs/5233fd2ba76a3accb5aaa999c00509a11fd0793c#hello":` +
				`{"version":"latest",` +
				`"platforms":["x86_64-darwin","aarch64-linux"],` +
				`"excluded_platforms":["x86_64-linux"],` +
				`"outputs":["cli"]` +
				`}}}`,
			expected: PackagesMutator{
				collection: []Package{
					{
						Name:              "github:nixos/nixpkgs/5233fd2ba76a3accb5aaa999c00509a11fd0793c#hello",
						Version:           "latest",
						Platforms:         []string{"x86_64-darwin", "aarch64-linux"},
						ExcludedPlatforms: []string{"x86_64-linux"},
						Outputs:           []string{"cli"},
					},
				},
			},
		},
		{
			name: "map-with-allow-insecure-nixpkgs-reference",
			jsonConfig: `{"packages":{"github:nixos/nixpkgs/5233fd2ba76a3accb5aaa999c00509a11fd0793c#python":` +
				`{"version":"2.7",` +
				`"allow_insecure":["python-2.7.18.1"]` +
				`}}}`,

			expected: PackagesMutator{
				collection: []Package{
					{
						Name:          "github:nixos/nixpkgs/5233fd2ba76a3accb5aaa999c00509a11fd0793c#python",
						Version:       "2.7",
						AllowInsecure: []string{"python-2.7.18.1"},
					},
				},
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			config, err := LoadBytes([]byte(testCase.jsonConfig))
			if err != nil {
				t.Errorf("load error: %v", err)
			}
			if diff := diffPackages(t, config.PackagesMutator, testCase.expected); diff != "" {
				t.Errorf("got wrong packages (-want +got):\n%s", diff)
			}

			got, err := hujson.Minimize(config.Bytes())
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != testCase.jsonConfig {
				t.Errorf("expected: %v, got: %v", testCase.jsonConfig, string(got))
			}
		})
	}
}

func diffPackages(t *testing.T, got, want PackagesMutator) string {
	t.Helper()

	return cmp.Diff(want, got, cmpopts.IgnoreUnexported(PackagesMutator{}, Package{}))
}

func TestParseVersionedName(t *testing.T) {
	testCases := []struct {
		name            string
		input           string
		expectedName    string
		expectedVersion string
	}{
		{
			name:            "no-version",
			input:           "python",
			expectedName:    "python",
			expectedVersion: "",
		},
		{
			name:            "with-version-latest",
			input:           "python@latest",
			expectedName:    "python",
			expectedVersion: "latest",
		},
		{
			name:            "with-version",
			input:           "python@1.2.3",
			expectedName:    "python",
			expectedVersion: "1.2.3",
		},
		{
			name:            "with-two-@-signs",
			input:           "emacsPackages.@@latest",
			expectedName:    "emacsPackages.@",
			expectedVersion: "latest",
		},
		{
			name:            "with-trailing-@-sign",
			input:           "emacsPackages.@",
			expectedName:    "emacsPackages.@",
			expectedVersion: "",
		},
		{
			name:            "local-flake",
			input:           "path:my-php-flake#hello",
			expectedName:    "path:my-php-flake#hello",
			expectedVersion: "",
		},
		{
			name:            "remote-flake",
			input:           "github:F1bonacc1/process-compose/v0.43.1",
			expectedName:    "github:F1bonacc1/process-compose/v0.43.1",
			expectedVersion: "",
		},
		{
			name:            "nixpkgs-reference",
			input:           "github:nixos/nixpkgs/5233fd2ba76a3accb5aaa999c00509a11fd0793c#hello",
			expectedName:    "github:nixos/nixpkgs/5233fd2ba76a3accb5aaa999c00509a11fd0793c#hello",
			expectedVersion: "",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			name, version := parseVersionedName(testCase.input)
			if name != testCase.expectedName {
				t.Errorf("expected: %v, got: %v", testCase.expectedName, name)
			}
			if version != testCase.expectedVersion {
				t.Errorf("expected: %v, got: %v", testCase.expectedVersion, version)
			}
		})
	}
}

func TestParsePackageSpec(t *testing.T) {
	cases := []struct {
		in   string
		want PackageSpec
	}{
		{in: "", want: PackageSpec{}},
		{in: "mail:nixpkgs#go", want: PackageSpec{}},

		// Common name@version strings.
		{
			in: "go", want: PackageSpec{
				Name: "go", Version: "latest",
				Installable:         mustFlake(t, "flake:go"),
				AttrPathInstallable: mustFlake(t, "nixpkgs#go"),
			},
		},
		{
			in: "go@latest", want: PackageSpec{
				Name: "go", Version: "latest",
				Installable:         mustFlake(t, "flake:go@latest"),
				AttrPathInstallable: mustFlake(t, "nixpkgs#go@latest"),
			},
		},
		{
			in: "go@1.22.0", want: PackageSpec{
				Name: "go", Version: "1.22.0",
				Installable:         mustFlake(t, "flake:go@1.22.0"),
				AttrPathInstallable: mustFlake(t, "nixpkgs#go@1.22.0"),
			},
		},

		// name@version splitting edge-cases.
		{
			in: "emacsPackages.@@latest", want: PackageSpec{
				Name: "emacsPackages.@", Version: "latest",
				Installable:         mustFlake(t, "flake:emacsPackages.@@latest"),
				AttrPathInstallable: mustFlake(t, "nixpkgs#emacsPackages.@@latest"),
			},
		},
		{
			in: "emacsPackages.@", want: PackageSpec{
				Name: "emacsPackages.@", Version: "latest",
				Installable:         mustFlake(t, "flake:emacsPackages.@"),
				AttrPathInstallable: mustFlake(t, "nixpkgs#emacsPackages.@"),
			},
		},
		{
			in: "@angular/cli", want: PackageSpec{
				Name: "@angular/cli", Version: "latest",
				Installable:         mustFlake(t, "flake:@angular/cli"),
				AttrPathInstallable: mustFlake(t, "nixpkgs#@angular/cli"),
			},
		},
		{
			in: "nodePackages.@angular/cli", want: PackageSpec{
				Name: "nodePackages.", Version: "angular/cli",
				Installable:         mustFlake(t, "flake:nodePackages.@angular/cli"),
				AttrPathInstallable: mustFlake(t, "nixpkgs#nodePackages.@angular/cli"),
			},
		},

		// Flake installables.
		{
			in:   "nixpkgs#go",
			want: PackageSpec{Installable: mustFlake(t, "flake:nixpkgs#go")},
		},
		{
			in:   "flake:nixpkgs",
			want: PackageSpec{Installable: mustFlake(t, "flake:nixpkgs")},
		},
		{
			in:   "flake:nixpkgs#go",
			want: PackageSpec{Installable: mustFlake(t, "flake:nixpkgs#go")},
		},
		{
			in:   "./my-php-flake",
			want: PackageSpec{Installable: mustFlake(t, "path:./my-php-flake")},
		},
		{
			in:   "./my-php-flake#hello",
			want: PackageSpec{Installable: mustFlake(t, "path:./my-php-flake#hello")},
		},
		{
			in:   "/my-php-flake",
			want: PackageSpec{Installable: mustFlake(t, "path:/my-php-flake")},
		},
		{
			in:   "/my-php-flake#hello",
			want: PackageSpec{Installable: mustFlake(t, "path:/my-php-flake#hello")},
		},
		{
			in:   "path:my-php-flake",
			want: PackageSpec{Installable: mustFlake(t, "path:my-php-flake")},
		},
		{
			in:   "path:my-php-flake#hello",
			want: PackageSpec{Installable: mustFlake(t, "path:my-php-flake#hello")},
		},
		{
			in:   "github:F1bonacc1/process-compose/v0.43.1",
			want: PackageSpec{Installable: mustFlake(t, "github:F1bonacc1/process-compose/v0.43.1")},
		},
		{
			in:   "github:nixos/nixpkgs/5233fd2ba76a3accb5aaa999c00509a11fd0793c#hello",
			want: PackageSpec{Installable: mustFlake(t, "github:nixos/nixpkgs/5233fd2ba76a3accb5aaa999c00509a11fd0793c#hello")},
		},
		{
			in: "mail:nixpkgs",
			want: PackageSpec{
				Name: "mail:nixpkgs", Version: "latest",
				AttrPathInstallable: mustFlake(t, "nixpkgs#mail:nixpkgs"),
			},
		},

		// RunX
		{
			in: "runx:golangci/golangci-lint", want: PackageSpec{
				RunX: types.PkgRef{
					Owner:   "golangci",
					Repo:    "golangci-lint",
					Version: "latest",
				},
			},
		},
		{
			in: "runx:golangci/golangci-lint@1.2.3", want: PackageSpec{
				RunX: types.PkgRef{
					Owner:   "golangci",
					Repo:    "golangci-lint",
					Version: "1.2.3",
				},
			},
		},

		// RunX missing scheme.
		{
			in: "golangci/golangci-lint", want: PackageSpec{
				Name: "golangci/golangci-lint", Version: "latest",
				Installable:         mustFlake(t, "flake:golangci/golangci-lint"),
				AttrPathInstallable: mustFlake(t, "nixpkgs#golangci/golangci-lint"),
			},
		},
		{
			in: "golangci/golangci-lint@1.2.3", want: PackageSpec{
				Name: "golangci/golangci-lint", Version: "1.2.3",
				Installable:         mustFlake(t, "flake:golangci/golangci-lint@1.2.3"),
				AttrPathInstallable: mustFlake(t, "nixpkgs#golangci/golangci-lint@1.2.3"),
			},
		},
	}

	for _, tc := range cases {
		t.Run(fmt.Sprintf("in=%s", tc.in), func(t *testing.T) {
			got := ParsePackageSpec(tc.in, "")
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("wrong PackageSpec for %q (-want +got):\n%s", tc.in, diff)
			}
		})
	}
}

// TestParseDeprecatedPackageSpec tests parsing behavior when the deprecated
// nixpkgs.commit field is set to snixpkgs-unstable. It's split into a separate
// test in case we ever drop support for nixpkgs.commit entirely.
func TestParseDeprecatedPackageSpec(t *testing.T) {
	nixpkgsCommit := flake.Ref{Type: flake.TypeIndirect, ID: "nixpkgs", Ref: "nixpkgs-unstable"}
	cases := []struct {
		in   string
		want PackageSpec
	}{
		{in: "", want: PackageSpec{}},

		// Parses Devbox package when @version specified.
		{
			in: "go@latest", want: PackageSpec{
				Name: "go", Version: "latest",
				AttrPathInstallable: mustFlake(t, "nixpkgs/nixpkgs-unstable#go@latest"),
			},
		},
		{
			in: "go@1.22.0", want: PackageSpec{
				Name: "go", Version: "1.22.0",
				AttrPathInstallable: mustFlake(t, "nixpkgs/nixpkgs-unstable#go@1.22.0"),
			},
		},

		// Missing @version does not imply @latest and is not a flake reference.
		{in: "go", want: PackageSpec{AttrPathInstallable: mustFlake(t, "nixpkgs/nixpkgs-unstable#go")}},
		{in: "cachix", want: PackageSpec{AttrPathInstallable: mustFlake(t, "nixpkgs/nixpkgs-unstable#cachix")}},

		// Unambiguous flake reference should not be parsed as an attribute path.
		{in: "flake:cachix", want: PackageSpec{Installable: mustFlake(t, "flake:cachix#")}},
		{in: "./flake", want: PackageSpec{Installable: mustFlake(t, "path:./flake")}},
		{in: "path:flake", want: PackageSpec{Installable: mustFlake(t, "path:flake")}},
		{in: "nixpkgs#go", want: PackageSpec{Installable: mustFlake(t, "nixpkgs#go")}},
		{in: "nixpkgs/branch#go", want: PackageSpec{Installable: mustFlake(t, "nixpkgs/branch#go")}},

		// // RunX unaffected by nixpkgs.commit.
		{in: "runx:golangci/golangci-lint", want: PackageSpec{RunX: types.PkgRef{Owner: "golangci", Repo: "golangci-lint", Version: "latest"}}},
	}

	for _, tc := range cases {
		t.Run(fmt.Sprintf("in=%s", tc.in), func(t *testing.T) {
			got := ParsePackageSpec(tc.in, nixpkgsCommit.Ref)
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("wrong PackageSpec for %q (-want +got):\n%s", tc.in, diff)
			}
		})
	}
}

// mustFlake parses s as a [flake.Installable] and fails the test if there's an
// error. It allows using the string form of a flake in test cases so they're
// easier to read.
func mustFlake(t *testing.T, s string) flake.Installable {
	t.Helper()
	i, err := flake.ParseInstallable(s)
	if err != nil {
		t.Fatal("error parsing wanted flake installable:", err)
	}
	return i
}
