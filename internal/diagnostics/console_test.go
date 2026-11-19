package diagnostics

import (
	"strings"
	"testing"
)

// Excerpt shaped like the real GNS3 Dynamips 7200 AutoInstall capture.
const autoInstallCapture = "Would you like to enter the initial configuration dialog? [yes/no]: \r\n" +
	"Loading network-confg from 192.0.2.2 (via FastEthernet0/0): !\r\n[OK - 25 bytes]\r\n" +
	"Configuration mapped ip address 192.0.2.10 to edge1\r\n" +
	"Loading edge1-confg from 192.0.2.2 (via FastEthernet0/0): !\r\n[OK - 93 bytes]\r\n" +
	"edge1>show ip interface brief | include FastEthernet0/0\r\n" +
	"FastEthernet0/0            192.0.2.10      YES TFTP   up                    up      \r\n" +
	"edge1#show version | include IOS Software\r\n" +
	"Cisco IOS Software, 7200 Software (C7200-ADVENTERPRISEK9-M), Version 12.4(24)T5, RELEASE SOFTWARE (fc3)\r\n" +
	"edge1#show running-config | include ^hostname\r\nhostname edge1\r\n" +
	"edge1#show running-config interface FastEthernet0/0\r\ninterface FastEthernet0/0\r\n ip address 192.0.2.10 255.255.255.0\r\n"

func TestVerifyConsoleConfirmsAppliedAutoInstallConfiguration(t *testing.T) {
	result := VerifyConsole(autoInstallCapture, ConsoleExpectation{DeviceID: "edge1", ExpectedHostname: "edge1", ExpectedIPv4: "192.0.2.10"})
	if !result.ConfigLoaded || result.ConfigRejected || !result.PostCheckPassed || result.OSVersion != "12.4(24)T5" || result.HostnameObserved != "edge1" {
		t.Fatalf("verification = %+v", result)
	}
	if len(result.Evidence) < 4 {
		t.Fatalf("verification lacks console evidence: %v", result.Evidence)
	}
}

func TestVerifyConsoleRejectsPromptOnlyOrMismatchedState(t *testing.T) {
	promptOnly := "Loading edge1-confg from 192.0.2.2 (via FastEthernet0/0): !\n[OK - 93 bytes]\nedge1>\n"
	if result := VerifyConsole(promptOnly, ConsoleExpectation{ExpectedHostname: "edge1", ExpectedIPv4: "192.0.2.10"}); result.PostCheckPassed {
		t.Fatalf("a prompt alone was accepted as proof: %+v", result)
	}
	wrong := strings.Replace(autoInstallCapture, "hostname edge1", "hostname Router", 1)
	if result := VerifyConsole(wrong, ConsoleExpectation{ExpectedHostname: "edge1", ExpectedIPv4: "192.0.2.10"}); result.PostCheckPassed || !strings.Contains(strings.Join(result.Problems, ";"), "does not match") {
		t.Fatalf("wrong hostname accepted: %+v", result)
	}
	noDownload := "edge1#show running-config | include ^hostname\nhostname edge1\n"
	if result := VerifyConsole(noDownload, ConsoleExpectation{ExpectedHostname: "edge1"}); result.PostCheckPassed || result.ConfigLoaded {
		t.Fatalf("state without a download confirmation was accepted: %+v", result)
	}
}

func TestVerifyConsoleDetectsRejectedConfigurationButIgnoresOperatorTypos(t *testing.T) {
	rejected := "Loading edge1-confg from 192.0.2.2 (via FastEthernet0/0): !\n[OK - 93 bytes]\n% Invalid input detected at '^' marker.\nRouter>\n"
	if result := VerifyConsole(rejected, ConsoleExpectation{ExpectedHostname: "edge1"}); !result.ConfigRejected || result.PostCheckPassed {
		t.Fatalf("rejected configuration not detected: %+v", result)
	}
	operatorTypo := autoInstallCapture + "edge1>show running-config\n % Invalid input detected at '^' marker.\n"
	if result := VerifyConsole(operatorTypo, ConsoleExpectation{ExpectedHostname: "edge1", ExpectedIPv4: "192.0.2.10"}); result.ConfigRejected || !result.PostCheckPassed {
		t.Fatalf("an operator command error was treated as a configuration rejection: %+v", result)
	}
}
