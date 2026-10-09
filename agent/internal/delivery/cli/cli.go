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
	"path/filepath"
	"syscall"
	"time"

	"infraflow/agent/internal/adapters/filesystem"
	"infraflow/agent/internal/adapters/processor"
	"infraflow/agent/internal/adapters/providergrpc"
	"infraflow/agent/internal/adapters/providerhttp"
	"infraflow/agent/internal/application"
	"infraflow/agent/internal/application/toolrunner"
	"infraflow/agent/internal/config"
	artifacthttp "infraflow/agent/internal/delivery/artifacts"
	dhcpserver "infraflow/agent/internal/delivery/dhcp"
	tftpserver "infraflow/agent/internal/delivery/tftpserver"
	"infraflow/agent/internal/delivery/tui"
	"infraflow/internal/infrastructure/security"
	"infraflow/pkg/observability"
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
	if arguments[0] == "serve-bootstrap" {
		return runBootstrapHTTPServer(arguments[1:], stdout, stderr)
	}
	if arguments[0] == "serve-dhcp" {
		return runDHCPServer(arguments[1:], stdout, stderr)
	}
	if arguments[0] == "serve-tftp" {
		return runTFTPServer(arguments[1:], stdout, stderr)
	}
	if arguments[0] == "execute-ansible" {
		return runAnsibleCheck(arguments[1:], stdout, stderr)
	}
	if arguments[0] == "validate-terraform" {
		return runTerraformValidation(arguments[1:], stdout, stderr)
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
	defaultLogConfig, err := observability.DefaultConfig("agent")
	if err != nil {
		fmt.Fprintf(stderr, "infraflow-agent: configure local logs: %v\n", err)
		return 2
	}
	bootstrapLogger, err := observability.New(defaultLogConfig)
	if err != nil {
		fmt.Fprintf(stderr, "infraflow-agent: initialize bootstrap logger: %v\n", err)
		return 2
	}
	_ = bootstrapLogger.Emit(context.Background(), observability.Event{Level: "INFO", Event: "agent.config.loading", Message: "loading agent configuration", Operation: "configuration", Status: "RUNNING"})
	settings, err := config.Load(*configPath)
	if err != nil {
		_ = bootstrapLogger.Emit(context.Background(), observability.Event{Level: "ERROR", Event: "agent.config.invalid", Message: "agent configuration validation failed", Operation: "configuration", Status: "FAILED", Error: err.Error()})
		_ = bootstrapLogger.Close()
		fmt.Fprintf(stderr, "infraflow-agent: %v\n", err)
		return 2
	}
	_ = bootstrapLogger.Close()
	outbox, err := observability.NewOutbox(filepath.Join(settings.Logging.Directory, "pending-events.json"), observability.DefaultOutboxLimit)
	if err != nil {
		fmt.Fprintf(stderr, "infraflow-agent: initialize local log outbox: %v\n", err)
		return 2
	}
	logConfig := observability.Config{
		Service: "agent", Directory: settings.Logging.Directory,
		Level: settings.Logging.Level, Format: settings.Logging.Format,
		MaxBytes: settings.Logging.MaxBytes, MaxFiles: settings.Logging.MaxFiles,
		Console: true, Sink: outbox,
	}
	logger, err := observability.New(logConfig)
	if err != nil {
		fmt.Fprintf(stderr, "infraflow-agent: initialize configured logger: %v\n", err)
		return 2
	}
	defer func() {
		if err := logger.Close(); err != nil {
			fmt.Fprintf(stderr, "infraflow-agent: close logger: %v\n", err)
		}
	}()
	siteID := settings.Agent.SiteID
	if siteID == "" {
		siteID = settings.Agent.ID
	}
	if err := logger.Emit(context.Background(), observability.Event{
		Level: "INFO", Event: "agent.started", Message: "agent starting configured run",
		AgentID: settings.Agent.ID, SiteID: siteID, Operation: "startup", Status: "RUNNING",
	}); err != nil {
		fmt.Fprintf(stderr, "infraflow-agent: persist startup event: %v\n", err)
		return 2
	}
	providerClient, err := providergrpc.New(
		settings.Provider.Address,
		os.Getenv(settings.Provider.TokenEnv),
		settings.Provider.TLS.Enabled,
		settings.Provider.TLS.CAFile,
	)
	if err != nil {
		_ = logger.Emit(context.Background(), observability.Event{
			Level: "ERROR", Event: "agent.provider.connect_failed", Message: "provider connection setup failed",
			AgentID: settings.Agent.ID, SiteID: siteID, Operation: "connect", Status: "FAILED", Error: err.Error(),
		})
		fmt.Fprintf(stderr, "infraflow-agent: %v\n", err)
		return 2
	}
	defer providerClient.Close()
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
			_ = logger.Emit(context.Background(), observability.Event{
				Level: "ERROR", Event: "agent.registration.failed", Message: "agent registration failed",
				AgentID: settings.Agent.ID, SiteID: siteID, Operation: "registration", Status: "FAILED", Error: err.Error(),
			})
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
	runner.SetObservability(logger, outbox, siteID)
	runContext, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	report, err := runner.Run(runContext)
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

func runBootstrapHTTPServer(arguments []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("serve-bootstrap", flag.ContinueOnError)
	flags.SetOutput(stderr)
	directory := flags.String("directory", "", "agent state directory containing verified bootstrap artifacts")
	interfaceName := flags.String("interface", "", "isolated bootstrap network interface")
	listenAddress := flags.String("listen", "", "IPv4 address assigned to the selected interface")
	port := flags.Int("port", 80, "HTTP bootstrap server TCP port")
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	if flags.NArg() != 0 || *directory == "" || *interfaceName == "" || *listenAddress == "" {
		fmt.Fprintln(stderr, "infraflow-agent: serve-bootstrap requires -directory, -interface, and -listen")
		return 2
	}
	listenIP := net.ParseIP(*listenAddress).To4()
	if listenIP == nil || *port < 1 || *port > 65535 {
		fmt.Fprintln(stderr, "infraflow-agent: bootstrap listen address must be IPv4 and port must be between 1 and 65535")
		return 2
	}
	if !interfaceHasIPv4Address(*interfaceName, listenIP) {
		fmt.Fprintf(stderr, "infraflow-agent: bootstrap listen address %s is not assigned to interface %s\n", listenIP, *interfaceName)
		return 2
	}
	handler, err := artifacthttp.NewBootstrapHandler(filesystem.NewStateStore(*directory))
	if err != nil {
		fmt.Fprintf(stderr, "infraflow-agent: configure bootstrap HTTP server: %v\n", err)
		return 2
	}
	listener, err := net.Listen("tcp", net.JoinHostPort(listenIP.String(), fmt.Sprint(*port)))
	if err != nil {
		fmt.Fprintf(stderr, "infraflow-agent: bootstrap HTTP listen: %v\n", err)
		return 1
	}
	server := &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second}
	signalContext, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	serverErr := make(chan error, 1)
	go func() { serverErr <- server.Serve(listener) }()
	fmt.Fprintf(stdout, "InfraFlow read-only bootstrap HTTP server listening on http://%s:%d for interface %s\n", listenIP, *port, *interfaceName)
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
		fmt.Fprintf(stderr, "infraflow-agent: bootstrap HTTP server: %v\n", err)
		return 1
	}
}

