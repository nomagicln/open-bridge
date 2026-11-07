// Package main is the entry point for the OpenBridge CLI.
// OpenBridge is a universal API runtime engine that bridges OpenAPI specifications
// to semantic CLI commands and MCP server for AI agents.
package main

import (
	"fmt"
	"os"

	"github.com/nomagicln/open-bridge/internal/app"
	"github.com/nomagicln/open-bridge/pkg/cli"
)

// Build information, set via ldflags.
var (
	version = "dev"
	commit  = "unknown"
	date    = "unknown"
)

func main() {
	app.SetBuildInfo(version, commit, date)

	deps, err := app.New()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	if err := deps.Run(os.Args); err != nil {
		// Don't print errors that have already been printed (e.g., formatted CLI errors)
		if !cli.IsPrintedError(err) && err.Error() != "" {
			fmt.Fprintln(os.Stderr, err)
		}
		os.Exit(1)
	}
}
