package executors

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"regexp"
	"strings"
	"time"

	"infraflow/internal/domain"
	"infraflow/internal/ports"
	"infraflow/pkg/observability"
	"infraflow/pkg/protocol"
	"infraflow/provider/internal/infrastructure/credentials"
)

const (
	// CiscoIOSConfigureType is the registered task type for configuring Cisco IOS devices over SSH.
	CiscoIOSConfigureType = "cisco.ios.configure"

	maxCiscoParamsLength = 64 << 10 // 64 KiB
	maxInterfacesCount   = 32
	maxDescriptionLength = 240
	maxHostnameLength    = 63
)

// hostnamePattern enforces RFC 1123 / Cisco IOS hostname restrictions.
var hostnamePattern = regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9-]*[a-zA-Z0-9])?$`)

// ciscoInterfaceRegex matches standard Ethernet-family interface names and common prefixes.
var ciscoInterfaceRegex = regexp.MustCompile(`^(?i)(Ethernet|FastEthernet|GigabitEthernet|TenGigabitEthernet|TwentyFiveGigE|FortyGigabitEthernet|HundredGigE|Eth|Fa|Gi|Te|Twe|Fo|Hu)(\d+(?:[/.][0-9]+)*)$`)

// CiscoIOSConfigureParameters defines the strict JSON payload schema for cisco.ios.configure tasks.
type CiscoIOSConfigureParameters struct {
	Site       string                   `json:"site"`
	DeviceID   string                   `json:"device_id"`
	Hostname   string                   `json:"hostname,omitempty"`
	Interfaces []InterfaceConfiguration `json:"interfaces"`
}

// InterfaceConfiguration defines the typed configuration for a single authorized interface.
type InterfaceConfiguration struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	IPv4CIDR    string `json:"ipv4_cidr,omitempty"`
	AdminState  string `json:"admin_state"`
}

// CiscoIOSConfigureResult represents the structured, verified evidence returned upon execution.
type CiscoIOSConfigureResult struct {
	Operation        string           `json:"operation"`
	Site             string           `json:"site"`
	DeviceID         string           `json:"device_id"`
	Vendor           string           `json:"vendor"`
	Family           string           `json:"family"`
	ExecutedAt       string           `json:"executed_at"`
	Status           string           `json:"status"`
	RequestedChanges RequestedChanges `json:"requested_changes"`
	ObservedChanges  ObservedChanges  `json:"observed_changes"`
	Verified         bool             `json:"verified"`
	Warnings         []string         `json:"warnings,omitempty"`
}

// RequestedChanges mirrors the requested changes.
type RequestedChanges struct {
	Hostname   string                   `json:"hostname,omitempty"`
	Interfaces []InterfaceConfiguration `json:"interfaces"`
}

// ObservedChanges captures the verified state observed on the router during read-back.
type ObservedChanges struct {
	Hostname   string                    `json:"hostname,omitempty"`
	Interfaces []ObservedInterfaceConfig `json:"interfaces"`
}

// ObservedInterfaceConfig represents the verified interface state read back from running-config.
type ObservedInterfaceConfig struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	IPv4Address string `json:"ipv4_address,omitempty"`
	SubnetMask  string `json:"subnet_mask,omitempty"`
	AdminState  string `json:"admin_state"`
}

// CiscoIOSConfigureExecutor implements ports.ExecutionExecutor to securely apply and verify typed Cisco IOS configurations.
type CiscoIOSConfigureExecutor struct {
	targetResolver     DeviceTargetResolver
	credentialResolver credentials.DeviceCredentialResolver
	transport          CiscoIOSTransport
}

// NewCiscoIOSConfigureExecutor constructs a new Cisco IOS configure executor.
func NewCiscoIOSConfigureExecutor(
	targetResolver DeviceTargetResolver,
	credentialResolver credentials.DeviceCredentialResolver,
	transport CiscoIOSTransport,
) (*CiscoIOSConfigureExecutor, error) {
	if targetResolver == nil {
		return nil, errors.New("target resolver is required")
	}
	if credentialResolver == nil {
		return nil, errors.New("credential resolver is required")
	}
	if transport == nil {
		return nil, errors.New("cisco ios transport is required")
	}
	return &CiscoIOSConfigureExecutor{
		targetResolver:     targetResolver,
		credentialResolver: credentialResolver,
		transport:          transport,
	}, nil
}

// Type returns "cisco.ios.configure".
func (e *CiscoIOSConfigureExecutor) Type() string {
	return CiscoIOSConfigureType
}

// ValidateParameters strictly parses, validates, and canonicalizes task parameters.
func (e *CiscoIOSConfigureExecutor) ValidateParameters(parameters json.RawMessage) (json.RawMessage, error) {
	if len(parameters) == 0 {
		return nil, errors.New("task parameters must not be empty")
	}
	if len(parameters) > maxCiscoParamsLength {
		return nil, fmt.Errorf("task parameters exceed maximum allowed size of %d bytes", maxCiscoParamsLength)
	}

	decoder := json.NewDecoder(bytes.NewReader(parameters))
	decoder.DisallowUnknownFields()
	var params CiscoIOSConfigureParameters
	if err := decoder.Decode(&params); err != nil {
		return nil, fmt.Errorf("invalid parameters: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return nil, errors.New("task parameters contain trailing JSON data")
	}

	// Validate Site
	params.Site = strings.TrimSpace(params.Site)
	if params.Site == "" {
		return nil, errors.New("site is required")
	}
	if !protocol.ValidSiteName(params.Site) || len(params.Site) > 64 {
		return nil, fmt.Errorf("invalid site name %q", params.Site)
	}

	// Validate DeviceID
	params.DeviceID = strings.TrimSpace(params.DeviceID)
	if params.DeviceID == "" {
		return nil, errors.New("device_id is required")
	}
	if !protocol.ValidSiteName(params.DeviceID) || len(params.DeviceID) > 64 {
		return nil, fmt.Errorf("invalid device_id %q", params.DeviceID)
	}

	// Validate Hostname if specified
	if params.Hostname != "" {
		params.Hostname = strings.TrimSpace(params.Hostname)
		if len(params.Hostname) > maxHostnameLength || !hostnamePattern.MatchString(params.Hostname) {
			return nil, fmt.Errorf("invalid hostname %q (must be RFC 1123 compliant, max %d characters)", params.Hostname, maxHostnameLength)
		}
	}

	// Validate Interfaces
	if len(params.Interfaces) == 0 {
		return nil, errors.New("at least one interface configuration is required")
	}
	if len(params.Interfaces) > maxInterfacesCount {
		return nil, fmt.Errorf("too many interfaces: maximum permitted is %d", maxInterfacesCount)
	}

	seenInterfaces := make(map[string]struct{}, len(params.Interfaces))
	canonicalInterfaces := make([]InterfaceConfiguration, 0, len(params.Interfaces))

	for i, iface := range params.Interfaces {
		canonicalName, err := validateAndCanonicalizeInterfaceName(iface.Name)
		if err != nil {
			return nil, fmt.Errorf("interface[%d]: %w", i, err)
		}
		nameKey := strings.ToLower(canonicalName)
		if _, exists := seenInterfaces[nameKey]; exists {
			return nil, fmt.Errorf("duplicate interface %q at index %d", canonicalName, i)
		}
		seenInterfaces[nameKey] = struct{}{}

		// Validate Description
		cleanDesc := strings.TrimSpace(iface.Description)
		if cleanDesc != "" {
			if len(cleanDesc) > maxDescriptionLength {
				return nil, fmt.Errorf("interface %q description exceeds maximum length of %d", canonicalName, maxDescriptionLength)
			}
			for _, r := range cleanDesc {
				if r < 32 || r > 126 {
					return nil, fmt.Errorf("interface %q description contains invalid character (code %d)", canonicalName, r)
				}
			}
		}

		// Validate IPv4 CIDR
		var canonicalCIDR string
		if iface.IPv4CIDR != "" {
			cleanCIDR := strings.TrimSpace(iface.IPv4CIDR)
			ip, ipNet, err := net.ParseCIDR(cleanCIDR)
			if err != nil {
				return nil, fmt.Errorf("interface %q has invalid ipv4_cidr %q: %w", canonicalName, cleanCIDR, err)
			}
			ipv4 := ip.To4()
			if ipv4 == nil {
				return nil, fmt.Errorf("interface %q ipv4_cidr %q is not an IPv4 address", canonicalName, cleanCIDR)
			}
			ones, bits := ipNet.Mask.Size()
			if bits != 32 || ones < 1 || ones > 32 {
				return nil, fmt.Errorf("interface %q ipv4_cidr %q has invalid prefix length", canonicalName, cleanCIDR)
			}
			if ipv4.IsMulticast() || ipv4.IsLoopback() || ipv4.Equal(net.IPv4bcast) || ipv4.Equal(net.IPv4zero) {
				return nil, fmt.Errorf("interface %q ipv4_cidr %q contains disallowed IP address", canonicalName, cleanCIDR)
			}
			canonicalCIDR = fmt.Sprintf("%s/%d", ipv4.String(), ones)
		}

		// Validate AdminState
		cleanAdmin := strings.ToLower(strings.TrimSpace(iface.AdminState))
		if cleanAdmin != "up" && cleanAdmin != "down" {
			return nil, fmt.Errorf("interface %q admin_state must be 'up' or 'down', got %q", canonicalName, iface.AdminState)
		}

		canonicalInterfaces = append(canonicalInterfaces, InterfaceConfiguration{
			Name:        canonicalName,
			Description: cleanDesc,
			IPv4CIDR:    canonicalCIDR,
			AdminState:  cleanAdmin,
		})
	}
	params.Interfaces = canonicalInterfaces

	canonicalJSON, err := json.Marshal(params)
	if err != nil {
		return nil, fmt.Errorf("canonicalize parameters: %w", err)
	}
	return canonicalJSON, nil
}

// Execute performs target authorization, credentials resolution, SSH transport connection,
// typed configuration application, and read-back verification.
func (e *CiscoIOSConfigureExecutor) Execute(ctx context.Context, task domain.ExecutionTask) (ports.ExecutionResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Err() != nil {
		return ports.ExecutionResult{Status: domain.ExecutionTaskCancelled}, ctx.Err()
	}

	var params CiscoIOSConfigureParameters
	if err := json.Unmarshal(task.Parameters, &params); err != nil {
		errMsg := "invalid task parameters format"
		return ports.ExecutionResult{Status: domain.ExecutionTaskFailed, Error: errMsg}, errors.New(errMsg)
	}

	// 1. Resolve and authorize target
	target, err := e.targetResolver.ResolveDevice(ctx, params.Site, params.DeviceID)
	if err != nil {
		safeMsg := fmt.Sprintf("target device authorization failed: %s", observability.Redact(err.Error()))
		return ports.ExecutionResult{Status: domain.ExecutionTaskFailed, Error: safeMsg}, errors.New(safeMsg)
	}

	// 2. Resolve credentials safely
	creds, err := e.credentialResolver.ResolveDeviceCredentials(ctx, params.Site, params.DeviceID)
	if err != nil {
		safeMsg := fmt.Sprintf("target credentials resolution failed for %s/%s: %s", params.Site, params.DeviceID, observability.Redact(err.Error()))
		return ports.ExecutionResult{Status: domain.ExecutionTaskFailed, Error: safeMsg}, errors.New(safeMsg)
	}

	// 3. Connect via SSH transport
	session, err := e.transport.Connect(ctx, target, creds)
	if err != nil {
		safeMsg := fmt.Sprintf("SSH connection to target %s (%s) failed: %s", params.DeviceID, target.ManagementIP, observability.Redact(err.Error()))
		return ports.ExecutionResult{Status: domain.ExecutionTaskFailed, Error: safeMsg}, errors.New(safeMsg)
	}
	defer session.Close()

	if ctx.Err() != nil {
		return ports.ExecutionResult{Status: domain.ExecutionTaskCancelled}, ctx.Err()
	}

	// 4. Initial prompt & terminal setup
	if _, err := session.ReadInitialPrompt(ctx); err != nil {
		safeMsg := fmt.Sprintf("initial prompt detection failed: %s", observability.Redact(err.Error()))
		return ports.ExecutionResult{Status: domain.ExecutionTaskFailed, Error: safeMsg}, errors.New(safeMsg)
	}

	if _, err := session.RunCommand(ctx, "terminal length 0"); err != nil {
		safeMsg := fmt.Sprintf("set terminal length failed: %s", observability.Redact(err.Error()))
		return ports.ExecutionResult{Status: domain.ExecutionTaskFailed, Error: safeMsg}, errors.New(safeMsg)
	}
	if _, err := session.RunCommand(ctx, "terminal width 0"); err != nil {
		safeMsg := fmt.Sprintf("set terminal width failed: %s", observability.Redact(err.Error()))
		return ports.ExecutionResult{Status: domain.ExecutionTaskFailed, Error: safeMsg}, errors.New(safeMsg)
	}

	// 5. Enter configuration mode
	if _, err := session.RunCommand(ctx, "configure terminal"); err != nil {
		safeMsg := fmt.Sprintf("enter configure terminal failed: %s", observability.Redact(err.Error()))
		return ports.ExecutionResult{Status: domain.ExecutionTaskFailed, Error: safeMsg}, errors.New(safeMsg)
	}

	// 6. Build and apply typed commands
	var appliedCommands []string
	var configErr error
	mutationMayHaveStarted := false

	if params.Hostname != "" {
		cmd := "hostname " + params.Hostname
		mutationMayHaveStarted = true
		if _, err := session.RunCommand(ctx, cmd); err != nil {
			configErr = fmt.Errorf("failed executing %q: %w", cmd, err)
		} else {
			appliedCommands = append(appliedCommands, cmd)
		}
	}

	if configErr == nil {
		for _, iface := range params.Interfaces {
			if ctx.Err() != nil {
				configErr = ctx.Err()
				break
			}
			ifaceCmd := "interface " + iface.Name
			if _, err := session.RunCommand(ctx, ifaceCmd); err != nil {
				configErr = fmt.Errorf("failed executing %q: %w", ifaceCmd, err)
				break
			}
			appliedCommands = append(appliedCommands, ifaceCmd)

			if iface.Description != "" {
				descCmd := "description " + iface.Description
				mutationMayHaveStarted = true
				if _, err := session.RunCommand(ctx, descCmd); err != nil {
					configErr = fmt.Errorf("failed executing %q: %w", descCmd, err)
					break
				}
				appliedCommands = append(appliedCommands, descCmd)
			} else {
				descCmd := "no description"
				mutationMayHaveStarted = true
				if _, err := session.RunCommand(ctx, descCmd); err != nil {
					configErr = fmt.Errorf("failed executing %q: %w", descCmd, err)
					break
				}
				appliedCommands = append(appliedCommands, descCmd)
			}

			if iface.IPv4CIDR != "" {
				ipStr, maskStr := cidrToIPAndMask(iface.IPv4CIDR)
				ipCmd := fmt.Sprintf("ip address %s %s", ipStr, maskStr)
				mutationMayHaveStarted = true
				if _, err := session.RunCommand(ctx, ipCmd); err != nil {
					configErr = fmt.Errorf("failed executing %q: %w", ipCmd, err)
					break
				}
				appliedCommands = append(appliedCommands, ipCmd)
			}

			var adminCmd string
			if iface.AdminState == "up" {
				adminCmd = "no shutdown"
			} else {
				adminCmd = "shutdown"
			}
			mutationMayHaveStarted = true
			if _, err := session.RunCommand(ctx, adminCmd); err != nil {
				configErr = fmt.Errorf("failed executing %q: %w", adminCmd, err)
				break
			}
			appliedCommands = append(appliedCommands, adminCmd)

			exitCmd := "exit"
			if _, err := session.RunCommand(ctx, exitCmd); err != nil {
				configErr = fmt.Errorf("failed executing %q: %w", exitCmd, err)
				break
			}
			appliedCommands = append(appliedCommands, exitCmd)
		}
	}

	// Exit configuration mode
	if _, err := session.RunCommand(ctx, "end"); err != nil && configErr == nil {
		configErr = fmt.Errorf("failed to exit configuration mode: %w", err)
	}

	if configErr != nil {
		safeMsg := fmt.Sprintf("configuration application failed after %d command(s): %s", len(appliedCommands), sanitizeErrorWithCreds(configErr, creds))
		if mutationMayHaveStarted {
			safeMsg += "; router state is uncertain and must be re-read before any retry"
		}
		return ports.ExecutionResult{Status: domain.ExecutionTaskFailed, Error: safeMsg}, errors.New(safeMsg)
	}

	if ctx.Err() != nil {
		if mutationMayHaveStarted {
			safeMsg := "configuration application outcome is uncertain; router state must be re-read before any retry"
			return ports.ExecutionResult{Status: domain.ExecutionTaskFailed, Error: safeMsg}, errors.New(safeMsg)
		}
		return ports.ExecutionResult{Status: domain.ExecutionTaskCancelled}, ctx.Err()
	}

	// 7. Read-back verification phase
	observed, verifyErr := e.verifyRunningConfig(ctx, session, params)
	if verifyErr != nil {
		safeMsg := fmt.Sprintf("verification failed: %s; router state must be re-read before any retry", sanitizeErrorWithCreds(verifyErr, creds))
		return ports.ExecutionResult{Status: domain.ExecutionTaskFailed, Error: safeMsg}, errors.New(safeMsg)
	}

	// 8. Assemble structured result
	family := target.Family
	if family == "" {
		family = "ios"
	}
	resultPayload, err := json.Marshal(CiscoIOSConfigureResult{
		Operation:  CiscoIOSConfigureType,
		Site:       params.Site,
		DeviceID:   params.DeviceID,
		Vendor:     "cisco",
		Family:     family,
		ExecutedAt: time.Now().UTC().Format(time.RFC3339Nano),
		Status:     string(domain.ExecutionTaskSucceeded),
		RequestedChanges: RequestedChanges{
			Hostname:   params.Hostname,
			Interfaces: params.Interfaces,
		},
		ObservedChanges: observed,
		Verified:        true,
		Warnings: []string{
			"Configuration was applied to running-config and verified; startup-config was not persisted (write memory disabled by policy)",
		},
	})
	if err != nil {
		errMsg := "failed to marshal execution result"
		return ports.ExecutionResult{Status: domain.ExecutionTaskFailed, Error: errMsg}, errors.New(errMsg)
	}

	return ports.ExecutionResult{
		Status: domain.ExecutionTaskSucceeded,
		Output: resultPayload,
	}, nil
}

// verifyRunningConfig reads the active configuration from the device and compares it to requested changes.
func (e *CiscoIOSConfigureExecutor) verifyRunningConfig(ctx context.Context, session CiscoIOSSession, params CiscoIOSConfigureParameters) (ObservedChanges, error) {
	observed := ObservedChanges{
		Interfaces: make([]ObservedInterfaceConfig, 0, len(params.Interfaces)),
	}

	// Verify hostname if requested
	if params.Hostname != "" {
		output, err := session.RunCommand(ctx, "show running-config | include ^hostname")
		if err != nil {
			return observed, fmt.Errorf("read hostname: %w", err)
		}
		actualHostname := parseHostnameFromOutput(output)
		observed.Hostname = actualHostname
		if !strings.EqualFold(actualHostname, params.Hostname) {
			return observed, fmt.Errorf("hostname mismatch: requested %q, observed %q", params.Hostname, actualHostname)
		}
	}

	// Verify each interface
	for _, reqIface := range params.Interfaces {
		output, err := session.RunCommand(ctx, "show running-config interface "+reqIface.Name)
		if err != nil {
			return observed, fmt.Errorf("read interface %s running-config: %w", reqIface.Name, err)
		}

		obsIface := parseInterfaceFromRunningConfig(reqIface.Name, output)
		observed.Interfaces = append(observed.Interfaces, obsIface)

		// Compare Description
		if reqIface.Description != "" {
			if obsIface.Description != reqIface.Description {
				return observed, fmt.Errorf("interface %s description mismatch: requested %q, observed %q", reqIface.Name, reqIface.Description, obsIface.Description)
			}
		} else {
			if obsIface.Description != "" {
				return observed, fmt.Errorf("interface %s expected no description, observed %q", reqIface.Name, obsIface.Description)
			}
		}

		// Compare IPv4 Address and Subnet Mask
		if reqIface.IPv4CIDR != "" {
			reqIP, reqMask := cidrToIPAndMask(reqIface.IPv4CIDR)
			if obsIface.IPv4Address != reqIP || obsIface.SubnetMask != reqMask {
				return observed, fmt.Errorf("interface %s ipv4 mismatch: requested %s %s, observed %s %s", reqIface.Name, reqIP, reqMask, obsIface.IPv4Address, obsIface.SubnetMask)
			}
		}

		// Compare AdminState
		if obsIface.AdminState != reqIface.AdminState {
			return observed, fmt.Errorf("interface %s admin_state mismatch: requested %s, observed %s", reqIface.Name, reqIface.AdminState, obsIface.AdminState)
		}
	}

	return observed, nil
}

// Retryable always returns false because a failed command may have partially changed the router.
func (e *CiscoIOSConfigureExecutor) Retryable(error) bool {
	return false
}

func validateAndCanonicalizeInterfaceName(raw string) (string, error) {
	clean := strings.TrimSpace(raw)
	if clean == "" {
		return "", errors.New("interface name is required")
	}
	matches := ciscoInterfaceRegex.FindStringSubmatch(clean)
	if len(matches) < 3 {
		return "", fmt.Errorf("invalid Cisco Ethernet interface name %q", clean)
	}

	prefix := strings.ToLower(matches[1])
	numbers := matches[2]

	var canonicalPrefix string
	switch prefix {
	case "ethernet", "eth":
		canonicalPrefix = "Ethernet"
	case "fastethernet", "fa":
		canonicalPrefix = "FastEthernet"
	case "gigabitethernet", "gi":
		canonicalPrefix = "GigabitEthernet"
	case "tengigabitethernet", "te":
		canonicalPrefix = "TenGigabitEthernet"
	case "twentyfivegige", "twe":
		canonicalPrefix = "TwentyFiveGigE"
	case "fortygigabitethernet", "fo":
		canonicalPrefix = "FortyGigabitEthernet"
	case "hundredgige", "hu":
		canonicalPrefix = "HundredGigE"
	default:
		return "", fmt.Errorf("unsupported interface prefix %q", matches[1])
	}

	return canonicalPrefix + numbers, nil
}

func cidrToIPAndMask(cidr string) (string, string) {
	ip, ipNet, err := net.ParseCIDR(cidr)
	if err != nil {
		return "", ""
	}
	ipv4 := ip.To4()
	if ipv4 == nil {
		return "", ""
	}
	mask := net.IP(ipNet.Mask)
	return ipv4.String(), mask.String()
}

func parseHostnameFromOutput(output string) string {
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "hostname ") {
			fields := strings.Fields(trimmed)
			if len(fields) >= 2 {
				return fields[1]
			}
		}
	}
	return ""
}

func parseInterfaceFromRunningConfig(ifaceName, output string) ObservedInterfaceConfig {
	obs := ObservedInterfaceConfig{
		Name:       ifaceName,
		AdminState: "up", // In Cisco IOS, absence of "shutdown" means administratively up
	}

	lines := strings.Split(output, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "description "):
			obs.Description = strings.TrimPrefix(trimmed, "description ")
		case strings.HasPrefix(trimmed, "ip address "):
			fields := strings.Fields(trimmed)
			if len(fields) >= 4 {
				obs.IPv4Address = fields[2]
				obs.SubnetMask = fields[3]
			}
		case trimmed == "shutdown":
			obs.AdminState = "down"
		}
	}
	return obs
}

func sanitizeErrorWithCreds(err error, creds credentials.DeviceCredentials) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	if creds.Password != "" {
		msg = strings.ReplaceAll(msg, creds.Password, "[REDACTED]")
	}
	if creds.Passphrase != "" {
		msg = strings.ReplaceAll(msg, creds.Passphrase, "[REDACTED]")
	}
	return observability.Redact(msg)
}
