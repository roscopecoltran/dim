// Copyright 2016
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//
// See the License for the specific language governing permissions and
// limitations under the License.

package cmd

import (
	"errors"
	"fmt"
	"strings"

	"strconv"

	"time"

	"context"

	"github.com/Sirupsen/logrus"
	"github.com/docker/engine-api/types"
	"github.com/nhurel/dim/cli"
	"github.com/nhurel/dim/lib/registry"
	"github.com/nhurel/dim/lib/utils"
	t "github.com/nhurel/dim/types"
	"github.com/spf13/cobra"
)

func newSearchCommand(c *cli.Cli, rootCommand *cobra.Command, ctx context.Context) {
	searchCommand := &cobra.Command{
		Use:   "search QUERY",
		Short: "Run a search against a private registry",
		Long: `Search an image on the private registry.
By default the provided query is searched in the names and tags of the images on the registry.
Using -a flag, you can run advanced queries and search in the labels and volumes too.`,
		Example: `# Find the images with label os=ubuntu
dim search -a Label.os:ubuntu
# Find the images having a label 'os'
dim search -a Labels:os

With the -a flag, you can also use the +/- operator to combine your clauses :
dim search -a +Label.os:ubuntu -Label.version=xenial`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSearch(c, args)
		},
	}

	searchCommand.Flags().BoolVarP(&advancedFlag, "advanced", "a", false, "Runs complex query")
	searchCommand.Flags().IntVar(&paginationFlag, "bulk-size", 15, "Number of restuls to fetch at a time")
	searchCommand.Flags().IntVarP(&widthFlag, "width", "W", 150, "Column width")
	rootCommand.AddCommand(searchCommand)
}

func runSearch(c *cli.Cli, args []string) error {
	if len(args) == 0 {
		return errors.New("query is missing")
	}
	query := args[0]

	var authConfig *types.AuthConfig
	if username != "" || password != "" {
		authConfig = &types.AuthConfig{Username: username, Password: password}
	}

	var client registry.Client
	var err error

	logrus.WithField("url", registryURL).Debugln("Connecting to registry")

	if client, err = registry.New(c, authConfig, registryURL); err != nil {
		return fmt.Errorf("Failed to connect to registry : %v", err)
	}

	var q, a string
	if advancedFlag {
		a = query
	} else {
		q = query
	}

	var results *t.SearchResults
	if results, err = client.Search(q, a, 0, paginationFlag); err != nil {
		return fmt.Errorf("Failed to search images : %v", err)
	}

	if results.NumResults > 0 {
		fmt.Fprintf(c.Err, "%d results found :\n", results.NumResults)
		printer := cli.NewTabPrinter(c.Out, c.In)
		printer.Width = widthFlag
		printer.Append([]string{"Name", "Tag", "Created", "Labels", "Volumes", "Ports"})
		for _, r := range results.Results {
			printer.Append([]string{r.Name, r.Tag, utils.ParseDuration(time.Since(r.Created)), utils.FlatMap(r.Label), strings.Join(r.Volumes, ","), strings.Join(intToStringSlice(r.ExposedPorts), ",")})
		}
		printer.PrintAll(false)
		for fetched := len(results.Results); results.NumResults > fetched; {
			if results, err = client.Search(q, a, fetched, paginationFlag); err != nil {
				return fmt.Errorf("Failed to search images : %v", err)
			}
			for _, r := range results.Results {
				printer.Append([]string{r.Name, r.Tag, utils.ParseDuration(time.Since(r.Created)), utils.FlatMap(r.Label), strings.Join(r.Volumes, ","), strings.Join(intToStringSlice(r.ExposedPorts), ",")})
			}
			printer.PrintAll(true)
			fetched += len(results.Results)
		}
		fmt.Println()
	} else {
		fmt.Fprintln(c.Err, "No result found")
	}

	return nil
}

func intToStringSlice(iSlice []int) []string {
	result := make([]string, len(iSlice))
	for ind, i := range iSlice {
		result[ind] = strconv.Itoa(i)
	}
	return result
}

var (
	advancedFlag   bool
	paginationFlag int
	widthFlag      int
)