func interfaceHasIPv4Address(interfaceName string, expected net.IP) bool {
	iface, err := net.InterfaceByName(interfaceName)
	if err != nil {
		return false
	}
	addresses, err := iface.Addrs()
	if err != nil {
		return false
	}
	for _, address := range addresses {
		localIP, _, err := net.ParseCIDR(address.String())
		if err == nil && localIP.To4() != nil && localIP.Equal(expected) {
			return true
		}
	}
	return false
}

func runDHCPServer(arguments []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("serve-dhcp", flag.ContinueOnError)
	flags.SetOutput(stderr)
	configPath := flags.String("config", "", "provider-generated bootstrap DHCP JSON config")
	interfaceName := flags.String("interface", "", "network interface to bind DHCP traffic to")
	listenAddress := flags.String("listen", "", "IPv4 address assigned to the selected interface")
	port := flags.Int("port", 67, "DHCPv4 UDP server port")
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	if flags.NArg() != 0 || *configPath == "" || *interfaceName == "" || *listenAddress == "" {
		fmt.Fprintln(stderr, "infraflow-agent: serve-dhcp requires -config, -interface, and -listen")
		return 2
	}
	listenIP := net.ParseIP(*listenAddress).To4()
	if listenIP == nil || *port < 1 || *port > 65535 {
		fmt.Fprintln(stderr, "infraflow-agent: DHCP listen address must be IPv4 and port must be between 1 and 65535")
		return 2
	}
	config, err := dhcpserver.LoadConfig(*configPath)
	if err != nil {
		fmt.Fprintf(stderr, "infraflow-agent: %v\n", err)
		return 2
	}
	server, err := dhcpserver.NewServer(config, listenIP)
	if err != nil {
		fmt.Fprintf(stderr, "infraflow-agent: configure DHCP server: %v\n", err)
		return 2
	}
	signalContext, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	fmt.Fprintf(stdout, "InfraFlow DHCP server listening on %s/%s:%d for %s\n", *interfaceName, listenIP, *port, config.Network)
	if err := server.Serve(signalContext, *interfaceName, *port); err != nil {
		fmt.Fprintf(stderr, "infraflow-agent: DHCP server: %v\n", err)
		return 1
	}
	return 0
}

