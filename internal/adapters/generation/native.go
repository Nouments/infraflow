package generator

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"infraflow/internal/adapters/config"
	"infraflow/internal/domain"
	"infraflow/internal/infrastructure/safefs"
	"infraflow/pkg/protocol"
)

const nativeTemplateVersion = "1"

var (
	nativeIdentifier = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_.:/-]{0,62}$`)
	nativeHostname   = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]{0,34}$`)
	nativeVDOM       = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]{0,30}$`)
	routerOS7Version = regexp.MustCompile(`^7\.(?:x|[0-9]+(?:\.[0-9]+)*)$`)
)

type nativeManifest struct {
	TemplateVersion string               `json:"template_version"`
	InputHash       string               `json:"input_hash"`
	Status          string               `json:"status"`
	ExecutionMode   string               `json:"execution_mode"`
	Warnings        []string             `json:"warnings"`
	Devices         []nativeDeviceReport `json:"devices"`
}

type nativeDeviceReport struct {
	ID                  string   `json:"id,omitempty"`
	Name                string   `json:"name"`
	Vendor              string   `json:"vendor"`
	Family              string   `json:"family"`
	Model               string   `json:"model,omitempty"`
	OSVersion           string   `json:"os_version,omitempty"`
	ProvisioningMethod  string   `json:"provisioning_method,omitempty"`
	TemplateID          string   `json:"template_id"`
	TemplateVersion     string   `json:"template_version"`
	InputHash           string   `json:"input_hash"`
	OutputHash          string   `json:"output_hash"`
	Compatibility       string   `json:"compatibility,omitempty"`
	CompatibilityStatus string   `json:"compatibility_status"`
	VerificationStatus  string   `json:"verification_status"`
	SourceReferences    []string `json:"source_references"`
	Path                string   `json:"path"`
	Status              string   `json:"status"`
}

type nativeOutput struct {
	device             domain.Device
	path               string
	data               []byte
	templateID         string
	templateVersion    string
	provisioningMethod string
	sourceReferences   []string
}

// GenerateNative renders a conservative, text-only native configuration for
// the existing vendor-family profiles. It never connects to or configures a device.
func GenerateNative(infrastructure domain.Infrastructure, outputDirectory string) ([]Artifact, error) {
	if problems := config.Validate(infrastructure); len(problems) > 0 {
		return nil, problems
	}
	if strings.TrimSpace(outputDirectory) == "" {
		return nil, fmt.Errorf("output directory is required")
	}

	canonical := canonicalize(infrastructure)
	canonicalInput, err := json.Marshal(canonical)
	if err != nil {
		return nil, fmt.Errorf("encode normalized input: %w", err)
	}
	inputHash := protocol.SHA256(canonicalInput)
	templates, err := loadDeviceTemplates()
	if err != nil {
		return nil, fmt.Errorf("load local device templates: %w", err)
	}
	files := make([]pendingFile, 0)
	artifacts := make([]Artifact, 0)
	for _, site := range canonical.Sites {
		outputs, err := renderNativeSite(site, templates)
		if err != nil {
			return nil, err
		}
		if len(outputs) == 0 {
			continue
		}
		manifest := nativeManifest{
			TemplateVersion: nativeTemplateVersion,
			InputHash:       inputHash,
			Status:          "rendered_unverified",
			ExecutionMode:   "render-only; review manually; no device connection or execution",
			Warnings: []string{
				"command syntax and behavior were not validated against a physical device",
				"vendor family profiles do not certify compatibility with the reported model or software version",
				"only hostname, IPv4 interface, and static-route intent is rendered; NAT and other intent are rejected",
				"generated files are inert text and are not automatically applied",
			},
			Devices: make([]nativeDeviceReport, 0, len(outputs)),
		}
		for _, output := range outputs {
			profile, err := nativeProfileFor(output.device)
			if err != nil {
				return nil, err
			}
			manifest.Devices = append(manifest.Devices, nativeDeviceReport{
				ID: output.device.ID, Name: output.device.Name, Vendor: nativeVendor(profile),
				Family: nativeFamily(profile), TemplateID: output.templateID,
				Model: output.device.Model, OSVersion: output.device.OSVersion,
				ProvisioningMethod: output.provisioningMethod,
				TemplateVersion:    output.templateVersion, InputHash: inputHash,
				OutputHash: protocol.SHA256(output.data), Compatibility: "unknown", CompatibilityStatus: "unknown",
				VerificationStatus: "not_performed", SourceReferences: append([]string(nil), output.sourceReferences...),
				Path: output.path, Status: "rendered_unverified",
			})
			files = append(files, pendingFile{path: output.path, data: output.data})
			artifacts = append(artifacts, Artifact{
				Type: "native_device_config", Path: output.path, InputHash: inputHash,
				OutputHash: protocol.SHA256(output.data),
			})
		}
		manifestBytes, err := json.MarshalIndent(manifest, "", "  ")
		if err != nil {
			return nil, fmt.Errorf("encode native manifest for site %q: %w", site.Name, err)
		}
		manifestBytes = append(manifestBytes, '\n')
		manifestPath := filepath.ToSlash(filepath.Join(site.Name, "native", "native-manifest.json"))
		files = append(files, pendingFile{path: manifestPath, data: manifestBytes})
		artifacts = append(artifacts, Artifact{
			Type: "native_manifest", Path: manifestPath, InputHash: inputHash,
			OutputHash: protocol.SHA256(manifestBytes),
		})
	}
	if len(files) == 0 {
		return artifacts, nil
	}
	root, err := filepath.Abs(outputDirectory)
	if err != nil {
		return nil, fmt.Errorf("resolve output directory: %w", err)
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, fmt.Errorf("create output directory: %w", err)
	}
	for _, file := range files {
		if err := safefs.AtomicWrite(root, file.path, file.data, 0o644); err != nil {
			return nil, fmt.Errorf("write native artifact %q: %w", file.path, err)
		}
	}
	sort.Slice(artifacts, func(i, j int) bool { return artifacts[i].Path < artifacts[j].Path })
	return artifacts, nil
}

func renderNativeSite(site domain.Site, templates []templateCandidate) ([]nativeOutput, error) {
	devices := append([]domain.Device(nil), site.Devices...)
	sort.Slice(devices, func(i, j int) bool { return devices[i].Name < devices[j].Name })
	outputs := make([]nativeOutput, 0, len(devices))
	seenIdentities := make(map[string]struct{}, len(devices))
	for _, device := range devices {
		method := strings.ToLower(strings.TrimSpace(device.Provisioning.Method))
		if device.Network == nil && (method == "" || method == "ssh" || method == "api" || method == "https") {
			continue
		}
		profile, err := nativeProfileFor(device)
		if err != nil {
			return nil, err
		}
		if device.Network == nil {
			_, _, _, _, _, _, err := renderNativeArtifact(device, profile, templates)
			if err != nil {
				return nil, err
			}
			continue
		}
		if len(device.Network.NAT.Source)+len(device.Network.NAT.Destination) > 0 {
			return nil, fmt.Errorf("device %s: native renderer does not support NAT intent", device.Name)
		}
		if !nativeHostname.MatchString(device.Name) {
			return nil, fmt.Errorf("device %s: native hostname must be a safe identifier", device.Name)
		}
		artifactID := device.ID
		if artifactID == "" {
			artifactID = device.Name
		}
		if !protocol.ValidSiteName(artifactID) || len(artifactID) > 63 {
			return nil, fmt.Errorf("device %s: native artifact identity must be a safe identifier no longer than 63 characters", device.Name)
		}
		for _, iface := range device.Network.Interfaces {
			if !nativeIdentifier.MatchString(iface.Name) {
				return nil, fmt.Errorf("device %s: native renderer cannot safely encode interface %q", device.Name, iface.Name)
			}
		}
		if device.Network.VDOM != "" && !nativeVDOM.MatchString(device.Network.VDOM) {
			return nil, fmt.Errorf("device %s: native renderer cannot safely encode VDOM %q", device.Name, device.Network.VDOM)
		}

		contents, templateID, templateVersion, provisioningMethod, sourceReferences, extension, err := renderNativeArtifact(device, profile, templates)
		if err != nil {
			return nil, err
		}
		path := filepath.ToSlash(filepath.Join(site.Name, "native", "device-"+artifactID+extension))
		identityKey := strings.ToLower(artifactID)
		if _, exists := seenIdentities[identityKey]; exists {
			return nil, fmt.Errorf("site %s: native artifact identity %q is not unique", site.Name, artifactID)
		}
		seenIdentities[identityKey] = struct{}{}
		outputs = append(outputs, nativeOutput{
			device: device, path: path, data: []byte(contents), templateID: templateID,
			templateVersion: templateVersion, provisioningMethod: provisioningMethod,
			sourceReferences: sourceReferences,
		})
	}
	return outputs, nil
}

type nativeProfile int

const (
	nativeCiscoIOSXE nativeProfile = iota
	nativeMikroTikRouterOS7
	nativeFortiOS
)

func nativeVendor(profile nativeProfile) string {
	switch profile {
	case nativeCiscoIOSXE:
		return "cisco"
	case nativeMikroTikRouterOS7:
		return "mikrotik"
	case nativeFortiOS:
		return "fortinet"
	default:
		return ""
	}
}

func nativeFamily(profile nativeProfile) string {
	switch profile {
	case nativeCiscoIOSXE:
		return "iosxe"
	case nativeMikroTikRouterOS7:
		return "routeros"
	case nativeFortiOS:
		return "fortios"
	default:
		return ""
	}
}

func nativeTemplateID(profile nativeProfile) string {
	switch profile {
	case nativeCiscoIOSXE:
		return "infraflow.cisco.iosxe.native"
	case nativeMikroTikRouterOS7:
		return "infraflow.mikrotik.routeros7.native"
	case nativeFortiOS:
		return "infraflow.fortinet.fortios.native"
	default:
		return ""
	}
}

func nativeProfileFor(device domain.Device) (nativeProfile, error) {
	vendor := strings.ToLower(strings.TrimSpace(device.Vendor))
	family := strings.ToLower(strings.TrimSpace(device.Family))
	switch {
	case vendor == "cisco" && (family == "iosxe" || family == "ios-xe" || family == "ios_xe"):
		return nativeCiscoIOSXE, nil
	case vendor == "mikrotik" && family == "routeros":
		version := strings.TrimSpace(device.OSVersion)
		if !routerOS7Version.MatchString(version) {
			return 0, fmt.Errorf("device %s: native MikroTik renderer requires an explicit RouterOS 7 version", device.Name)
		}
		return nativeMikroTikRouterOS7, nil
	case (vendor == "fortinet" || vendor == "fortigate") && (family == "fortios" || family == "fortigate"):
		if device.Network == nil || strings.TrimSpace(device.Network.VDOM) == "" {
			return 0, fmt.Errorf("device %s: FortiOS network intent requires network.vdom", device.Name)
		}
		return nativeFortiOS, nil
	default:
		return 0, fmt.Errorf("device %s: unsupported native vendor profile %s/%s", device.Name, device.Vendor, device.Family)
	}
}

func renderNativeArtifact(device domain.Device, profile nativeProfile, templates []templateCandidate) (string, string, string, string, []string, string, error) {
	method := strings.ToLower(strings.TrimSpace(device.Provisioning.Method))
	switch method {
	case "", "ssh", "api", "https":
		contents, err := renderNativeDevice(device, profile)
		return contents, nativeTemplateID(profile), nativeTemplateVersion, "", nil, ".cfg", err
	case "auto-rsc":
		if profile != nativeMikroTikRouterOS7 {
			return "", "", "", "", nil, "", fmt.Errorf("device %s: auto-rsc provisioning is only supported for MikroTik RouterOS 7", device.Name)
		}
		candidate, found := findProvisioningTemplate(templates, "infraflow.mikrotik.routeros7.auto-rsc")
		if !found {
			return "", "", "", "", nil, "", fmt.Errorf("device %s: RouterOS auto-rsc template is not registered", device.Name)
		}
		if !strings.EqualFold(strings.TrimSpace(device.Model), candidate.Metadata.Model) {
			return "", "", "", "", nil, "", fmt.Errorf("device %s: auto-rsc template %s does not support model %q", device.Name, candidate.Metadata.ID, device.Model)
		}
		if !containsExact(candidate.Metadata.OSVersions, strings.TrimSpace(device.OSVersion)) {
			return "", "", "", "", nil, "", fmt.Errorf("device %s: auto-rsc template %s does not support RouterOS version %q; declared version scope is %v", device.Name, candidate.Metadata.ID, device.OSVersion, candidate.Metadata.OSVersions)
		}
		if strings.TrimSpace(device.Provisioning.TemplateVersion) != candidate.Metadata.Version {
			return "", "", "", "", nil, "", fmt.Errorf("device %s: auto-rsc requires provisioning.template_version %q", device.Name, candidate.Metadata.Version)
		}
		if candidate.Metadata.ProvisioningMethod != method {
			return "", "", "", "", nil, "", fmt.Errorf("device %s: template provisioning method does not match %q", device.Name, method)
		}
		contents, err := renderRouterOSAutoRSC(device, candidate)
		return contents, candidate.Metadata.ID, candidate.Metadata.Version, method,
			append([]string(nil), candidate.Metadata.SourceReferences...), ".auto.rsc", err
	case "cisco-ztp":
		return "", "", "", "", nil, "", fmt.Errorf("device %s: Cisco ZTP generation is blocked; repository evidence documents HTTP/TFTP bootstrap examples but does not confirm the required TLS-only transport for a model/release profile", device.Name)
	case "cisco-autoinstall":
		return "", "", "", "", nil, "", fmt.Errorf("device %s: Cisco AutoInstall generation is unsupported; no model/release-specific discovery and filename profile is verified", device.Name)
	case "fortimanager-ztp", "fortideploy-cloud":
		return "", "", "", "", nil, "", fmt.Errorf("device %s: Fortinet ZTP generation is unsupported until an exact model, FortiOS release, backend, and required DHCP/enrollment values are established", device.Name)
	default:
		return "", "", "", "", nil, "", fmt.Errorf("device %s: unsupported native provisioning method %q", device.Name, device.Provisioning.Method)
	}
}

func findProvisioningTemplate(templates []templateCandidate, id string) (templateCandidate, bool) {
	for _, candidate := range templates {
		if candidate.Metadata.ID == id {
			return candidate, true
		}
	}
	return templateCandidate{}, false
}

func containsExact(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

type routerOSInterfaceTemplateData struct {
	Name        string
	IPv4Mode    string
	IPv4Address string
}

type routerOSRouteTemplateData struct {
	Destination string
	Gateway     string
	Distance    int
}

type routerOSAutoRSCTemplateData struct {
	Name       string
	Interfaces []routerOSInterfaceTemplateData
	Routes     []routerOSRouteTemplateData
}

func renderRouterOSAutoRSC(device domain.Device, candidate templateCandidate) (string, error) {
	if device.Network == nil {
		return "", fmt.Errorf("device %s: RouterOS auto-rsc requires network intent", device.Name)
	}
	if !nativeHostname.MatchString(device.Name) {
		return "", fmt.Errorf("device %s: RouterOS identity must be a safe identifier", device.Name)
	}
	if len(device.Network.Interfaces) == 0 {
		return "", fmt.Errorf("device %s: RouterOS auto-rsc requires at least one interface", device.Name)
	}
	interfaces := append([]domain.NetworkInterface(nil), device.Network.Interfaces...)
	sort.Slice(interfaces, func(i, j int) bool { return interfaces[i].Name < interfaces[j].Name })
	data := routerOSAutoRSCTemplateData{Name: device.Name, Interfaces: make([]routerOSInterfaceTemplateData, 0, len(interfaces))}
	for _, iface := range interfaces {
		if !nativeIdentifier.MatchString(iface.Name) {
			return "", fmt.Errorf("device %s: RouterOS template cannot safely encode interface %q", device.Name, iface.Name)
		}
		if iface.Enabled != nil {
			return "", fmt.Errorf("device %s interface %s: RouterOS auto-rsc does not infer admin-state commands from generic enabled intent", device.Name, iface.Name)
		}
		if iface.IPv4Mode == "static" {
			if _, _, err := nativeAddressAndMask(iface.IPv4Address); err != nil {
				return "", fmt.Errorf("device %s interface %s: %w", device.Name, iface.Name, err)
			}
		}
		data.Interfaces = append(data.Interfaces, routerOSInterfaceTemplateData{
			Name: iface.Name, IPv4Mode: iface.IPv4Mode, IPv4Address: iface.IPv4Address,
		})
	}
	routes := append([]domain.StaticRoute(nil), device.Network.Routes...)
	sort.Slice(routes, func(i, j int) bool {
		if routes[i].Destination != routes[j].Destination {
			return routes[i].Destination < routes[j].Destination
		}
		if routes[i].NextHop != routes[j].NextHop {
			return routes[i].NextHop < routes[j].NextHop
		}
		return routes[i].Interface < routes[j].Interface
	})
	for _, route := range routes {
		if route.Distance < 0 {
			return "", fmt.Errorf("device %s route %s: distance must not be negative", device.Name, route.Destination)
		}
		if _, err := nativeNetworkPrefix(route.Destination); err != nil {
			return "", fmt.Errorf("device %s route %s: %w", device.Name, route.Destination, err)
		}
		gateway := route.NextHop
		if gateway == "" {
			gateway = route.Interface
		}
		ip := net.ParseIP(gateway)
		if !nativeIdentifier.MatchString(gateway) && (ip == nil || ip.To4() == nil) {
			return "", fmt.Errorf("device %s route %s: RouterOS template cannot safely encode gateway %q", device.Name, route.Destination, gateway)
		}
		data.Routes = append(data.Routes, routerOSRouteTemplateData{
			Destination: route.Destination, Gateway: gateway, Distance: route.Distance,
		})
	}
	var output strings.Builder
	if err := candidate.Parsed.Execute(&output, data); err != nil {
		return "", fmt.Errorf("execute template %q: %w", candidate.Metadata.ID, err)
	}
	if strings.TrimSpace(output.String()) == "" {
		return "", fmt.Errorf("template %q produced an empty script", candidate.Metadata.ID)
	}
	return output.String(), nil
}

func renderNativeDevice(device domain.Device, profile nativeProfile) (string, error) {
	var out strings.Builder
	switch profile {
	case nativeCiscoIOSXE:
		fmt.Fprintf(&out, "hostname %s\n", device.Name)
		for _, iface := range device.Network.Interfaces {
			fmt.Fprintf(&out, "interface %s\n", iface.Name)
			switch iface.IPv4Mode {
			case "static":
				address, mask, err := nativeAddressAndMask(iface.IPv4Address)
				if err != nil {
					return "", fmt.Errorf("device %s interface %s: %w", device.Name, iface.Name, err)
				}
				fmt.Fprintf(&out, " ip address %s %s\n", address, mask)
			case "dhcp":
				out.WriteString(" ip address dhcp\n")
			default:
				return "", fmt.Errorf("device %s interface %s: unsupported IPv4 mode %q", device.Name, iface.Name, iface.IPv4Mode)
			}
			if iface.Enabled != nil {
				if *iface.Enabled {
					out.WriteString(" no shutdown\n")
				} else {
					out.WriteString(" shutdown\n")
				}
			}
			out.WriteString("!\n")
		}
		for _, route := range device.Network.Routes {
			destination, mask, err := nativeNetworkAddressAndMask(route.Destination)
			if err != nil {
				return "", fmt.Errorf("device %s route %s: %w", device.Name, route.Destination, err)
			}
			fmt.Fprintf(&out, "ip route %s %s", destination, mask)
			if route.Interface != "" {
				fmt.Fprintf(&out, " %s", route.Interface)
			}
			if route.NextHop != "" {
				fmt.Fprintf(&out, " %s", route.NextHop)
			}
			if route.Distance > 0 {
				fmt.Fprintf(&out, " %d", route.Distance)
			}
			out.WriteByte('\n')
		}
	case nativeMikroTikRouterOS7:
		fmt.Fprintf(&out, `/system identity set name="%s"`+"\n", device.Name)
		for _, iface := range device.Network.Interfaces {
			switch iface.IPv4Mode {
			case "static":
				fmt.Fprintf(&out, `/ip address add address=%s interface="%s"`+"\n", iface.IPv4Address, iface.Name)
			case "dhcp":
				fmt.Fprintf(&out, `/ip dhcp-client add interface="%s" disabled=no`+"\n", iface.Name)
			default:
				return "", fmt.Errorf("device %s interface %s: unsupported IPv4 mode %q", device.Name, iface.Name, iface.IPv4Mode)
			}
			if iface.Enabled != nil {
				return "", fmt.Errorf("device %s interface %s: native RouterOS renderer cannot apply enabled intent without a verified interface-type mapping", device.Name, iface.Name)
			}
		}
		for _, route := range device.Network.Routes {
			gateway := route.NextHop
			if gateway == "" {
				gateway = route.Interface
			}
			fmt.Fprintf(&out, `/ip route add dst-address=%s gateway="%s"`, route.Destination, gateway)
			if route.Distance > 0 {
				fmt.Fprintf(&out, " distance=%d", route.Distance)
			}
			out.WriteByte('\n')
		}
	case nativeFortiOS:
		fmt.Fprintf(&out, "config global\n    config system global\n        set hostname %s\n    end\nend\n", device.Name)
		fmt.Fprintf(&out, "config vdom\n    edit %s\n", device.Network.VDOM)
		if len(device.Network.Interfaces) > 0 {
			out.WriteString("        config system interface\n")
			for _, iface := range device.Network.Interfaces {
				fmt.Fprintf(&out, "            edit %q\n", iface.Name)
				switch iface.IPv4Mode {
				case "static":
					address, mask, err := nativeAddressAndMask(iface.IPv4Address)
					if err != nil {
						return "", fmt.Errorf("device %s interface %s: %w", device.Name, iface.Name, err)
					}
					fmt.Fprintf(&out, "                set mode static\n                set ip %s %s\n", address, mask)
				case "dhcp":
					out.WriteString("                set mode dhcp\n")
				default:
					return "", fmt.Errorf("device %s interface %s: unsupported IPv4 mode %q", device.Name, iface.Name, iface.IPv4Mode)
				}
				if iface.Enabled != nil {
					if *iface.Enabled {
						out.WriteString("                set status up\n")
					} else {
						out.WriteString("                set status down\n")
					}
				}
				out.WriteString("            next\n")
			}
			out.WriteString("        end\n")
		}
		if len(device.Network.Routes) > 0 {
			out.WriteString("        config router static\n")
			for _, route := range device.Network.Routes {
				destination, mask, err := nativeNetworkAddressAndMask(route.Destination)
				if err != nil {
					return "", fmt.Errorf("device %s route %s: %w", device.Name, route.Destination, err)
				}
				fmt.Fprintf(&out, "            edit %d\n", route.FortinetSequence)
				fmt.Fprintf(&out, "                set dst %s %s\n", destination, mask)
				if route.NextHop != "" {
					fmt.Fprintf(&out, "                set gateway %s\n", route.NextHop)
				}
				fmt.Fprintf(&out, "                set device %q\n", route.Interface)
				if route.Distance > 0 {
					fmt.Fprintf(&out, "                set distance %d\n", route.Distance)
				}
				out.WriteString("            next\n")
			}
			out.WriteString("        end\n")
		}
		out.WriteString("    next\nend\n")
	default:
		return "", fmt.Errorf("unsupported native profile")
	}
	return out.String(), nil
}

func nativeAddressAndMask(prefix string) (string, string, error) {
	address, network, err := net.ParseCIDR(prefix)
	if err != nil || address.To4() == nil || network == nil {
		return "", "", fmt.Errorf("expected IPv4 CIDR, got %q", prefix)
	}
	mask := net.IP(network.Mask).String()
	return address.To4().String(), mask, nil
}

func nativeNetworkAddressAndMask(prefix string) (string, string, error) {
	_, network, err := net.ParseCIDR(prefix)
	if err != nil || network == nil || network.IP.To4() == nil {
		return "", "", fmt.Errorf("expected IPv4 network CIDR, got %q", prefix)
	}
	return network.IP.To4().String(), net.IP(network.Mask).String(), nil
}

func nativeNetworkPrefix(prefix string) (string, error) {
	_, network, err := net.ParseCIDR(prefix)
	if err != nil || network == nil || network.IP.To4() == nil {
		return "", fmt.Errorf("expected IPv4 network CIDR, got %q", prefix)
	}
	return network.String(), nil
}
