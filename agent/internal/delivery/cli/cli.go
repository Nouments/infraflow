package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"

	"infraflow/agent/internal/adapters/filesystem"
	"infraflow/agent/internal/adapters/processor"
	"infraflow/agent/internal/adapters/providergrpc"
	"infraflow/agent/internal/application"
	"infraflow/agent/internal/config"
)

func Run(arguments []string, stdout, stderr io.Writer) int {
	if len(arguments) == 0 || arguments[0] == "help" || arguments[0] == "-h" || arguments[0] == "--help" {
		printUsage(stdout)
		return 0
	}
	if arguments[0] != "run" {
		fmt.Fprintf(stderr, "infraflow-agent: unknown command %q\n", arguments[0])
		printUsage(stderr)
		return 2
	}
	flags := flag.NewFlagSet("run", flag.ContinueOnError)
	flags.SetOutput(stderr)
	configPath := flags.String("config", "", "path to the agent YAML configuration")
	if err := flags.Parse(arguments[1:]); err != nil {
		return 2
	}
	if flags.NArg() != 0 || *configPath == "" {
		fmt.Fprintln(stderr, "infraflow-agent: run requires -config")
		return 2
	}
	settings, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintf(stderr, "infraflow-agent: %v\n", err)
		return 2
	}
	providerClient, err := providergrpc.New(
		settings.Provider.Address,
		os.Getenv(settings.Provider.TokenEnv),
		settings.Provider.TLS.Enabled,
		settings.Provider.TLS.CAFile,
	)
	if err != nil {
		fmt.Fprintf(stderr, "infraflow-agent: %v\n", err)
		return 2
	}
	defer providerClient.Close()
	runner, err := application.NewRunner(
		settings.Agent.ID,
		providerClient,
		filesystem.NewStateStore(settings.Agent.StateDirectory),
		processor.Generic{},
	)
	if err != nil {
		fmt.Fprintf(stderr, "infraflow-agent: %v\n", err)
		return 2
	}
	report, err := runner.Run(context.Background())
	for _, artifact := range report.Artifacts {
		fmt.Fprintf(stdout, "%s %s\n", artifact.Status, artifact.Path)
	}
	if err != nil {
		fmt.Fprintf(stderr, "infraflow-agent: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "reported execution %s\n", report.ReportID)
	return 0
}

func printUsage(writer io.Writer) {
	fmt.Fprintln(writer, `InfraFlow Agent - provider artifact executor and state reporter

Usage:
	infraflow-agent run -config <agent.yaml>

The agent reads its provider address and TLS settings from YAML. Its token value is read from the configured environment variable. Artifact files are downloaded with gRPC streaming.`)
}