func runTFTPServer(arguments []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("serve-tftp", flag.ContinueOnError)
	flags.SetOutput(stderr)
	configPath := flags.String("config", "", "provider-generated bootstrap TFTP JSON config")
	bootstrapDirectory := flags.String("root", "", "bootstrap directory containing the configured TFTP root")
	interfaceName := flags.String("interface", "", "network interface to bind TFTP traffic to")
	listenAddress := flags.String("listen", "", "IPv4 address assigned to the selected interface")
	port := flags.Int("port", 69, "TFTP UDP server port")
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	if flags.NArg() != 0 || *configPath == "" || *bootstrapDirectory == "" || *interfaceName == "" || *listenAddress == "" {
		fmt.Fprintln(stderr, "infraflow-agent: serve-tftp requires -config, -root, -interface, and -listen")
		return 2
	}
	config, err := tftpserver.LoadConfig(*configPath)
	if err != nil {
		fmt.Fprintf(stderr, "infraflow-agent: %v\n", err)
		return 2
	}
	server, err := tftpserver.NewServer(config, *bootstrapDirectory)
	if err != nil {
		fmt.Fprintf(stderr, "infraflow-agent: configure TFTP server: %v\n", err)
		return 2
	}
	signalContext, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	fmt.Fprintf(stdout, "InfraFlow TFTP server listening on %s/%s:%d for site %s (read-only)\n", *interfaceName, *listenAddress, *port, config.Site)
	if err := server.Serve(signalContext, *interfaceName, *listenAddress, *port); err != nil {
		fmt.Fprintf(stderr, "infraflow-agent: TFTP server: %v\n", err)
		return 1
	}
	return 0
}

func runAnsibleCheck(arguments []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("execute-ansible", flag.ContinueOnError)
	flags.SetOutput(stderr)
	directory := flags.String("directory", "", "agent state directory containing verified Ansible artifacts")
	site := flags.String("site", "", "site identifier whose playbook should run")
	timeout := flags.Duration("timeout", 5*time.Minute, "maximum Ansible execution time")
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	if flags.NArg() != 0 || *directory == "" || *site == "" {
		fmt.Fprintln(stderr, "infraflow-agent: execute-ansible requires -directory and -site")
		return 2
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	result, err := toolrunner.RunAnsible(ctx, *directory, *site, *timeout)
	if err != nil {
		fmt.Fprintf(stderr, "infraflow-agent: %v\n", err)
		return 1
	}
	for _, step := range result.Steps {
		fmt.Fprintln(stdout, step)
	}
	if result.Output != "" {
		fmt.Fprintln(stdout, result.Output)
	}
	return 0
}

func runTerraformValidation(arguments []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("validate-terraform", flag.ContinueOnError)
	flags.SetOutput(stderr)
	directory := flags.String("directory", "", "agent state directory containing verified Terraform artifacts")
	site := flags.String("site", "", "site identifier whose Terraform files should be validated")
	timeout := flags.Duration("timeout", 5*time.Minute, "maximum Terraform validation time")
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	if flags.NArg() != 0 || *directory == "" || *site == "" {
		fmt.Fprintln(stderr, "infraflow-agent: validate-terraform requires -directory and -site")
		return 2
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	result, err := toolrunner.RunTerraform(ctx, *directory, *site, *timeout)
	if err != nil {
		fmt.Fprintf(stderr, "infraflow-agent: %v\n", err)
		return 1
	}
	for _, step := range result.Steps {
		fmt.Fprintln(stdout, step)
	}
	if result.Output != "" {
		fmt.Fprintln(stdout, result.Output)
	}
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
	infraflow-agent serve-artifacts -directory <state-dir> -listen <host:port> [-token-env <env>]
	infraflow-agent serve-bootstrap -directory <state-dir> -interface <name> -listen <ipv4> [-port 80]
	infraflow-agent serve-dhcp -config <dhcp-config.json> -interface <name> -listen <ipv4> [-port 67]
	infraflow-agent serve-tftp -config <tftp-config.json> -root <bootstrap-dir> -interface <name> -listen <ipv4> [-port 69]
	infraflow-agent execute-ansible -directory <state-dir> -site <site> [-timeout 5m]
	infraflow-agent validate-terraform -directory <state-dir> -site <site> [-timeout 5m]
	infraflow-agent tui -address <http(s)://server:port> -username <name> [-ca-file <path>] [-password-env <env>]

The agent reads its provider address and TLS settings from YAML. Its token value is read from the configured environment variable. Artifact files are downloaded with gRPC streaming.`)
}
