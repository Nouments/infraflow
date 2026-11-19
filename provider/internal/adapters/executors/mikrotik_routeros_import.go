package executors

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/netip"
	"strings"
	"time"

	"infraflow/internal/domain"
	"infraflow/internal/ports"
	"infraflow/pkg/observability"
	"infraflow/pkg/protocol"
	"infraflow/provider/internal/infrastructure/credentials"
)

// RouterOSImportType is the task type that delivers a published RouterOS
// Auto Import script to a device and verifies the resulting state.
const RouterOSImportType = "mikrotik.routeros.import"

const (
	maxRouterOSScriptBytes = 256 << 10
	routerOSImportSuccess  = "Script file loaded and executed successfully"
)

// RouterOSImportParameters is the strict task schema.
type RouterOSImportParameters struct {
	Site     string `json:"site"`
	DeviceID string `json:"device_id"`
}

// RouterOSImportResult is the structured evidence of one execution.
type RouterOSImportResult struct {
	Operation      string           `json:"operation"`
	Site           string           `json:"site"`
	DeviceID       string           `json:"device_id"`
	ExecutedAt     string           `json:"executed_at"`
	Status         string           `json:"status"`
	Artifact       string           `json:"artifact"`
	ArtifactSHA256 string           `json:"artifact_sha256"`
	DeviceVersion  string           `json:"device_version,omitempty"`
	UploadedFile   string           `json:"uploaded_file,omitempty"`
	ImportOutput   string           `json:"import_output,omitempty"`
	Observed       RouterOSObserved `json:"observed"`
	Mismatches     []string         `json:"mismatches,omitempty"`
	Verified       bool             `json:"verified"`
	Error          string           `json:"error,omitempty"`
	Warnings       []string         `json:"warnings,omitempty"`
}

// RouterOSObserved is the device state read back after the import.
type RouterOSObserved struct {
	Identity      string              `json:"identity"`
	Addresses     []map[string]string `json:"addresses"`
	DHCPClients   []map[string]string `json:"dhcp_clients,omitempty"`
	Routes        []map[string]string `json:"routes"`
	OSPFInstances []map[string]string `json:"ospf_instances,omitempty"`
}

// RouterOSImportExecutor implements ports.ExecutionExecutor.
type RouterOSImportExecutor struct {
	targets     DeviceTargetResolver
	credentials credentials.DeviceCredentialResolver
	transport   RouterOSTransport
	artifacts   ports.ArtifactRepository
}

// NewRouterOSImportExecutor wires the executor. targets must authorize only
// MikroTik devices and return their published intent.
func NewRouterOSImportExecutor(targets DeviceTargetResolver, creds credentials.DeviceCredentialResolver, transport RouterOSTransport, artifacts ports.ArtifactRepository) (*RouterOSImportExecutor, error) {
	if targets == nil || creds == nil || transport == nil || artifacts == nil {
		return nil, errors.New("RouterOS import executor requires a target resolver, credential resolver, transport and artifact repository")
	}
	return &RouterOSImportExecutor{targets: targets, credentials: creds, transport: transport, artifacts: artifacts}, nil
}

func (e *RouterOSImportExecutor) Type() string { return RouterOSImportType }

// Retryable is false: an import that failed half-way has already changed the device.
func (e *RouterOSImportExecutor) Retryable(error) bool { return false }

func (e *RouterOSImportExecutor) ValidateParameters(parameters json.RawMessage) (json.RawMessage, error) {
	if len(parameters) == 0 || len(parameters) > 4<<10 {
		return nil, errors.New("task parameters must be a JSON object of at most 4 KiB")
	}
	decoder := json.NewDecoder(bytes.NewReader(parameters))
	decoder.DisallowUnknownFields()
	var params RouterOSImportParameters
	if err := decoder.Decode(&params); err != nil {
		return nil, fmt.Errorf("invalid parameters: %w", err)
	}
	if decoder.More() {
		return nil, errors.New("task parameters contain trailing JSON data")
	}
	params.Site = strings.TrimSpace(params.Site)
	params.DeviceID = strings.TrimSpace(params.DeviceID)
	if !protocol.ValidSiteName(params.Site) || !protocol.ValidSiteName(params.DeviceID) {
		return nil, errors.New("site and device_id must be valid identifiers")
	}
	return json.Marshal(params)
}

