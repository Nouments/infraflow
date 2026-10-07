package cli

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"time"

	configadapter "infraflow/internal/adapters/config"
	planningadapter "infraflow/internal/adapters/planning"
	"infraflow/provider/internal/adapters/config"
	"infraflow/provider/internal/adapters/filesystem"
	"infraflow/provider/internal/adapters/generation"
	"infraflow/provider/internal/adapters/sqlite"
	"infraflow/provider/internal/application"
	"infraflow/provider/internal/delivery/grpcapi"
	"infraflow/provider/internal/delivery/httpapi"
	"infraflow/provider/internal/delivery/web"
	credentialstore "infraflow/provider/internal/infrastructure/credentials"

	"google.golang.org/grpc/credentials"
)

func Run(arguments []string, stdout, stderr io.Writer) int {
	if len(arguments) == 0 {
		printUsage(stderr)
		return 2
	}
	command := arguments[0]
	if command == "help" || command == "-h" || command == "--help" {
		printUsage(stdout)
		return 0
	}
	if command == "serve" {
		return runServe(arguments[1:], stdout, stderr)
	}
	if command == "template-info" {
		return runTemplateInfo(arguments[1:], stdout, stderr)
	}
	if command == "capability-summary" {
		return runCapabilitySummary(arguments[1:], stdout, stderr)
	}
	if command != "validate" && command != "plan" && command != "generate" && command != "generate-all" && command != "generate-ansible" && command != "generate-terraform" && command != "generate-bootstrap" && command != "template-info" && command != "capability-summary" {
		fmt.Fprintf(stderr, "infraflow-provider: unknown command %q\n", command)
		printUsage(stderr)
		return 2
	}

	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	flags.SetOutput(stderr)
	inputPath := flags.String("f", "", "path to the infrastructure YAML file")
	outputPath := flags.String("out", "", "artifact output directory (generate commands)")
	if err := flags.Parse(arguments[1:]); err != nil {
		return 2
	}
	if flags.NArg() != 0 || *inputPath == "" || (command == "generate" || command == "generate-all" || command == "generate-ansible" || command == "generate-terraform" || command == "generate-bootstrap") && *outputPath == "" {
		fmt.Fprintln(stderr, "infraflow-provider: command requires -f; generate commands also require -out")
		return 2
	}
	input, err := os.ReadFile(*inputPath)
	if err != nil {
		fmt.Fprintf(stderr, "infraflow-provider: open input %q: %v\n", *inputPath, err)
		return 1
	}
	dependencies := application.Dependencies{Parser: configadapter.Parser{}, PlanBuilder: planningadapter.Builder{}}
	service := application.NewService(nil, nil, generation.Generator{}, dependencies)
	switch command {
	case "validate":
		infrastructure, err := service.Validate(input)
		if err != nil {
			fmt.Fprintf(stderr, "infraflow-provider: %v\n", err)
			return 1
		}
		deviceCount := 0
		for _, site := range infrastructure.Sites {
			deviceCount += len(site.Devices)
		}
		fmt.Fprintf(stdout, "Valid: %d site(s), %d device(s); no infrastructure changes made.\n", len(infrastructure.Sites), deviceCount)
	case "plan":
		plan, err := service.Plan(input)
		if err != nil {
			fmt.Fprintf(stderr, "infraflow-provider: %v\n", err)
			return 1
		}
		encoder := json.NewEncoder(stdout)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(plan); err != nil {
			fmt.Fprintf(stderr, "infraflow-provider: encode plan: %v\n", err)
			return 1
		}
	case "generate":
		artifacts, err := service.Generate(input, *outputPath)
		if err != nil {
			fmt.Fprintf(stderr, "infraflow-provider: %v\n", err)
			return 1
		}
		for _, artifact := range artifacts {
			fmt.Fprintf(stdout, "generated %s (sha256 %s)\n", artifact.Path, artifact.OutputHash)
		}
	case "generate-all":
		artifacts, err := service.GenerateAll(input, *outputPath)
		if err != nil {
			fmt.Fprintf(stderr, "infraflow-provider: %v\n", err)
			return 1
		}
		for _, artifact := range artifacts {
			fmt.Fprintf(stdout, "generated and published %s (sha256 %s)\n", artifact.Path, artifact.OutputHash)
		}
	case "generate-ansible":
		artifacts, err := service.GenerateAnsible(input, *outputPath)
		if err != nil {
			fmt.Fprintf(stderr, "infraflow-provider: %v\n", err)
			return 1
		}
		for _, artifact := range artifacts {
			fmt.Fprintf(stdout, "generated %s (sha256 %s)\n", artifact.Path, artifact.OutputHash)
		}
	case "generate-terraform":
		artifacts, err := service.GenerateTerraform(input, *outputPath)
		if err != nil {
			fmt.Fprintf(stderr, "infraflow-provider: %v\n", err)
			return 1
		}
		for _, artifact := range artifacts {
			fmt.Fprintf(stdout, "generated %s (sha256 %s)\n", artifact.Path, artifact.OutputHash)
		}
	case "generate-bootstrap":
		artifacts, err := service.GenerateBootstrap(input, *outputPath)
		if err != nil {
			fmt.Fprintf(stderr, "infraflow-provider: %v\n", err)
			return 1
		}
		for _, artifact := range artifacts {
			fmt.Fprintf(stdout, "generated %s (sha256 %s)\n", artifact.Path, artifact.OutputHash)
		}
	}
	return 0
}

