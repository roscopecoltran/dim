package cmd

import (
	"fmt"
	"github.com/Sirupsen/logrus"
	"github.com/docker/engine-api/types"
	"github.com/nhurel/dim/lib/index"
	"github.com/nhurel/dim/server"
	"github.com/spf13/cobra"
	"os"
	"os/signal"
	"path"
	"time"
)

var serverCommand = &cobra.Command{
	Use: "server",
	RunE: func(cmd *cobra.Command, args []string) error {
		handleSignal()

		if len(args) == 0 {
			return fmt.Errorf("No registry URL given")
		}

		realDir := path.Join(IndexDir, time.Now().Format("20060102150405.000"))
		logrus.Warnf("Creating index dir at %s\n", realDir)

		var authConfig *types.AuthConfig
		if username != "" || password != "" {
			authConfig = &types.AuthConfig{Username: username, Password: password}
		}

		idx, err := index.New(realDir, args[0], authConfig)
		if err != nil {
			return err
		}
		if err = idx.Build(); err != nil {
			return err
		}
		s = server.NewServer(Port, idx)
		logrus.Infoln("Server listening...")
		return s.Run()
	},
}

var (
	Port     string
	IndexDir string
	username string
	password string
	url      string
	//secure bool
)

var s *server.Server

func init() {
	serverCommand.Flags().StringVarP(&Port, "port", "p", "0.0.0.0:6000", "Dim listening port")
	serverCommand.Flags().StringVar(&IndexDir, "index-path", "dim.index", "Dim listening port")
	serverCommand.Flags().StringVar(&username, "registry-user", "", "Registry username")
	serverCommand.Flags().StringVar(&password, "registry-password", "", "Registry password")
	RootCommand.AddCommand(serverCommand)
}

func handleSignal() {
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)
	go func() {
		for _ = range c {
			if s != nil {
				logrus.Infoln("ShuttingDown server")
				s.BlockingClose()
			}
			os.Exit(0)
		}
	}()
}