func (e *RouterOSImportExecutor) Execute(ctx context.Context, task domain.ExecutionTask) (ports.ExecutionResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	var params RouterOSImportParameters
	if err := json.Unmarshal(task.Parameters, &params); err != nil {
		return failed(RouterOSImportResult{}, "invalid task parameters format")
	}
	result := RouterOSImportResult{Operation: RouterOSImportType, Site: params.Site, DeviceID: params.DeviceID}

	target, err := e.targets.ResolveDevice(ctx, params.Site, params.DeviceID)
	if err != nil {
		return failed(result, "target device authorization failed: "+observability.Redact(err.Error()))
	}
	intent := target.Intent
	if intent == nil || intent.Provisioning.Method != domain.ProvisioningMethod("auto-rsc") {
		return failed(result, "target has no published auto-rsc intent; generate and publish the site first")
	}
	release, err := sharedDeviceLocks.Acquire(ctx, params.Site, params.DeviceID)
	if err != nil {
		return failed(result, err.Error()+"; no configuration was imported")
	}
	defer release()
	script, artifactPath, digest, err := e.loadScript(params.Site, intent.Name)
	result.Artifact, result.ArtifactSHA256 = artifactPath, digest
	if err != nil {
		return failed(result, err.Error())
	}
	creds, err := e.credentials.ResolveDeviceCredentials(ctx, params.Site, params.DeviceID)
	if err != nil {
		return failed(result, "credential resolution failed: "+observability.Redact(err.Error()))
	}
	client, err := e.transport.Connect(ctx, target, creds)
	if err != nil {
		return failed(result, fmt.Sprintf("SSH connection to %s (%s) failed: %s; no configuration was imported", params.DeviceID, target.ManagementIP, sanitizeErrorWithCreds(err, creds)))
	}
	defer client.Close()

	version, err := client.Run(ctx, ":put [/system resource get version]")
	if err != nil {
		return failed(result, "preflight failed: "+sanitizeErrorWithCreds(err, creds)+"; no configuration was imported")
	}
	result.DeviceVersion = strings.TrimSpace(version)
	if !strings.HasPrefix(result.DeviceVersion, "7.") {
		return failed(result, fmt.Sprintf("device reports RouterOS %q; the published script targets RouterOS 7; no configuration was imported", result.DeviceVersion))
	}

	// The upload name deliberately avoids the .auto.rsc suffix so RouterOS
	// does not execute a partially transferred file on its own.
	fileName := "infraflow-" + strings.ToLower(intent.Name) + ".rsc"
	result.UploadedFile = fileName
	if err := client.Upload(ctx, fileName, script); err != nil {
		cleanup := e.removeFile(client, fileName)
		return failed(result, "script upload failed: "+sanitizeErrorWithCreds(err, creds)+"; no configuration was imported"+cleanup)
	}
	importOutput, importErr := client.Run(ctx, fmt.Sprintf(`/import file-name=%s verbose=no`, fileName))
	result.ImportOutput = observability.Redact(strings.TrimSpace(importOutput))
	cleanup := e.removeFile(client, fileName)
	if cleanup != "" {
		result.Warnings = append(result.Warnings, strings.TrimPrefix(cleanup, "; "))
	}
	if importErr != nil || !strings.Contains(importOutput, routerOSImportSuccess) {
		reason := "RouterOS did not report a successful import"
		if importErr != nil {
			reason = sanitizeErrorWithCreds(importErr, creds)
		}
		result.Observed, _ = readRouterOSState(ctx, client)
		return failed(result, "import failed: "+reason+"; RouterOS stops at the first failing command, so earlier commands may be applied; device state must be re-read before any retry")
	}

	observed, err := readRouterOSState(ctx, client)
	result.Observed = observed
	if err != nil {
		return failed(result, "read-back failed after import: "+sanitizeErrorWithCreds(err, creds))
	}
	result.Mismatches = compareRouterOSState(*intent, observed)
	if len(result.Mismatches) > 0 {
		return failed(result, "imported configuration does not match the published intent: "+strings.Join(result.Mismatches, "; "))
	}
	result.Verified = true
	result.Status = string(domain.ExecutionTaskSucceeded)
	result.ExecutedAt = time.Now().UTC().Format(time.RFC3339Nano)
	result.Warnings = append(result.Warnings, "RouterOS persists configuration immediately; no rollback is performed by this executor")
	output, _ := json.Marshal(result)
	return ports.ExecutionResult{Status: domain.ExecutionTaskSucceeded, Output: output}, nil
}

func failed(result RouterOSImportResult, message string) (ports.ExecutionResult, error) {
	result.Status = string(domain.ExecutionTaskFailed)
	result.Error = message
	result.ExecutedAt = time.Now().UTC().Format(time.RFC3339Nano)
	output, _ := json.Marshal(result)
	return ports.ExecutionResult{Status: domain.ExecutionTaskFailed, Error: message, Output: output}, errors.New(message)
}

// loadScript reads the published artifact through the repository, which
// rejects catalog hash mismatches, and re-hashes the bytes it will send.
func (e *RouterOSImportExecutor) loadScript(site, deviceName string) ([]byte, string, string, error) {
	artifactPath := site + "/native/device-" + deviceName + ".auto.rsc"
	reader, published, err := e.artifacts.Open(artifactPath)
	if err != nil {
		return nil, artifactPath, "", fmt.Errorf("published script %s is unavailable or failed its integrity check: %v", artifactPath, err)
	}
	defer reader.Close()
	script, err := io.ReadAll(io.LimitReader(reader, maxRouterOSScriptBytes+1))
	if err != nil {
		return nil, artifactPath, "", fmt.Errorf("read published script: %v", err)
	}
	if len(script) > maxRouterOSScriptBytes {
		return nil, artifactPath, "", fmt.Errorf("published script exceeds %d bytes", maxRouterOSScriptBytes)
	}
	sum := sha256.Sum256(script)
	digest := hex.EncodeToString(sum[:])
	if !protocol.IsSHA256(published.OutputHash) || digest != published.OutputHash {
		return nil, artifactPath, digest, fmt.Errorf("published script %s does not match its catalog hash; refusing to deliver it", artifactPath)
	}
	return script, artifactPath, digest, nil
}