func runTemplateInfo(arguments []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("template-info", flag.ContinueOnError)
	flags.SetOutput(stderr)
	inputPath := flags.String("f", "", "path to the infrastructure YAML file")
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	if flags.NArg() != 0 || *inputPath == "" {
		fmt.Fprintln(stderr, "infraflow-provider: template-info requires -f")
		return 2
	}
	input, err := os.ReadFile(*inputPath)
	if err != nil {
		fmt.Fprintf(stderr, "infraflow-provider: open input %q: %v\n", *inputPath, err)
		return 1
	}
	service := application.NewService(nil, nil, nil, application.Dependencies{Parser: configadapter.Parser{}, PlanBuilder: planningadapter.Builder{}})
	results, err := service.TemplateInfo(input)
	if err != nil {
		fmt.Fprintf(stderr, "infraflow-provider: %v\n", err)
		return 1
	}
	for _, result := range results {
		status := "ready"
		if result.Blocked {
			status = "blocked"
		}
		if result.TemplateID != "" {
			fmt.Fprintf(stdout, "selected template %s (hash %s) for %s/%s: capability=%s status=%s\n", result.TemplateID, result.TemplateHash, result.Site, result.Device, result.Capability, status)
		} else {
			fmt.Fprintf(stdout, "no template selected for %s/%s: capability=%s status=%s\n", result.Site, result.Device, result.Capability, status)
		}
		if result.CapabilityMsg != "" {
			fmt.Fprintf(stdout, "  %s\n", result.CapabilityMsg)
		}
	}
	return 0
}

func runCapabilitySummary(arguments []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("capability-summary", flag.ContinueOnError)
	flags.SetOutput(stderr)
	inputPath := flags.String("f", "", "path to the infrastructure YAML file")
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	if flags.NArg() != 0 || *inputPath == "" {
		fmt.Fprintln(stderr, "infraflow-provider: capability-summary requires -f")
		return 2
	}
	input, err := os.ReadFile(*inputPath)
	if err != nil {
		fmt.Fprintf(stderr, "infraflow-provider: open input %q: %v\n", *inputPath, err)
		return 1
	}
	service := application.NewService(nil, nil, nil, application.Dependencies{Parser: configadapter.Parser{}, PlanBuilder: planningadapter.Builder{}})
	summaries, err := service.CapabilitySummary(input)
	if err != nil {
		fmt.Fprintf(stderr, "infraflow-provider: %v\n", err)
		return 1
	}
	for _, item := range summaries {
		status := "not-ready"
		if item.Ready {
			status = "ready"
		}
		if item.TemplateID != "" {
			fmt.Fprintf(stdout, "%s/%s template=%s hash=%s capability=%s status=%s\n", item.Site, item.Device, item.TemplateID, item.TemplateHash, item.Capability, status)
		} else {
			fmt.Fprintf(stdout, "%s/%s template=none capability=%s status=%s\n", item.Site, item.Device, item.Capability, status)
		}
		if item.CapabilityMsg != "" {
			fmt.Fprintf(stdout, "  %s\n", item.CapabilityMsg)
		}
	}
	return 0
}

