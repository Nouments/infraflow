package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"infraflow/agent/internal/adapters/filesystem"
	"infraflow/agent/internal/adapters/processor"
	"infraflow/agent/internal/adapters/providergrpc"
	"infraflow/agent/internal/adapters/providerhttp"
	"infraflow/agent/internal/application"
	"infraflow/agent/internal/config"
	artifacthttp "infraflow/agent/internal/delivery/artifacts"
	"infraflow/agent/internal/delivery/tui"
	"infraflow/internal/infrastructure/security"
)

func Run(arguments []string, stdout, stderr io.Writer) int {
	if len(arguments) == 0 || arguments[0] == "help" || arguments[0] == "-h" || arguments[0] == "--help" {
		printUsage(stdout)
		return 0
	}
	if arguments[0] == "tui" {
		return runTUI(arguments[1:], stdout, stderr)
	}
	if arguments[0] == "serve-artifacts" {
		return runArtifactServer(arguments[1:], stdout, stderr)
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

func runArtifactServer(arguments []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("serve-artifacts", flag.ContinueOnError)
	flags.SetOutput(stderr)
	directory := flags.String("directory", "", "agent state directory containing verified artifacts")
	listenAddress := flags.String("listen", "", "HTTP listen address, for example 127.0.0.1:8081")
	tokenEnv := flags.String("token-env", "", "environment variable containing the bearer token for non-loopback access")
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	if flags.NArg() != 0 || *directory == "" || *listenAddress == "" {
		fmt.Fprintln(stderr, "infraflow-agent: serve-artifacts requires -directory and -listen")
		return 2
	}
	token := ""
	if *tokenEnv != "" {
		token = os.Getenv(*tokenEnv)
	}
	if !artifacthttp.IsLoopbackListenAddress(*listenAddress) && len([]byte(token)) < security.MinAgentTokenBytes {
		fmt.Fprintf(stderr, "infraflow-agent: non-loopback artifact serving requires -token-env with a token of at least %d bytes\n", security.MinAgentTokenBytes)
		return 2
	}
	store := filesystem.NewStateStore(*directory)
	handler, err := artifacthttp.NewHandler(store, token)
	if err != nil {
		fmt.Fprintf(stderr, "infraflow-agent: configure artifact server: %v\n", err)
		return 2
	}
	listener, err := net.Listen("tcp", *listenAddress)
	if err != nil {
		fmt.Fprintf(stderr, "infraflow-agent: artifact listen: %v\n", err)
		return 1
	}
	server := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	signalContext, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	serverErr := make(chan error, 1)
	go func() { serverErr <- server.Serve(listener) }()
	fmt.Fprintf(stdout, "InfraFlow agent artifact server listening on http://%s\n", *listenAddress)
	select {
	case <-signalContext.Done():
		shutdownContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownContext)
		return 0
	case err := <-serverErr:
		if err == http.ErrServerClosed {
			return 0
		}
		fmt.Fprintf(stderr, "infraflow-agent: artifact server: %v\n", err)
		return 1
	}
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
	infraflow-agent serve-artifacts -directory <state-dir> -listen <host:port> [-token-env <env>]
	infraflow-agent tui -address <http(s)://server:port> -username <name> [-ca-file <path>] [-password-env <env>]

The agent reads its provider address and TLS settings from YAML. Its token value is read from the configured environment variable. Artifact files are downloaded with gRPC streaming.`)
}
