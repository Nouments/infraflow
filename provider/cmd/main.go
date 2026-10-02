package main

import (
	"os"

	"infraflow/provider/internal/delivery/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:], os.Stdout, os.Stderr))
}
