// Copyright 2024 Jetify Inc. and contributors. All rights reserved.
// Use of this source code is governed by the license in the LICENSE file.

package boxcli

import (
	"fmt"
	"io"
	"math"
	"strings"

	"github.com/samber/lo"
	"github.com/spf13/cobra"

	"go.jetpack.io/devbox/internal/boxcli/usererr"
	"go.jetpack.io/devbox/internal/searcher"
	"go.jetpack.io/devbox/internal/ux"
)

const trimmedVersionsLength = 10

type searchCmdFlags struct {
	showAll bool
}

func searchCmd() *cobra.Command {
	flags := &searchCmdFlags{}
	command := &cobra.Command{
		Use:   "search <pkg>",
		Short: "Search for nix packages",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			query := args[0]
			name, version, isVersioned := searcher.ParseVersionedPackage(query)
			if !isVersioned {
				results, err := searcher.Client().Search(query)
				if err != nil {
					return err
				}
				return printSearchResults(
					cmd.OutOrStdout(), query, results, flags.showAll)
			}
			packageVersion, err := searcher.Client().Resolve(name, version)
			if err != nil {
				// This is not ideal. Search service should return valid response we
				// can parse
				return usererr.WithUserMessage(err, "No results found for %q\n", query)
			}
			fmt.Fprintf(
				cmd.OutOrStdout(),
				"%s resolves to: %s@%s\n",
				query,
				packageVersion.Name,
				packageVersion.Version,
			)
			return nil
		},
	}

	command.Flags().BoolVar(
		&flags.showAll, "show-all", false,
		"show all available templates",
	)

	return command
}

func printSearchResults(
	w io.Writer,
	query string,
	results *searcher.SearchResults,
	showAll bool,
) error {
	if len(results.Packages) == 0 {
		fmt.Fprintf(w, "No results found for %q\n", query)
		return nil
	}
	fmt.Fprintf(
		w,
		"Found %d+ results for %q:\n\n",
		results.NumResults,
		query,
	)

	resultsAreTrimmed := false
	pkgs := results.Packages
	if !showAll && len(pkgs) > trimmedVersionsLength {
		resultsAreTrimmed = true
		pkgs = results.Packages[:int(math.Min(10, float64(len(results.Packages))))]
	}

	for _, pkg := range pkgs {
		nonEmptyVersions := []string{}
		for i, v := range pkg.Versions {
			if !showAll && i >= trimmedVersionsLength {
				resultsAreTrimmed = true
				break
			}
			if v.Version != "" {
				nonEmptyVersions = append(nonEmptyVersions, v.Version)
			}
		}

		versionString := ""
		if len(nonEmptyVersions) > 0 {
			ellipses := lo.Ternary(resultsAreTrimmed && pkg.NumVersions > trimmedVersionsLength, " ...", "")
			versionString = fmt.Sprintf(" (%s%s)", strings.Join(nonEmptyVersions, ", "), ellipses)
		}
		fmt.Fprintf(w, "* %s %s\n", pkg.Name, versionString)
	}

	if resultsAreTrimmed {
		fmt.Println()
		ux.Fwarning(
			w,
			"Showing top 10 results and truncated versions. Use --show-all to "+
				"show all.\n\n",
		)
	}

	return nil
}
