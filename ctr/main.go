package main

import (
	"fmt"
	"os"
	"time"

	"github.com/Sirupsen/logrus"
	"github.com/codegangsta/cli"
	"github.com/docker/containerd"
)

const usage = `High performance container daemon cli`

type Exit struct {
	Code int
}

func main() {
	// We want our defer functions to be run when calling fatal()
	defer func() {
		if e := recover(); e != nil {
			if ex, ok := e.(Exit); ok == true {
				os.Exit(ex.Code)
			}
			panic(e)
		}
	}()
	app := cli.NewApp()
	app.Name = "ctr"
	if containerd.GitCommit != "" {
		app.Version = fmt.Sprintf("%s commit: %s", containerd.Version, containerd.GitCommit)
	} else {
		app.Version = containerd.Version
	}
	app.Usage = usage
	app.Flags = []cli.Flag{
		cli.BoolFlag{
			Name:  "debug",
			Usage: "enable debug output in the logs",
		},
		cli.StringFlag{
			Name:  "address",
			Value: "/run/containerd/containerd.sock",
			Usage: "address of GRPC API",
		},
		cli.DurationFlag{
			Name:  "conn-timeout",
			Value: 1 * time.Second,
			Usage: "GRPC connection timeout",
		},
		cli.StringFlag{
			Name:  "runtime",
			Usage: "rechoose the runtime for client, use the default one which the daemon choose if not specified",
		},
	}
	app.Commands = []cli.Command{
		checkpointCommand,
		containersCommand,
		eventsCommand,
		stateCommand,
	}
	app.Before = func(context *cli.Context) error {
		if context.GlobalBool("debug") {
			logrus.SetLevel(logrus.DebugLevel)
		}

		// Check the runtime for client.
		//
		// TODO(hukeping): if we should create a default constant to indicate which runtimes are supported by default,
		// and return an error if an unsupported runtime was provided.
		if context.GlobalString("runtime") == "" {
			logrus.Infof("No runtime for client specified, use the default one from daemon.")
		}

		return nil
	}

	if err := app.Run(os.Args); err != nil {
		logrus.Fatal(err)
	}
}

func fatal(err string, code int) {
	fmt.Fprintf(os.Stderr, "[ctr] %s\n", err)
	panic(Exit{code})
}
