package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
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
	if command != "validate" && command != "plan" && command != "generate" && command != "generate-ansible" && command != "generate-terraform" && command != "generate-bootstrap" {
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
	if flags.NArg() != 0 || *inputPath == "" || (command == "generate" || command == "generate-ansible" || command == "generate-terraform" || command == "generate-bootstrap") && *outputPath == "" {
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
	var apiServer *http.Server
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
		apiServer = &http.Server{
			Handler:           handler,
			ReadHeaderTimeout: 5 * time.Second,
			ReadTimeout:       15 * time.Second,
			WriteTimeout:      15 * time.Second,
			IdleTimeout:       60 * time.Second,
		}
		go func() {
			var serveErr error
			if settings.TLS.CertificateFile != "" {
				serveErr = apiServer.ServeTLS(apiListener, settings.TLS.CertificateFile, settings.TLS.KeyFile)
			} else {
				serveErr = apiServer.Serve(apiListener)
			}
			if err := serveErr; err != nil && err != http.ErrServerClosed {
				fmt.Fprintf(stderr, "infraflow-provider: API serve: %v\n", err)
			}
		}()
		defer func() {
			shutdownContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = apiServer.Shutdown(shutdownContext)
		}()
		apiScheme := "http"
		if settings.TLS.CertificateFile != "" {
			apiScheme = "https"
		}
		fmt.Fprintf(stdout, "InfraFlow provider %s API listening on %s\n", apiScheme, settings.APIListenAddress)
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
	infraflow-provider generate-ansible -f <infra.yaml> -out <directory>
	infraflow-provider generate-terraform -f <infra.yaml> -out <directory>
	infraflow-provider generate-bootstrap -f <infra.yaml> -out <directory>
	infraflow-provider serve -config <provider.yaml>

Validation is side-effect free. Planning does not execute tasks. The gRPC service streams verified artifacts to authenticated agents and accepts execution reports. When api_listen_address is configured, the HTTP API manages persisted planning jobs; remote API addresses require configured TLS certificate and key files. The Linux TUI connects directly to this API.`)
}
