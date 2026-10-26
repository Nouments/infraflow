package config

import (
	"strings"
	"testing"
)

const validNetworkDeployment = `apiVersion: infraflow/v1
kind: NetworkDeployment
metadata:
  name: demo
spec:
  site: lab
  devices:
    - name: R1
      vendor: cisco
      model: isr-1100
      os: ios-xe
      interfaces:
        - name: GigabitEthernet0/0/0
          ipv4: 192.0.2.1/24
          enabled: true
`

func TestParseNetworkDeploymentAdaptsVersionedSchema(t *testing.T) {
	infrastructure, deployment, err := ParseNetworkDeployment([]byte(validNetworkDeployment))
	if err != nil {
		t.Fatal(err)
	}
	if deployment.Metadata.Name != "demo" || infrastructure.Sites[0].Devices[0].Family != "ios-xe" {
		t.Fatalf("unexpected adapted deployment: %#v / %#v", deployment, infrastructure)
	}
	if got := infrastructure.Sites[0].Devices[0].Network.Interfaces[0].IPv4Address; got != "192.0.2.1/24" {
		t.Fatalf("expected normalized interface prefix, got %q", got)
	}
}

func TestParseNetworkDeploymentRejectsInvalidAndUnsupportedInput(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"malformed YAML", "apiVersion: [", "decode NetworkDeployment"},
		{"unknown interface field", strings.Replace(validNetworkDeployment, "enabled: true", "enabled: true\n          vlan: 10", 1), "field vlan not found"},
		{"missing interface state", strings.Replace(validNetworkDeployment, "          enabled: true\n", "", 1), "spec.devices[0].interfaces[0].enabled"},
		{"invalid IPv4", strings.Replace(validNetworkDeployment, "192.0.2.1/24", "not-an-ip", 1), "spec.devices[0].interfaces[0].ipv4"},
		{"OSPF without template", validNetworkDeployment + "      routing:\n        ospf:\n          process_id: 1\n", "spec.devices[0].routing.ospf"},
		{"unsupported version", strings.Replace(validNetworkDeployment, "infraflow/v1", "infraflow/v2", 1), "apiVersion"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, _, err := ParseNetworkDeployment([]byte(test.input))
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("expected error containing %q, got %v", test.want, err)
			}
		})
	}
}

func TestParseRecognizesNetworkDeploymentInExistingValidatePath(t *testing.T) {
	if _, err := Parse([]byte(validNetworkDeployment)); err != nil {
		t.Fatalf("existing parser did not accept versioned deployment: %v", err)
	}
}
