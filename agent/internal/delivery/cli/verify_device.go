package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"infraflow/internal/diagnostics"
	"infraflow/pkg/observability"
)

const maxConsoleCaptureBytes = 4 << 20

// runVerifyDevice checks a captured device console against the expected
// post-configuration state and records the result as device lifecycle events.
// It is read-only: it never connects to or sends commands to the device.
func runVerifyDevice(arguments []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("verify-device", flag.ContinueOnError)
	flags.SetOutput(stderr)
	consolePath := flags.String("console", "", "captured device console output (read-only input)")
	deviceID := flags.String("device-id", "", "device identifier used for correlation")
	expectedHostname := flags.String("expected-hostname", "", "hostname expected in the running configuration")
	expectedIPv4 := flags.String("expected-ipv4", "", "management IPv4 address expected on the device")
	agentConfig := flags.String("agent-config", "", "optional agent configuration used to persist and synchronize the result")
	jsonOutput := flags.Bool("json", false, "write a machine-readable result")
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	if flags.NArg() != 0 || *consolePath == "" || *deviceID == "" || (*expectedHostname == "" && *expectedIPv4 == "") {
		fmt.Fprintln(stderr, "infraflow-agent: verify-device requires -console, -device-id, and -expected-hostname or -expected-ipv4")
		return 2
	}
	file, err := os.Open(*consolePath)
	if err != nil {
		fmt.Fprintf(stderr, "infraflow-agent: open console capture: %v\n", err)
		return 2
	}
	data, err := io.ReadAll(io.LimitReader(file, maxConsoleCaptureBytes+1))
	_ = file.Close()
	if err != nil || len(data) > maxConsoleCaptureBytes {
		fmt.Fprintf(stderr, "infraflow-agent: console capture must be readable and at most %d bytes\n", maxConsoleCaptureBytes)
		return 2
	}
	result := diagnostics.VerifyConsole(string(data), diagnostics.ConsoleExpectation{DeviceID: *deviceID, ExpectedHostname: *expectedHostname, ExpectedIPv4: *expectedIPv4})
	sink, closeSink, err := bootstrapEventSink(context.Background(), *agentConfig, io.Discard, stderr)
	if err != nil {
		fmt.Fprintf(stderr, "infraflow-agent: %v\n", err)
		return 2
	}
	evidence := strings.Join(result.Evidence, " | ")
	base := observability.Event{Service: "agent", Source: "device-console", DeviceID: *deviceID, Operation: "device-verification", Line: evidence}
	if result.ConfigLoaded {
		event := base
		event.Level, event.Event, event.Status = "INFO", "device.config.applied", "COMPLETED"
		event.Message = "device console confirms the bootstrap configuration download"
		if result.ConfigRejected {
			event.Level, event.Event, event.Status = "ERROR", "device.config.rejected", "FAILED"
			event.Message = "device console reports the downloaded configuration was rejected"
		}
		sink(event)
	}
	event := base
	if result.PostCheckPassed {
		event.Level, event.Event, event.Status = "INFO", "device.postcheck.passed", "COMPLETED"
		event.Message = fmt.Sprintf("device %s running state matches the expected hostname/addressing (IOS %s)", *deviceID, valueOrUnknown(result.OSVersion))
	} else {
		event.Level, event.Event, event.Status = "ERROR", "device.postcheck.failed", "FAILED"
		event.Message = "device running state does not match the expected configuration"
		event.Error = strings.Join(result.Problems, "; ")
	}
	sink(event)
	closeSink()
	if *jsonOutput {
		encoder := json.NewEncoder(stdout)
		encoder.SetIndent("", "  ")
		_ = encoder.Encode(result)
	} else {
		fmt.Fprintf(stdout, "Configuration loaded: %v\nConfiguration rejected: %v\nOS version: %s\nHostname observed: %s\nExpected IPv4 observed: %v\nPost-check passed: %v\n",
			result.ConfigLoaded, result.ConfigRejected, valueOrUnknown(result.OSVersion), valueOrUnknown(result.HostnameObserved), result.IPv4Observed, result.PostCheckPassed)
		for _, problem := range result.Problems {
			fmt.Fprintf(stdout, "Problem: %s\n", problem)
		}
		for _, line := range result.Evidence {
			fmt.Fprintf(stdout, "Evidence: %s\n", line)
		}
	}
	if !result.PostCheckPassed {
		return 1
	}
	return 0
}
