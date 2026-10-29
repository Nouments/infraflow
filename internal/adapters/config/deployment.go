package config

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"regexp"
	"strings"

	"infraflow/internal/domain"

	"gopkg.in/yaml.v3"
)

const NetworkDeploymentAPIVersion = "infraflow/v1"

var deploymentInterfaceName = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_./:-]*$`)

type NetworkDeploymentMetadata struct {
	Name string `yaml:"name"`
}

type NetworkDeployment struct {
	APIVersion string                    `yaml:"apiVersion"`
	Kind       string                    `yaml:"kind"`
	Metadata   NetworkDeploymentMetadata `yaml:"metadata"`
	Spec       NetworkDeploymentSpec     `yaml:"spec"`
}

type NetworkDeploymentSpec struct {
	Site    string                    `yaml:"site"`
	Devices []NetworkDeploymentDevice `yaml:"devices"`
}

type NetworkDeploymentDevice struct {
	Name       string                       `yaml:"name"`
	Vendor     string                       `yaml:"vendor"`
	Model      string                       `yaml:"model"`
	OS         string                       `yaml:"os"`
	OSVersion  string                       `yaml:"os_version,omitempty"`
	Interfaces []NetworkDeploymentInterface `yaml:"interfaces"`
	Routing    *NetworkDeploymentRouting    `yaml:"routing,omitempty"`
}

type NetworkDeploymentInterface struct {
	Name    string `yaml:"name"`
	IPv4    string `yaml:"ipv4"`
	Enabled *bool  `yaml:"enabled"`
}

type NetworkDeploymentRouting struct {
	OSPF *NetworkDeploymentOSPF `yaml:"ospf,omitempty"`
}

type NetworkDeploymentOSPF struct {
	ProcessID int                            `yaml:"process_id"`
	RouterID  string                         `yaml:"router_id"`
	Networks  []NetworkDeploymentOSPFNetwork `yaml:"networks"`
}

type NetworkDeploymentOSPFNetwork struct {
	Prefix string `yaml:"prefix"`
	Area   int    `yaml:"area"`
}

// ParseNetworkDeployment strictly decodes the versioned user-facing schema,
// validates fields needed by the current renderer, and adapts it to the
// existing infrastructure model.
func ParseNetworkDeployment(data []byte) (domain.Infrastructure, NetworkDeployment, error) {
	if len(data) > MaxInputBytes {
		return domain.Infrastructure{}, NetworkDeployment{}, fmt.Errorf("configuration exceeds the %d-byte limit", MaxInputBytes)
	}
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	var deployment NetworkDeployment
	if err := decoder.Decode(&deployment); err != nil {
		return domain.Infrastructure{}, NetworkDeployment{}, fmt.Errorf("decode NetworkDeployment: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return domain.Infrastructure{}, NetworkDeployment{}, fmt.Errorf("NetworkDeployment must contain exactly one YAML document")
		}
		return domain.Infrastructure{}, NetworkDeployment{}, fmt.Errorf("decode trailing NetworkDeployment YAML: %w", err)
	}
	if deployment.APIVersion != NetworkDeploymentAPIVersion {
		return domain.Infrastructure{}, NetworkDeployment{}, fmt.Errorf("apiVersion: unsupported value %q; expected %q", deployment.APIVersion, NetworkDeploymentAPIVersion)
	}
	if deployment.Kind != "NetworkDeployment" {
		return domain.Infrastructure{}, NetworkDeployment{}, fmt.Errorf("kind: expected NetworkDeployment")
	}
	if !validTopologyIdentifier(deployment.Metadata.Name) {
		return domain.Infrastructure{}, NetworkDeployment{}, fmt.Errorf("metadata.name: required; use letters, digits, '.', '_' or '-'")
	}
	if !validTopologyIdentifier(deployment.Spec.Site) {
		return domain.Infrastructure{}, NetworkDeployment{}, fmt.Errorf("spec.site: required; use letters, digits, '.', '_' or '-'")
	}
	if len(deployment.Spec.Devices) == 0 {
		return domain.Infrastructure{}, NetworkDeployment{}, fmt.Errorf("spec.devices: at least one device is required")
	}

	seenDevices := make(map[string]struct{}, len(deployment.Spec.Devices))
	seenAddresses := make(map[string]string)
	site := domain.Site{Name: deployment.Spec.Site}
	for deviceIndex, declared := range deployment.Spec.Devices {
		devicePath := fmt.Sprintf("spec.devices[%d]", deviceIndex)
		if !validTopologyIdentifier(declared.Name) {
			return domain.Infrastructure{}, NetworkDeployment{}, fmt.Errorf("%s.name: required; use letters, digits, '.', '_' or '-'", devicePath)
		}
		if _, exists := seenDevices[declared.Name]; exists {
			return domain.Infrastructure{}, NetworkDeployment{}, fmt.Errorf("%s.name: duplicate device %q", devicePath, declared.Name)
		}
		seenDevices[declared.Name] = struct{}{}
		if strings.TrimSpace(declared.Vendor) == "" {
			return domain.Infrastructure{}, NetworkDeployment{}, fmt.Errorf("%s.vendor: required", devicePath)
		}
		if strings.TrimSpace(declared.Model) == "" {
			return domain.Infrastructure{}, NetworkDeployment{}, fmt.Errorf("%s.model: required", devicePath)
		}
		if strings.TrimSpace(declared.OS) == "" {
			return domain.Infrastructure{}, NetworkDeployment{}, fmt.Errorf("%s.os: required", devicePath)
		}
		if len(declared.Interfaces) == 0 {
			return domain.Infrastructure{}, NetworkDeployment{}, fmt.Errorf("%s.interfaces: at least one interface is required", devicePath)
		}
		if declared.Routing != nil && declared.Routing.OSPF != nil {
			return domain.Infrastructure{}, NetworkDeployment{}, fmt.Errorf("%s.routing.ospf: unsupported; no matching OSPF template is registered", devicePath)
		}

		device := domain.Device{
			Name:   declared.Name,
			Vendor: strings.TrimSpace(declared.Vendor),
			Model:  strings.TrimSpace(declared.Model),
			Family: strings.TrimSpace(declared.OS),
		}
		network := &domain.DeviceNetwork{}
		seenInterfaces := make(map[string]struct{}, len(declared.Interfaces))
		for interfaceIndex, declaredInterface := range declared.Interfaces {
			interfacePath := fmt.Sprintf("%s.interfaces[%d]", devicePath, interfaceIndex)
			if !validInterfaceIdentifier(declaredInterface.Name) {
				return domain.Infrastructure{}, NetworkDeployment{}, fmt.Errorf("%s.name: invalid interface identifier", interfacePath)
			}
			if _, exists := seenInterfaces[declaredInterface.Name]; exists {
				return domain.Infrastructure{}, NetworkDeployment{}, fmt.Errorf("%s.name: duplicate interface %q", interfacePath, declaredInterface.Name)
			}
			seenInterfaces[declaredInterface.Name] = struct{}{}
			ip, networkMask, err := net.ParseCIDR(declaredInterface.IPv4)
			if err != nil || ip.To4() == nil {
				return domain.Infrastructure{}, NetworkDeployment{}, fmt.Errorf("%s.ipv4: expected an IPv4 CIDR prefix", interfacePath)
			}
			ones, bits := networkMask.Mask.Size()
			if bits != 32 {
				return domain.Infrastructure{}, NetworkDeployment{}, fmt.Errorf("%s.ipv4: expected an IPv4 CIDR prefix", interfacePath)
			}
			if ones <= 30 {
				broadcast := append(net.IP(nil), networkMask.IP.To4()...)
				for maskIndex, maskByte := range networkMask.Mask {
					broadcast[maskIndex] |= ^maskByte
				}
				if ip.To4().Equal(networkMask.IP.To4()) || ip.To4().Equal(broadcast) {
					return domain.Infrastructure{}, NetworkDeployment{}, fmt.Errorf("%s.ipv4: network and broadcast addresses cannot be assigned to an interface", interfacePath)
				}
			}
			if declaredInterface.Enabled == nil {
				return domain.Infrastructure{}, NetworkDeployment{}, fmt.Errorf("%s.enabled: required boolean", interfacePath)
			}
			address := ip.To4().String()
			if previous, exists := seenAddresses[address]; exists {
				return domain.Infrastructure{}, NetworkDeployment{}, fmt.Errorf("%s.ipv4: address duplicates %s", interfacePath, previous)
			}
			seenAddresses[address] = interfacePath + ".ipv4"
			network.Interfaces = append(network.Interfaces, domain.NetworkInterface{
				Name:        declaredInterface.Name,
				IPv4Mode:    "static",
				IPv4Address: address + "/" + fmt.Sprint(maskSize(networkMask)),
				Enabled:     declaredInterface.Enabled,
			})
		}
		device.Network = network
		site.Devices = append(site.Devices, device)
	}
	infrastructure := domain.Infrastructure{Sites: []domain.Site{site}}
	if problems := Validate(infrastructure); len(problems) > 0 {
		return domain.Infrastructure{}, NetworkDeployment{}, fmt.Errorf("validate NetworkDeployment: %s", strings.Join(problems, "; "))
	}
	return infrastructure, deployment, nil
}

func maskSize(network *net.IPNet) int {
	ones, _ := network.Mask.Size()
	return ones
}