func (e *RouterOSImportExecutor) removeFile(client RouterOSClient, name string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := client.Run(ctx, fmt.Sprintf(`/file remove [find name="%s"]`, name)); err != nil {
		return fmt.Sprintf("; uploaded file %s could not be removed: %v", name, err)
	}
	return ""
}

func readRouterOSState(ctx context.Context, client RouterOSClient) (RouterOSObserved, error) {
	var observed RouterOSObserved
	identity, err := client.Run(ctx, ":put [/system identity get name]")
	if err != nil {
		return observed, err
	}
	observed.Identity = strings.TrimSpace(identity)
	for _, query := range []struct {
		command string
		into    *[]map[string]string
	}{
		{"/ip address print terse", &observed.Addresses},
		{"/ip dhcp-client print terse", &observed.DHCPClients},
		{"/ip route print terse", &observed.Routes},
		{"/routing ospf instance print terse", &observed.OSPFInstances},
	} {
		output, err := client.Run(ctx, query.command)
		if err != nil {
			return observed, err
		}
		*query.into = parseRouterOSTerse(output)
	}
	return observed, nil
}

// parseRouterOSTerse parses "print terse" lines into key/value maps. Flags
// and the item number before the first key are kept under "_flags".
func parseRouterOSTerse(output string) []map[string]string {
	var entries []map[string]string
	for _, line := range strings.Split(strings.ReplaceAll(output, "\r", ""), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || !strings.Contains(line, "=") {
			continue
		}
		entry := map[string]string{}
		var flags []string
		for _, token := range splitTerseTokens(line) {
			key, value, ok := strings.Cut(token, "=")
			if !ok {
				flags = append(flags, token)
				continue
			}
			entry[key] = strings.Trim(value, `"`)
		}
		entry["_flags"] = strings.Join(flags, " ")
		entries = append(entries, entry)
	}
	return entries
}

func splitTerseTokens(line string) []string {
	var tokens []string
	var current strings.Builder
	quoted := false
	for _, r := range line {
		switch {
		case r == '"':
			quoted = !quoted
			current.WriteRune(r)
		case r == ' ' && !quoted:
			if current.Len() > 0 {
				tokens = append(tokens, current.String())
				current.Reset()
			}
		default:
			current.WriteRune(r)
		}
	}
	if current.Len() > 0 {
		tokens = append(tokens, current.String())
	}
	return tokens
}

// compareRouterOSState checks every element of the published intent that the
// auto-rsc template renders.
func compareRouterOSState(intent domain.Device, observed RouterOSObserved) []string {
	var mismatches []string
	if observed.Identity != intent.Name {
		mismatches = append(mismatches, fmt.Sprintf("identity is %q, expected %q", observed.Identity, intent.Name))
	}
	if intent.Network != nil {
		for _, iface := range intent.Network.Interfaces {
			switch iface.IPv4Mode {
			case "static":
				if !hasEntry(observed.Addresses, map[string]string{"address": iface.IPv4Address, "interface": iface.Name}) {
					mismatches = append(mismatches, fmt.Sprintf("address %s on %s not found", iface.IPv4Address, iface.Name))
				}
			case "dhcp":
				if !hasEntry(observed.DHCPClients, map[string]string{"interface": iface.Name}) {
					mismatches = append(mismatches, fmt.Sprintf("DHCP client on %s not found", iface.Name))
				}
			}
		}
		for _, route := range intent.Network.Routes {
			if !hasEntry(observed.Routes, map[string]string{"dst-address": route.Destination, "gateway": route.NextHop}) {
				mismatches = append(mismatches, fmt.Sprintf("route %s via %s not found", route.Destination, route.NextHop))
			}
		}
	}
	if intent.Routing != nil && intent.Routing.OSPF.IsEnabled() {
		want := map[string]string{"name": "infraflow-v2"}
		if intent.Routing.OSPF.RouterID != "" {
			want["router-id"] = intent.Routing.OSPF.RouterID
		}
		if !hasEntry(observed.OSPFInstances, want) {
			mismatches = append(mismatches, "OSPF instance infraflow-v2 with the expected router-id not found")
		}
	}
	return mismatches
}

func hasEntry(entries []map[string]string, want map[string]string) bool {
	for _, entry := range entries {
		if strings.Contains(entry["_flags"], "X") { // disabled
			continue
		}
		match := true
		for key, value := range want {
			if !sameRouterOSValue(entry[key], value) {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

func sameRouterOSValue(got, want string) bool {
	if got == want {
		return true
	}
	gotPrefix, err1 := netip.ParsePrefix(got)
	wantPrefix, err2 := netip.ParsePrefix(want)
	return err1 == nil && err2 == nil && gotPrefix == wantPrefix
}
