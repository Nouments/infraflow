package application

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"infraflow/pkg/protocol"
)

type siteCredential struct {
	siteID  string
	agentID string
	token   string
}

func TestMultiSiteAgentCLITransfersOnlyAuthorizedArtifacts(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	repoRoot, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	tempRoot := t.TempDir()
	providerBinary := filepath.Join(tempRoot, "infraflow-provider")
	agentBinary := filepath.Join(tempRoot, "infraflow-agent")
	buildBinary(t, ctx, repoRoot, providerBinary, "./provider/cmd")
	buildBinary(t, ctx, repoRoot, agentBinary, "./agent/cmd")

	example := filepath.Join(repoRoot, "examples", "multisite-preparation.yaml")
	artifactRoot := filepath.Join(tempRoot, "artifacts")
	for _, commandName := range []string{"validate", "plan"} {
		command := exec.CommandContext(ctx, providerBinary, commandName, "-f", example)
		command.Dir = repoRoot
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("Provider %s failed: %v\n%s", commandName, err, output)
		}
	}
	unsupportedOutput := filepath.Join(tempRoot, "unsupported")
	unsupported := exec.CommandContext(
		ctx, providerBinary, "generate-device-config",
		"-f", filepath.Join(repoRoot, "examples", "network-deployment-unsupported-ospf.yaml"),
		"-out", unsupportedOutput,
	)
	unsupported.Dir = repoRoot
	if output, err := unsupported.CombinedOutput(); err == nil {
		t.Fatalf("unsupported OSPF generation unexpectedly succeeded: %s", output)
	}
	for _, path := range []string{
		filepath.Join(unsupportedOutput, "demo", "R1", "config.cfg"),
		filepath.Join(unsupportedOutput, "demo", "generation-report.json"),
	} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("unsupported generation published partial artifact %s: %v", path, err)
		}
	}
	generate := exec.CommandContext(ctx, providerBinary, "generate-all", "-f", example, "-out", artifactRoot)
	generate.Dir = repoRoot
	if output, err := generate.CombinedOutput(); err != nil {
		t.Fatalf("generate four-site catalog: %v\n%s", err, output)
	}

	credentials := []siteCredential{
		{siteID: "site-a", agentID: "agent-a", token: strings.Repeat("a", 32)},
		{siteID: "site-b", agentID: "agent-b", token: strings.Repeat("b", 32)},
		{siteID: "site-c", agentID: "agent-c", token: strings.Repeat("c", 32)},
		{siteID: "site-d", agentID: "agent-d", token: strings.Repeat("d", 32)},
	}
	grpcPort := freeTCPPort(t)
	apiPort := freeTCPPort(t)
	for grpcPort == apiPort {
		apiPort = freeTCPPort(t)
	}
	globalToken := strings.Repeat("g", 32)
	providerConfigPath := filepath.Join(tempRoot, "provider.yaml")
	providerConfig := fmt.Sprintf(
		"listen_address: 127.0.0.1:%d\napi_listen_address: 127.0.0.1:%d\nartifact_directory: %s\ntoken_env: INFRAFLOW_E2E_GLOBAL_TOKEN\nknown_hosts_file: %s\nlogging:\n  directory: %s\nagent_credentials:\n",
		grpcPort, apiPort, yamlQuote(artifactRoot),
		yamlQuote(filepath.Join(tempRoot, "known_hosts")),
		yamlQuote(filepath.Join(tempRoot, "provider-logs")),
	)
	for index, credential := range credentials {
		providerConfig += fmt.Sprintf(
			"  - agent_id: %s\n    site_id: %s\n    token_env: INFRAFLOW_E2E_SITE_%d_TOKEN\n",
			credential.agentID, credential.siteID, index,
		)
	}
	if err := os.WriteFile(providerConfigPath, []byte(providerConfig), 0o600); err != nil {
		t.Fatal(err)
	}

	providerContext, stopProvider := context.WithCancel(ctx)
	provider := exec.CommandContext(providerContext, providerBinary, "serve", "-config", providerConfigPath)
	provider.Dir = repoRoot
	provider.Env = append(os.Environ(), "INFRAFLOW_E2E_GLOBAL_TOKEN="+globalToken)
	for index, credential := range credentials {
		provider.Env = append(provider.Env, fmt.Sprintf("INFRAFLOW_E2E_SITE_%d_TOKEN=%s", index, credential.token))
	}
	var providerOutput bytes.Buffer
	provider.Stdout = &providerOutput
	provider.Stderr = &providerOutput
	if err := provider.Start(); err != nil {
		stopProvider()
		t.Fatalf("start Provider: %v", err)
	}
	providerDone := make(chan struct{})
	var providerErr error
	go func() {
		providerErr = provider.Wait()
		close(providerDone)
	}()
	t.Cleanup(func() {
		stopProvider()
		select {
		case <-providerDone:
		case <-time.After(5 * time.Second):
			t.Errorf("Provider did not stop; output: %s", providerOutput.String())
		}
	})
	waitForTCP(t, ctx, fmt.Sprintf("127.0.0.1:%d", grpcPort), providerDone, &providerErr, &providerOutput)
	waitForTCP(t, ctx, fmt.Sprintf("127.0.0.1:%d", apiPort), providerDone, &providerErr, &providerOutput)

	for _, credential := range credentials {
		agentRoot := filepath.Join(tempRoot, credential.agentID)
		stateRoot := filepath.Join(agentRoot, "state")
		configPath := filepath.Join(agentRoot, "agent.yaml")
		if err := os.MkdirAll(agentRoot, 0o700); err != nil {
			t.Fatal(err)
		}
		agentConfig := fmt.Sprintf(
			"agent:\n  id: %s\n  site_id: %s\n  state_directory: %s\nprovider:\n  address: 127.0.0.1:%d\n  api_address: http://127.0.0.1:%d\n  token_env: INFRAFLOW_E2E_GLOBAL_TOKEN\n  artifact_token_env: INFRAFLOW_E2E_SITE_TOKEN\n  tls:\n    enabled: false\nlogging:\n  directory: %s\n",
			credential.agentID, credential.siteID, yamlQuote(stateRoot), grpcPort, apiPort,
			yamlQuote(filepath.Join(agentRoot, "logs")),
		)
		if err := os.WriteFile(configPath, []byte(agentConfig), 0o600); err != nil {
			t.Fatal(err)
		}
		runContext, stopRun := context.WithTimeout(ctx, 30*time.Second)
		run := exec.CommandContext(runContext, agentBinary, "run", "-config", configPath)
		run.Dir = repoRoot
		run.Env = append(os.Environ(),
			"INFRAFLOW_E2E_GLOBAL_TOKEN="+globalToken,
			"INFRAFLOW_E2E_SITE_TOKEN="+credential.token,
		)
		output, runErr := run.CombinedOutput()
		stopRun()
		if runErr != nil {
			t.Fatalf("run %s: %v\n%s", credential.agentID, runErr, output)
		}
		if !bytes.Contains(output, []byte("reported execution ")) {
			t.Fatalf("%s did not report its execution: %s", credential.agentID, output)
		}

		manifestPath := filepath.Join(artifactRoot, credential.siteID, "manifest.json")
		manifestBytes, err := os.ReadFile(manifestPath)
		if err != nil {
			t.Fatalf("read %s manifest: %v", credential.siteID, err)
		}
		var manifest struct {
			Artifacts []protocol.Artifact `json:"artifacts"`
		}
		if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
			t.Fatalf("decode %s manifest: %v", credential.siteID, err)
		}
		if len(manifest.Artifacts) == 0 {
			t.Fatalf("%s manifest contains no artifacts", credential.siteID)
		}
		bootstrapDHCPFound := false
		for _, artifact := range manifest.Artifacts {
			if !strings.HasPrefix(artifact.Path, credential.siteID+"/") {
				t.Errorf("%s manifest contains out-of-scope artifact %q", credential.siteID, artifact.Path)
				continue
			}
			if !bytes.Contains(output, []byte(protocol.StatusCompleted+" "+artifact.Path)) {
				t.Errorf("%s did not report completed transfer for %s", credential.agentID, artifact.Path)
			}
			data, err := os.ReadFile(filepath.Join(stateRoot, filepath.FromSlash(artifact.Path)))
			if err != nil {
				t.Errorf("%s did not persist %s: %v", credential.agentID, artifact.Path, err)
				continue
			}
			if got := protocol.SHA256(data); got != artifact.OutputHash {
				t.Errorf("%s stored %s with hash %s, expected %s", credential.agentID, artifact.Path, got, artifact.OutputHash)
			}
			if artifact.Type == "bootstrap_dhcp" {
				bootstrapDHCPFound = true
				var service struct {
					Enabled bool `json:"enabled"`
				}
				if err := json.Unmarshal(data, &service); err != nil || service.Enabled {
					t.Errorf("%s DHCP bootstrap artifact must remain disabled: enabled=%t err=%v", credential.siteID, service.Enabled, err)
				}
			}
		}
		if !bootstrapDHCPFound {
			t.Errorf("%s manifest omitted its explicit disabled bootstrap artifact", credential.siteID)
		}
		for _, other := range credentials {
			if other.siteID == credential.siteID {
				continue
			}
			if _, err := os.Stat(filepath.Join(stateRoot, other.siteID)); !os.IsNotExist(err) {
				t.Errorf("%s received another site's artifacts (%s): %v", credential.agentID, other.siteID, err)
			}
		}
	}
	reportFile, err := os.Open(filepath.Join(artifactRoot, ".agent-reports.jsonl"))
	if err != nil {
		t.Fatalf("Provider did not persist Agent reports: %v", err)
	}
	defer reportFile.Close()
	reportScanner := bufio.NewScanner(reportFile)
	reportScanner.Buffer(make([]byte, 4096), 1<<20)
	reportedAgents := make(map[string]string, len(credentials))
	reportCount := 0
	for reportScanner.Scan() {
		reportCount++
		var report protocol.AgentReport
		if err := json.Unmarshal(reportScanner.Bytes(), &report); err != nil {
			t.Fatalf("decode Provider report: %v", err)
		}
		siteID := ""
		for _, credential := range credentials {
			if credential.agentID == report.AgentID {
				siteID = credential.siteID
				break
			}
		}
		if siteID == "" {
			t.Errorf("Provider stored a report for an unknown Agent %q", report.AgentID)
			continue
		}
		if _, exists := reportedAgents[report.AgentID]; exists {
			t.Errorf("Provider persisted duplicate reports for Agent %q", report.AgentID)
		}
		for _, result := range report.Artifacts {
			if !strings.HasPrefix(result.Path, siteID+"/") || result.Status != protocol.StatusCompleted {
				t.Errorf("Provider stored an out-of-scope or incomplete report for %s: %#v", report.AgentID, result)
			}
		}
		reportedAgents[report.AgentID] = siteID
	}
	if err := reportScanner.Err(); err != nil {
		t.Fatalf("read Provider reports: %v", err)
	}
	if reportCount != len(credentials) || len(reportedAgents) != len(credentials) {
		t.Errorf("Provider persisted %d reports for %d Agents, want exactly %d", reportCount, len(reportedAgents), len(credentials))
	}
}

func buildBinary(t *testing.T, ctx context.Context, repoRoot, outputPath, packagePath string) {
	t.Helper()
	command := exec.CommandContext(ctx, "go", "build", "-o", outputPath, packagePath)
	command.Dir = repoRoot
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build %s: %v\n%s", packagePath, err, output)
	}
}

func freeTCPPort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	_, portText, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatal(err)
	}
	return port
}

func waitForTCP(t *testing.T, ctx context.Context, address string, providerDone <-chan struct{}, providerErr *error, output *bytes.Buffer) {
	t.Helper()
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		connection, err := net.DialTimeout("tcp", address, 100*time.Millisecond)
		if err == nil {
			_ = connection.Close()
			return
		}
		select {
		case <-providerDone:
			t.Fatalf("Provider exited before becoming ready: %v\n%s", *providerErr, output.String())
		case <-ctx.Done():
			t.Fatalf("Provider did not listen on %s: %v\n%s", address, ctx.Err(), output.String())
		case <-ticker.C:
		}
	}
}

func yamlQuote(value string) string {
	encoded, _ := json.Marshal(value)
	return string(encoded)
}
