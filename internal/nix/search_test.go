package nix

import (
	"context"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestSearch(t *testing.T) {
	index, err := IndexPackages(context.Background(), "017fd895276dc0e45e9a596b1aa1ad199bfc7c4d")
	if err != nil {
		t.Fatal(err)
	}

	gotPkgs := map[string]bool{
		"python":    false,
		"python2":   false,
		"python3":   false,
		"python310": false,
	}
	results := index.Search("python")
	for i, pkg := range results {
		if _, ok := gotPkgs[pkg.AttrPath.String()]; ok {
			gotPkgs[pkg.AttrPath.String()] = true
		}
		t.Logf("Result %d:\nAttrPath = %s:\nName     = %s\nPname    = %s\nVersion  = %s\nTokens   = %s",
			i+1, pkg.AttrPath.String(), pkg.RawName, pkg.Pname, pkg.RawVersion, pkg.AttrPath.tokens)
	}
	for pkg, found := range gotPkgs {
		if !found {
			t.Errorf("package attribute path %q is missing from the results", pkg)
		}
	}
}

func TestParseAttrPath(t *testing.T) {
	tests := []struct {
		in   string
		want []string
	}{
		{"go", []string{"go"}},
		{"go-1", []string{"go", "1"}},
		{"go--1", []string{"go", "1"}},
		{"go-_ .1", []string{"go", "1"}},
		{"_go", []string{"go"}},
		{"goGo", []string{"go", "Go"}},
		{"goGoGoG", []string{"go", "Go", "Go", "G"}},
		{"go1.19", []string{"go", "1", "19"}},
		{"go 1.19", []string{"go", "1", "19"}},
		{"go119", []string{"go", "119"}},
	}
	for _, test := range tests {
		t.Run(test.in, func(t *testing.T) {
			got := ParseAttrPath(test.in).tokens
			if diff := cmp.Diff(test.want, got); diff != "" {
				t.Errorf("wrong tokens (-want +got):\n%s", diff)
			}
		})
	}
}