func runServe(arguments []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("serve", flag.ContinueOnError)
	flags.SetOutput(stderr)
	configPath := flags.String("config", "", "provider YAML configuration file")
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	if flags.NArg() != 0 || *configPath == "" {
		fmt.Fprintln(stderr, "infraflow-provider: serve requires -config")
		return 2
	}
	settings, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintf(stderr, "infraflow-provider: %v\n", err)
		return 2
	}
	reportStore, err := filesystem.NewReportStore(settings.ArtifactDirectory)
	if err != nil {
		fmt.Fprintf(stderr, "infraflow-provider: %v\n", err)
		return 2
	}
	jobStore, err := filesystem.NewJobStore(settings.ArtifactDirectory)
	if err != nil {
		fmt.Fprintf(stderr, "infraflow-provider: %v\n", err)
		return 2
	}
	agentStore, err := filesystem.NewAgentStore(settings.ArtifactDirectory)
	if err != nil {
		fmt.Fprintf(stderr, "infraflow-provider: %v\n", err)
		return 2
	}
	eventStore, err := filesystem.NewEventStore(settings.ArtifactDirectory)
	if err != nil {
		fmt.Fprintf(stderr, "infraflow-provider: %v\n", err)
		return 2
	}
	dependencies := application.Dependencies{Parser: configadapter.Parser{}, PlanBuilder: planningadapter.Builder{}}
	service := application.NewServiceWithJobsAgentsEvents(filesystem.NewArtifactRepository(settings.ArtifactDirectory), reportStore, generation.Generator{}, jobStore, agentStore, eventStore, dependencies)
	var transportCredentials credentials.TransportCredentials
	if settings.TLS.CertificateFile != "" {
		transportCredentials, err = credentials.NewServerTLSFromFile(settings.TLS.CertificateFile, settings.TLS.KeyFile)
		if err != nil {
			fmt.Fprintf(stderr, "infraflow-provider: load TLS certificate: %v\n", err)
			return 2
		}
	}
	server, err := grpcapi.NewServer(service, os.Getenv(settings.TokenEnv), settings.ChunkSize, transportCredentials)
	if err != nil {
		fmt.Fprintf(stderr, "infraflow-provider: %v\n", err)
		return 2
	}
	var authenticator *application.Authenticator
	var userStore *sqlite.Store
	if settings.APIListenAddress != "" {
		userStore, err = sqlite.New(settings.DatabasePath)
		if err != nil {
			fmt.Fprintf(stderr, "infraflow-provider: %v\n", err)
			return 2
		}
		defer userStore.Close()
		authenticator, err = application.NewAuthenticator(userStore, time.Duration(settings.SessionTTLMinutes)*time.Minute)
		if err != nil {
			fmt.Fprintf(stderr, "infraflow-provider: %v\n", err)
			return 2
		}
		password, created, err := authenticator.BootstrapAdmin(context.Background(), settings.AdminUsername)
		if err != nil {
			fmt.Fprintf(stderr, "infraflow-provider: initialize user authentication: %v\n", err)
			return 2
		}
		if created {
			if err := credentialstore.WriteAdminPassword(password, settings.AdminCredentialFile, settings.AdminCredentialScript); err != nil {
				fmt.Fprintf(stderr, "infraflow-provider: store administrator credential: %v\n", err)
				return 2
			}
			fmt.Fprintf(stdout, "InfraFlow administrator credentials written to %s; retrieve with %s\n", settings.AdminCredentialFile, settings.AdminCredentialScript)
		}
	}
	var webApp interface {
		Listener(net.Listener) error
		ShutdownWithContext(context.Context) error
	}
	var apiListener net.Listener
	if settings.APIListenAddress != "" {
		handler, err := httpapi.NewHandler(service, os.Getenv(settings.TokenEnv), authenticator)
		if err != nil {
			fmt.Fprintf(stderr, "infraflow-provider: %v\n", err)
			return 2
		}
		apiListener, err = net.Listen("tcp", settings.APIListenAddress)
		if err != nil {
			fmt.Fprintf(stderr, "infraflow-provider: API listen: %v\n", err)
			return 1
		}
		webApp = web.NewApp(handler, settings.WebUIEnabled)
		if settings.TLS.CertificateFile != "" {
			certificate, err := tls.LoadX509KeyPair(settings.TLS.CertificateFile, settings.TLS.KeyFile)
			if err != nil {
				_ = apiListener.Close()
				fmt.Fprintf(stderr, "infraflow-provider: load API TLS certificate: %v\n", err)
				return 2
			}
			apiListener = tls.NewListener(apiListener, &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{certificate}})
		}
		go func() {
			if err := webApp.Listener(apiListener); err != nil && !errors.Is(err, net.ErrClosed) {
				fmt.Fprintf(stderr, "infraflow-provider: API serve: %v\n", err)
			}
		}()
		defer func() {
			shutdownContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = webApp.ShutdownWithContext(shutdownContext)
		}()
		apiScheme := "http"
		if settings.TLS.CertificateFile != "" {
			apiScheme = "https"
		}
		fmt.Fprintf(stdout, "InfraFlow provider %s API listening on %s\n", apiScheme, settings.APIListenAddress)
		if settings.WebUIEnabled {
			fmt.Fprintf(stdout, "InfraFlow web console available at %s://%s/\n", apiScheme, settings.APIListenAddress)
		}
	}
	listener, err := net.Listen("tcp", settings.ListenAddress)
	if err != nil {
		if apiListener != nil {
			_ = apiListener.Close()
		}
		fmt.Fprintf(stderr, "infraflow-provider: listen: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "InfraFlow provider gRPC listening on %s; agent token is read from %s\n", settings.ListenAddress, settings.TokenEnv)
	if err := server.Serve(listener); err != nil {
		fmt.Fprintf(stderr, "infraflow-provider: serve: %v\n", err)
		return 1
	}
	return 0
}

func printUsage(writer io.Writer) {
	fmt.Fprintln(writer, `InfraFlow Provider - declarative infrastructure planning

Usage:
  infraflow-provider validate -f <infra.yaml>
  infraflow-provider plan -f <infra.yaml>
	infraflow-provider generate -f <infra.yaml> -out <directory>
	infraflow-provider generate-all -f <infra.yaml> -out <directory>
	infraflow-provider generate-ansible -f <infra.yaml> -out <directory>
	infraflow-provider generate-terraform -f <infra.yaml> -out <directory>
	infraflow-provider generate-bootstrap -f <infra.yaml> -out <directory>
	infraflow-provider template-info -f <infra.yaml>
	infraflow-provider capability-summary -f <infra.yaml>
	infraflow-provider serve -config <provider.yaml>

Validation is side-effect free. Planning does not execute tasks. Template inspection reports selected metadata and capability state without executing anything. Capability summaries provide an evidence-only readiness snapshot and do not trigger jobs or provisioning. The gRPC service streams verified artifacts to authenticated agents and accepts execution reports. Configure api_listen_address to enable Fiber REST/web hosting and web_ui_enabled: true to serve the console at /. Remote API addresses require TLS certificate/key files. The browser console uses the existing user login and RBAC.`)
}
