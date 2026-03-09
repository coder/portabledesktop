package main

import (
	"fmt"
	"os"

	"github.com/coder/portabledesktop/pd/internal/cli"
)

func main() {
	// Set the embedded runtime blob for the CLI to use.
	cli.EmbeddedRuntime = embeddedRuntime

	if err := cli.Run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
