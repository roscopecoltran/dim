package cmd

import (
	"fmt"
	"github.com/Sirupsen/logrus"
	"github.com/docker/docker/reference"
	"github.com/docker/engine-api/types"
	imageParser "github.com/docker/engine-api/types/reference"
	"github.com/nhurel/dim/lib/registry"
	"github.com/spf13/cobra"
	"strings"
)

var deleteCommand = &cobra.Command{
	Use:   "delete",
	Short: "Deletes an image",
	RunE: func(cmd *cobra.Command, args []string) error {
		image := args[0]

		Dim.Remove(image)

		if RemoteFlag {
			var parsedName reference.Named
			var err error
			if parsedName, err = reference.ParseNamed(image); err != nil || parsedName.Hostname() == "" {
				return fmt.Errorf("Fail to parse the name to delete the image on a remote repository %v", err)
			}

			var authConfig *types.AuthConfig
			if username != "" || password != "" {
				authConfig = &types.AuthConfig{Username: username, Password: password}
			}

			var client *registry.Client

			logrus.WithField("hostname", parsedName.Hostname()).Debugln("Connecting to registry")

			if client, err = registry.New(authConfig, buildURL(parsedName.Hostname())); err != nil {
				return fmt.Errorf("Failed to connect to registry : %v", err)
			}

			var repo registry.Repository
			parsedName, _ = reference.ParseNamed(parsedName.Name()[strings.Index(parsedName.Name(), "/")+1:])
			if repo, err = client.NewRepository(parsedName); err != nil {
				return err
			}

			var tag string
			if _, tag, err = imageParser.Parse(image); err != nil {
				return err
			}

			if tag == "" {
				tag = "latest"
			}

			logrus.Debugln("Deleting image")
			if err = repo.DeleteImage(tag); err != nil {
				logrus.WithError(err).Errorln("Failed to delete image on the remote registry")
				return err
			}

		}

		return nil
	},
}

func init() {
	deleteCommand.Flags().BoolVarP(&RemoteFlag, "remote", "r", false, "Delete the image both locally and on the remote registry")
	RootCommand.AddCommand(deleteCommand)
}
