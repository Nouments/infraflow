package diagnostics

import (
	"regexp"
	"strings"
)

// ConsoleExpectation is the post-configuration state expected on a device.
type ConsoleExpectation struct {
	DeviceID         string
	ExpectedHostname string
	ExpectedIPv4     string
}

// ConsoleVerification is the result of reading a captured device console. It
// is derived only from text the device printed; nothing is sent to the device.
type ConsoleVerification struct {
	ConfigLoaded     bool     `json:"config_loaded"`
	ConfigRejected   bool     `json:"config_rejected"`
	HostnameObserved string   `json:"hostname_observed,omitempty"`
	IPv4Observed     bool     `json:"ipv4_observed"`
	OSVersion        string   `json:"os_version,omitempty"`
	PostCheckPassed  bool     `json:"post_check_passed"`
	Problems         []string `json:"problems,omitempty"`
	Evidence         []string `json:"evidence"`
}

var (
	consoleLoadedLine   = regexp.MustCompile(`(?m)^Loading (\S+) from (\S+).*\n?\s*\[OK - (\d+) bytes\]`)
	consoleRejectedLine = regexp.MustCompile(`(?im)^.*(% Invalid input detected|configuration rejected|error parsing configuration).*$`)
	consoleHostnameLine = regexp.MustCompile(`(?m)^\s*hostname\s+([A-Za-z][A-Za-z0-9_-]{0,62})\s*$`)
	consoleVersionLine  = regexp.MustCompile(`(?m)Cisco IOS Software, .*Version ([0-9][0-9A-Za-z().]*)`)
	consoleAddressLine  = regexp.MustCompile(`(?m)^\s*ip address ((?:[0-9]{1,3}\.){3}[0-9]{1,3}) ((?:[0-9]{1,3}\.){3}[0-9]{1,3})\s*$`)
	consoleBriefLine    = regexp.MustCompile(`(?m)^(\S+)\s+((?:[0-9]{1,3}\.){3}[0-9]{1,3})\s+YES\s+(\S+)\s+(up|down|administratively down)\s+(up|down)\s*$`)
)

// VerifyConsole checks a captured console against the expected state. Only
// configuration that the device itself printed counts as evidence: a hostname in
// the prompt alone is not accepted, the running-config line is required.
func VerifyConsole(capture string, expect ConsoleExpectation) ConsoleVerification {
	text := strings.ReplaceAll(capture, "\r", "")
	result := ConsoleVerification{Evidence: []string{}}
	add := func(line string) {
		line = truncate(strings.TrimSpace(line), 240)
		if line != "" && len(result.Evidence) < 40 {
			result.Evidence = append(result.Evidence, line)
		}
	}
	for _, match := range consoleLoadedLine.FindAllStringSubmatch(text, -1) {
		result.ConfigLoaded = true
		add("Loading " + match[1] + " from " + match[2] + " [OK - " + match[3] + " bytes]")
	}
	if match := consoleRejectedLine.FindString(text); match != "" && result.ConfigLoaded {
		// A rejection after the configuration download is a device-side
		// configuration failure. Operator typos on the console are excluded
		// by only considering text before the first prompt command echo.
		if beforeOperator(text, match) {
			result.ConfigRejected = true
			add(match)
		}
	}
	if match := consoleVersionLine.FindStringSubmatch(text); len(match) == 2 {
		result.OSVersion = match[1]
		add(match[0])
	}
	hostnames := consoleHostnameLine.FindAllStringSubmatch(text, -1)
	if len(hostnames) > 0 {
		last := hostnames[len(hostnames)-1]
		result.HostnameObserved = last[1]
		add(last[0])
	}
	for _, match := range consoleAddressLine.FindAllStringSubmatch(text, -1) {
		if match[1] == expect.ExpectedIPv4 {
			result.IPv4Observed = true
			add(match[0])
		}
	}
	for _, match := range consoleBriefLine.FindAllStringSubmatch(text, -1) {
		if match[2] == expect.ExpectedIPv4 {
			result.IPv4Observed = true
			add(match[0])
		}
	}
	if !result.ConfigLoaded {
		result.Problems = append(result.Problems, "no configuration download confirmation ([OK - n bytes]) in the console capture")
	}
	if result.ConfigRejected {
		result.Problems = append(result.Problems, "the device reported invalid configuration input after the download")
	}
	if expect.ExpectedHostname != "" && !strings.EqualFold(result.HostnameObserved, expect.ExpectedHostname) {
		if result.HostnameObserved == "" {
			result.Problems = append(result.Problems, "running-config hostname line not observed; capture 'show running-config | include ^hostname'")
		} else {
			result.Problems = append(result.Problems, "hostname "+result.HostnameObserved+" does not match expected "+expect.ExpectedHostname)
		}
	}
	if expect.ExpectedIPv4 != "" && !result.IPv4Observed {
		result.Problems = append(result.Problems, "expected address "+expect.ExpectedIPv4+" not observed in running-config or 'show ip interface brief'")
	}
	result.PostCheckPassed = len(result.Problems) == 0 && (expect.ExpectedHostname != "" || expect.ExpectedIPv4 != "")
	return result
}

// beforeOperator reports whether match occurs before the first privileged
// operator command echo (a line starting with "<host>#show" or ">show").
func beforeOperator(text, match string) bool {
	index := strings.Index(text, match)
	operator := regexp.MustCompile(`(?m)^\S+[>#](show|enable|terminal)\b`).FindStringIndex(text)
	return operator == nil || index < operator[0]
}
