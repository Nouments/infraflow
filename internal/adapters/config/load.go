package config

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"sort"
	"strings"

	"infraflow/internal/domain"
	"infraflow/pkg/protocol"

	"gopkg.in/yaml.v3"
)

const MaxInputBytes = 1 << 20

type Parser struct{}

func (Parser) Parse(data []byte) (domain.Infrastructure, error) {
	return Parse(data)
}

type document struct {
	CapabilityRegistry domain.CapabilityRegistry `yaml:"capability_registry"`
	TemplateRegistry   domain.TemplateRegistry   `yaml:"template_registry"`
	Sites              []domain.Site             `yaml:"sites"`
	Site               *domain.Site              `yaml:"site"`
}

func Parse(data []byte) (domain.Infrastructure, error) {
	if len(data) > MaxInputBytes {
		return domain.Infrastructure{}, fmt.Errorf("configuration exceeds the %d-byte limit", MaxInputBytes)
	}

	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	var input document
	if err := decoder.Decode(&input); err != nil {
		return domain.Infrastructure{}, fmt.Errorf("decode configuration: %w", err)
	}

	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return domain.Infrastructure{}, fmt.Errorf("configuration must contain exactly one YAML document")
		}
		return domain.Infrastructure{}, fmt.Errorf("decode trailing YAML: %w", err)
	}
	if input.Site != nil && len(input.Sites) > 0 {
		return domain.Infrastructure{}, fmt.Errorf("use either 'site' or 'sites', not both")
	}

	infrastructure := domain.Infrastructure{
		CapabilityRegistry: input.CapabilityRegistry,
		TemplateRegistry:   input.TemplateRegistry,
		Sites:              input.Sites,
	}
	if err := validateCapabilityRegistry(infrastructure.CapabilityRegistry); err != nil {
		return domain.Infrastructure{}, err
	}
	if err := validateTemplateRegistry(infrastructure.TemplateRegistry); err != nil {
		return domain.Infrastructure{}, err
	}
	if input.Site != nil {
		infrastructure.Sites = []domain.Site{*input.Site}
	}
	for siteIndex := range infrastructure.Sites {
		for linkIndex := range infrastructure.Sites[siteIndex].Links {
			link := &infrastructure.Sites[siteIndex].Links[linkIndex]
			if len(link.Endpoints) == 2 && link.A == "" && link.B == "" {
				link.A = link.Endpoints[0].Device + ":" + link.Endpoints[0].Interface
				link.B = link.Endpoints[1].Device + ":" + link.Endpoints[1].Interface
				link.Endpoints = nil
			}
		}
	}
	if validationErrors := Validate(infrastructure); len(validationErrors) > 0 {
		return domain.Infrastructure{}, validationErrors
	}
	return infrastructure, nil
}

func Read(reader io.Reader) (domain.Infrastructure, error) {
	data, err := io.ReadAll(io.LimitReader(reader, MaxInputBytes+1))
	if err != nil {
		return domain.Infrastructure{}, fmt.Errorf("read configuration: %w", err)
	}
	return Parse(data)
}

func validateCapabilityRegistry(registry domain.CapabilityRegistry) error {
	for key, methods := range registry.Entries {
		for method, capability := range methods {
			if capability.State == "" {
				return fmt.Errorf("unsupported capability state for %s/%s: empty state", key, method)
			}
			switch capability.State {
			case domain.CapabilityImplemented, domain.CapabilityLabVerified, domain.CapabilityExperimental, domain.CapabilityUnverified, domain.CapabilityUnsupported, domain.CapabilityUnknown:
				if capability.State.IsUsable() && strings.TrimSpace(capability.Evidence) == "" {
					return fmt.Errorf("unsupported capability state for %s/%s: %s requires evidence", key, method, capability.State)
				}
			default:
				return fmt.Errorf("unsupported capability state for %s/%s: %s", key, method, capability.State)
			}
		}
	}
	return nil
}

func validateTemplateRegistry(registry domain.TemplateRegistry) error {
	for id, template := range registry.Entries {
		if template.ID == "" {
			return fmt.Errorf("template registry key %q does not contain an id", id)
		}
		if template.ID != id {
			return fmt.Errorf("template registry key %q does not match template id %q", id, template.ID)
		}
		if strings.TrimSpace(template.Version) == "" {
			return fmt.Errorf("template %q requires a version", template.ID)
		}
		if strings.TrimSpace(template.Hash) == "" {
			return fmt.Errorf("template %q requires a hash", template.ID)
		}
		if strings.TrimSpace(template.Evidence) == "" {
			return fmt.Errorf("template %q requires provenance evidence", template.ID)
		}
	}
	return nil
}

type ValidationErrors []string

func (validationErrors ValidationErrors) Error() string {
	return strings.Join(validationErrors, "\n")
}

