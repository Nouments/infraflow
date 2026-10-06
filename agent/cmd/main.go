package main

import (
	"os"

	"infraflow/agent/internal/delivery/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:], os.Stdout, os.Stderr))
}
