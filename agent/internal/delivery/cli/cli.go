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
	"infraflow/agent/internal/adapters/providerhttp"
	"infraflow/agent/internal/application"
	"infraflow/agent/internal/config"
	"infraflow/agent/internal/delivery/tui"
)

func Run(arguments []string, stdout, stderr io.Writer) int {
	if len(arguments) == 0 || arguments[0] == "help" || arguments[0] == "-h" || arguments[0] == "--help" {
		printUsage(stdout)
		return 0
	}
	if arguments[0] == "tui" {
		return runTUI(arguments[1:], stdout, stderr)
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
	siteID := settings.Agent.SiteID
	if siteID == "" {
		siteID = settings.Agent.ID
	}
	var registry *providerhttp.Client
	if settings.Provider.APIAddress != "" {
		registry, err = providerhttp.New(settings.Provider.APIAddress, os.Getenv(settings.Provider.TokenEnv))
		if err != nil {
			fmt.Fprintf(stderr, "infraflow-agent: %v\n", err)
			return 2
		}
		if err := registry.Register(context.Background(), providerhttp.Registration{
			AgentID: settings.Agent.ID, SiteID: siteID, Version: agentVersion,
			Capabilities: settings.Agent.Capabilities,
		}); err != nil {
			fmt.Fprintf(stderr, "infraflow-agent: register agent: %v\n", err)
			return 1
		}
	}
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
		if registry != nil {
			_ = registry.Heartbeat(context.Background(), providerhttp.Heartbeat{AgentID: settings.Agent.ID, SiteID: siteID, Version: agentVersion, Capabilities: settings.Agent.Capabilities})
		}
		return 1
	}
	if registry != nil {
		if heartbeatErr := registry.Heartbeat(context.Background(), providerhttp.Heartbeat{AgentID: settings.Agent.ID, SiteID: siteID, Version: agentVersion, Capabilities: settings.Agent.Capabilities}); heartbeatErr != nil {
			fmt.Fprintf(stderr, "infraflow-agent: heartbeat: %v\n", heartbeatErr)
			return 1
		}
	}
	fmt.Fprintf(stdout, "reported execution %s\n", report.ReportID)
	return 0
}

func runTUI(arguments []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("tui", flag.ContinueOnError)
	flags.SetOutput(stderr)
	address := flags.String("address", "", "provider HTTP(S) API address, for example https://10.0.0.5:8080")
	caFile := flags.String("ca-file", "", "CA certificate file for the provider HTTPS API")
	username := flags.String("username", "", "provider platform username")
	passwordEnv := flags.String("password-env", "INFRAFLOW_TUI_PASSWORD", "environment variable containing the platform password")
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	if flags.NArg() != 0 || *address == "" || *username == "" || *passwordEnv == "" {
		fmt.Fprintln(stderr, "infraflow-agent: tui requires -address and -username; password is read from -password-env")
		return 2
	}
	password := os.Getenv(*passwordEnv)
	if password == "" {
		fmt.Fprintf(stderr, "infraflow-agent: password environment variable %s is empty\n", *passwordEnv)
		return 2
	}
	client, err := providerhttp.NewUserClient(*address, *caFile)
	if err != nil {
		fmt.Fprintf(stderr, "infraflow-agent: %v\n", err)
		return 2
	}
	if err := tui.Run(context.Background(), os.Stdin, stdout, client, *username, password); err != nil {
		fmt.Fprintf(stderr, "infraflow-agent: %v\n", err)
		return 1
	}
	return 0
}

const agentVersion = "0.1.0"

func printUsage(writer io.Writer) {
	fmt.Fprintln(writer, `InfraFlow Agent - provider artifact executor and state reporter

Usage:
	infraflow-agent run -config <agent.yaml>
	infraflow-agent tui -address <http(s)://server:port> -username <name> [-ca-file <path>] [-password-env <env>]

The agent reads its provider address and TLS settings from YAML. Its token value is read from the configured environment variable. Artifact files are downloaded with gRPC streaming.`)
}