func Validate(infrastructure domain.Infrastructure) ValidationErrors {
	var problems ValidationErrors
	if len(infrastructure.Sites) == 0 {
		return ValidationErrors{"at least one site is required"}
	}

	seenSites := make(map[string]struct{})
	seenSiteIDs := make(map[string]struct{})
	seenMACs := make(map[string]string)
	for _, site := range infrastructure.Sites {
		prefix := "site " + display(site.Name)
		if !protocol.ValidSiteName(site.Name) {
			problems = append(problems, prefix+": name must contain only letters, digits, '.', '_' or '-' and start with a letter or digit")
		}
		if _, exists := seenSites[site.Name]; exists {
			problems = append(problems, prefix+": duplicate site name")
		}
		seenSites[site.Name] = struct{}{}
		if site.ID != "" {
			if _, exists := seenSiteIDs[site.ID]; exists {
				problems = append(problems, prefix+": duplicate site id "+site.ID)
			}
			seenSiteIDs[site.ID] = struct{}{}
		}

		var networks []namedNetwork
		if site.Bootstrap.Network != "" {
			_, bootstrapNetwork, err := net.ParseCIDR(site.Bootstrap.Network)
			if err != nil {
				problems = append(problems, prefix+": invalid bootstrap CIDR "+site.Bootstrap.Network)
			} else {
				networks = append(networks, namedNetwork{name: "bootstrap", network: bootstrapNetwork})
				if site.Bootstrap.Gateway != "" {
					gateway := net.ParseIP(site.Bootstrap.Gateway)
					if gateway == nil || !bootstrapNetwork.Contains(gateway) {
						problems = append(problems, prefix+": bootstrap gateway must be a valid address inside the bootstrap network")
					}
				}
			}
		} else if site.Bootstrap.Gateway != "" {
			problems = append(problems, prefix+": bootstrap gateway requires a bootstrap network")
		}

		seenDevices := make(map[string]struct{})
		seenDeviceIDs := make(map[string]struct{})
		for _, device := range site.Devices {
			devicePrefix := prefix + ", device " + display(device.Name)
			if strings.TrimSpace(device.Name) == "" {
				problems = append(problems, devicePrefix+": name is required")
			}
			if _, exists := seenDevices[device.Name]; exists {
				problems = append(problems, devicePrefix+": duplicate device name")
			}
			seenDevices[device.Name] = struct{}{}
			if device.ID != "" {
				if _, exists := seenDeviceIDs[device.ID]; exists {
					problems = append(problems, devicePrefix+": duplicate device id "+device.ID)
				}
				seenDeviceIDs[device.ID] = struct{}{}
			}
			if device.Management.IPv4 != "" {
				ip := net.ParseIP(device.Management.IPv4)
				if ip == nil || ip.To4() == nil {
					problems = append(problems, devicePrefix+": management.ipv4 must be an IPv4 address")
				}
			}
			for _, rawMAC := range device.Identity.MACs {
				mac, err := net.ParseMAC(rawMAC)
				if err != nil {
					problems = append(problems, devicePrefix+": invalid MAC address "+rawMAC)
					continue
				}
				normalizedMAC := strings.ToLower(mac.String())
				if previous, exists := seenMACs[normalizedMAC]; exists {
					problems = append(problems, devicePrefix+": MAC address duplicates "+previous)
				}
				seenMACs[normalizedMAC] = device.Name
			}
		}

		seenLinkIDs := make(map[string]struct{})
		for _, link := range site.Links {
			linkPrefix := prefix + ", link " + display(link.ID)
			if link.ID != "" {
				if _, exists := seenLinkIDs[link.ID]; exists {
					problems = append(problems, linkPrefix+": duplicate link id")
				}
				seenLinkIDs[link.ID] = struct{}{}
			}
			if len(link.Endpoints) != 0 && (link.A != "" || link.B != "") {
				problems = append(problems, linkPrefix+": use either 'a'/'b' or 'endpoints', not both")
			}
			if len(link.Endpoints) != 0 && len(link.Endpoints) != 2 {
				problems = append(problems, linkPrefix+": exactly two endpoints are required")
			}
			if link.A == "" || link.B == "" {
				problems = append(problems, linkPrefix+": both endpoint fields 'a' and 'b' are required")
			} else {
				for _, endpoint := range []string{link.A, link.B} {
					deviceName, iface, found := strings.Cut(endpoint, ":")
					if !found || deviceName == "" || iface == "" {
						problems = append(problems, linkPrefix+": endpoint "+endpoint+" must use device:interface format")
						continue
					}
					if _, exists := seenDevices[deviceName]; !exists {
						problems = append(problems, linkPrefix+": endpoint references unknown device "+deviceName)
					}
				}
			}
			_, linkNetwork, err := net.ParseCIDR(link.Network)
			if err != nil {
				problems = append(problems, linkPrefix+": invalid or missing network CIDR "+link.Network)
			} else {
				networks = append(networks, namedNetwork{name: "link " + display(link.ID), network: linkNetwork})
			}
		}
		for index, current := range networks {
			for _, previous := range networks[:index] {
				if networksOverlap(current.network, previous.network) {
					problems = append(problems, prefix+": network "+current.name+" overlaps "+previous.name)
				}
			}
		}
	}

	sort.Strings(problems)
	return problems
}

type namedNetwork struct {
	name    string
	network *net.IPNet
}

func networksOverlap(first, second *net.IPNet) bool {
	return first.Contains(second.IP) || second.Contains(first.IP)
}

func display(value string) string {
	if strings.TrimSpace(value) == "" {
		return "<unnamed>"
	}
	return value
}
