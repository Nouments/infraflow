package generator

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/template"

	"infraflow/internal/adapters/config"
	"infraflow/internal/domain"
	"infraflow/internal/infrastructure/safefs"
	"infraflow/pkg/protocol"
)

// Bootstrap templates are trusted, versioned provider assets. User values are
// serialized as JSON before they are inserted into JSON templates.
//
//go:embed templates/bootstrap
var bootstrapTemplates embed.FS

type bootstrapPendingFile struct {
	path string
	data []byte
}

type bootstrapDHCP struct {
	Version           int                    `json:"version"`
	Service           string                 `json:"service"`
	Enabled           bool                   `json:"enabled"`
	Site              string                 `json:"site"`
	Network           string                 `json:"network"`
	Gateway           string                 `json:"gateway"`
	Pools             []bootstrapPool        `json:"pools"`
	ReservedAddresses []string               `json:"reserved_addresses"`
	Reservations      []bootstrapReservation `json:"reservations"`
	Options           bootstrapDHCPOptions   `json:"options"`
}

type bootstrapPool struct {
	Start string `json:"start"`
	End   string `json:"end"`
}

type bootstrapReservation struct {
	MAC      string `json:"mac"`
	IP       string `json:"ip"`
	Hostname string `json:"hostname"`
}

type bootstrapDHCPOptions struct {
	DNS                []string `json:"dns_servers"`
	NextServer         string   `json:"next_server"`
	NextServerSource   string   `json:"next_server_source"`
	BootMode           string   `json:"boot_mode"`
	BootFilename       string   `json:"boot_filename"`
	TFTPDirectory      string   `json:"tftp_directory"`
	IPXEScript         string   `json:"ipxe_script"`
	CiscoAutoInstall   bool     `json:"cisco_autoinstall,omitempty"`
	FortiManagerIP     string   `json:"fortimanager_ip,omitempty"`
	FortiManagerDomain string   `json:"fortimanager_domain,omitempty"`
}

type bootstrapDNS struct {
	Version        int                  `json:"version"`
	Service        string               `json:"service"`
	Enabled        bool                 `json:"enabled"`
	Site           string               `json:"site"`
	Zone           string               `json:"zone"`
	TTL            int                  `json:"ttl"`
	Records        []bootstrapDNSRecord `json:"records"`
	ReverseRecords []bootstrapDNSRecord `json:"reverse_records"`
}

type bootstrapDNSRecord struct {
	Name  string `json:"name"`
	Type  string `json:"type"`
	Value string `json:"value"`
	TTL   int    `json:"ttl"`
}

type bootstrapTFTP struct {
	Version       int                 `json:"version"`
	Service       string              `json:"service"`
	Enabled       bool                `json:"enabled"`
	Site          string              `json:"site"`
	RootDirectory string              `json:"root_directory"`
	ReadOnly      bool                `json:"read_only"`
	AllowedFiles  []bootstrapTFTPFile `json:"allowed_files"`
}

type bootstrapTFTPFile struct {
	Path   string `json:"path"`
	Source string `json:"source"`
}

type bootstrapPXE struct {
	Version       int      `json:"version"`
	Service       string   `json:"service"`
	Enabled       bool     `json:"enabled"`
	Site          string   `json:"site"`
	TFTPDirectory string   `json:"tftp_directory"`
	HTTPDirectory string   `json:"http_directory"`
	Scripts       []string `json:"scripts"`
	Images        []string `json:"images"`
}

// GenerateBootstrap creates provider-side static artifacts only for services
// explicitly enabled by the site. It does not start services, access devices,
// or include credentials.
func GenerateBootstrap(infrastructure domain.Infrastructure, outputDirectory string) ([]Artifact, error) {
	if strings.TrimSpace(outputDirectory) == "" {
		return nil, fmt.Errorf("output directory is required")
	}
	files, artifacts, err := prepareBootstrapArtifacts(infrastructure)
	if err != nil {
		return nil, err
	}
	return withOutputGenerationLock(outputDirectory, func(root string) ([]Artifact, error) {
		if err := writeBootstrapArtifacts(root, files); err != nil {
			return nil, err
		}
		return artifacts, nil
	})
}

func generateBootstrap(infrastructure domain.Infrastructure, outputDirectory string) ([]Artifact, error) {
	files, artifacts, err := prepareBootstrapArtifacts(infrastructure)
	if err != nil {
		return nil, err
	}
	if err := writeBootstrapArtifacts(outputDirectory, files); err != nil {
		return nil, err
	}
	return artifacts, nil
}

func prepareBootstrapArtifacts(infrastructure domain.Infrastructure) ([]bootstrapPendingFile, []Artifact, error) {
	if problems := config.Validate(infrastructure); len(problems) > 0 {
		return nil, nil, problems
	}

	canonical := canonicalize(infrastructure)
	canonicalInput, err := json.Marshal(canonical)
	if err != nil {
		return nil, nil, fmt.Errorf("encode normalized input: %w", err)
	}
	inputHash := protocol.SHA256(canonicalInput)
	files := make([]bootstrapPendingFile, 0, len(canonical.Sites)*6)
	artifacts := make([]Artifact, 0, len(canonical.Sites)*6)
	var nativeTemplates []templateCandidate
	for _, site := range canonical.Sites {
		if siteHasCiscoAutoInstall(site) {
			var err error
			nativeTemplates, err = loadDeviceTemplates()
			if err != nil {
				return nil, nil, fmt.Errorf("load Cisco AutoInstall native templates: %w", err)
			}
			break
		}
	}
	for _, site := range canonical.Sites {
		if err := validateSiteBootstrapProfiles(site); err != nil {
			return nil, nil, err
		}
		siteArtifactStart := len(artifacts)
		autoInstallFiles, autoInstallArtifacts, err := buildCiscoAutoInstallArtifacts(site, nativeTemplates, inputHash)
		if err != nil {
			return nil, nil, err
		}
		files = append(files, autoInstallFiles...)
		artifacts = append(artifacts, autoInstallArtifacts...)
		type bootstrapJSONFile struct {
			name     string
			template string
			value    any
			typeName string
		}
		jsonFiles := make([]bootstrapJSONFile, 0, 4)
		if bootstrapServiceEnabled(site, "dhcp") {
			dhcp, err := buildBootstrapDHCP(site)
			if err != nil {
				return nil, nil, err
			}
			jsonFiles = append(jsonFiles, bootstrapJSONFile{
				name: "dhcp/config.json", template: "templates/bootstrap/dhcp/config.json.tmpl",
				value: dhcp, typeName: "bootstrap_dhcp",
			})
		}
		if bootstrapServiceEnabled(site, "dns") {
			dns, err := buildBootstrapDNS(site)
			if err != nil {
				return nil, nil, err
			}
			jsonFiles = append(jsonFiles, bootstrapJSONFile{
				name: "dns/config.json", template: "templates/bootstrap/dns/config.json.tmpl",
				value: dns, typeName: "bootstrap_dns",
			})
		}
		if bootstrapServiceEnabled(site, "tftp") {
			jsonFiles = append(jsonFiles, bootstrapJSONFile{
				name: "tftp/config.json", template: "templates/bootstrap/tftp/config.json.tmpl",
				value: buildBootstrapTFTP(site, autoInstallTFTPFiles(site)), typeName: "bootstrap_tftp",
			})
		}
		pxeEnabled := bootstrapPXEEnabled(site)
		if pxeEnabled {
			jsonFiles = append(jsonFiles, bootstrapJSONFile{
				name: "pxe/metadata.json", template: "templates/bootstrap/pxe/metadata.json.tmpl",
				value: buildBootstrapPXE(site), typeName: "bootstrap_pxe",
			})
		}
		for _, item := range jsonFiles {
			data, err := renderBootstrapJSON(item.template, item.value)
			if err != nil {
				return nil, nil, fmt.Errorf("render %s for site %q: %w", item.name, site.Name, err)
			}
			appendBootstrapArtifact(&files, &artifacts, site.Name, item.name, item.typeName, data, inputHash)
		}

		if pxeEnabled {
			for _, item := range []struct {
				name     string
				template string
				typeName string
			}{
				{"pxe/ipxe/bootstrap.ipxe", "templates/bootstrap/pxe/ipxe/bootstrap.ipxe.tmpl", "bootstrap_ipxe_script"},
				{"pxe/ipxe/menu.ipxe", "templates/bootstrap/pxe/ipxe/menu.ipxe.tmpl", "bootstrap_ipxe_menu"},
			} {
				data, err := renderBootstrapTemplate(item.template, map[string]string{"SiteName": site.Name})
				if err != nil {
					return nil, nil, fmt.Errorf("render %s for site %q: %w", item.name, site.Name, err)
				}
				appendBootstrapArtifact(&files, &artifacts, site.Name, item.name, item.typeName, data, inputHash)
			}
		}
		siteArtifacts := append([]Artifact(nil), artifacts[siteArtifactStart:]...)
		if len(siteArtifacts) == 0 {
			continue
		}
		manifest, err := marshal(Manifest{
			GeneratorVersion: Version,
			TemplateVersion:  TemplateVersion,
			InputHash:        inputHash,
			Artifacts:        siteArtifacts,
		})
		if err != nil {
			return nil, nil, fmt.Errorf("render bootstrap manifest for site %q: %w", site.Name, err)
		}
		files = append(files, bootstrapPendingFile{
			path: filepath.ToSlash(filepath.Join(site.Name, "manifest.json")),
			data: manifest,
		})
	}

	return files, artifacts, nil
}

func validateSiteBootstrapProfiles(site domain.Site) error {
	if !bootstrapPXEEnabled(site) && !bootstrapServiceEnabled(site, "dhcp") && !bootstrapServiceEnabled(site, "tftp") {
		return nil
	}
	for _, device := range site.Devices {
		profile, known := domain.BootstrapProfileFor(device)
		method := strings.ToLower(strings.TrimSpace(string(device.Provisioning.Method)))
		methodOutputImplemented := method == string(domain.ProvisioningMethodFortiManagerZTP) ||
			method == string(domain.ProvisioningMethodFortiOSFortiManager) ||
			method == string(domain.ProvisioningMethodCiscoIOSXEAutoInstall)
		if !known || profile.State == domain.CapabilityImplemented || profile.State == domain.CapabilityLabVerified || (profile.State == domain.CapabilityPartial && methodOutputImplemented) {
			continue
		}
		return fmt.Errorf("site %s, device %s: bootstrap method %q cannot use generic DHCP/TFTP/PXE output: %s", site.Name, device.Name, device.Provisioning.Method, profile.Reason)
	}
	return nil
}

func writeBootstrapArtifacts(outputDirectory string, files []bootstrapPendingFile) error {
	root, err := filepath.Abs(outputDirectory)
	if err != nil {
		return fmt.Errorf("resolve output directory: %w", err)
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}
	for _, file := range files {
		if err := safefs.AtomicWrite(root, file.path, file.data, 0o644); err != nil {
			return fmt.Errorf("write bootstrap artifact %q: %w", file.path, err)
		}
	}
	return nil
}

func appendBootstrapArtifact(files *[]bootstrapPendingFile, artifacts *[]Artifact, site, name, typeName string, data []byte, inputHash string) {
	path := filepath.ToSlash(filepath.Join(site, "bootstrap", name))
	*files = append(*files, bootstrapPendingFile{path: path, data: data})
	*artifacts = append(*artifacts, Artifact{Type: typeName, Path: path, InputHash: inputHash, OutputHash: protocol.SHA256(data)})
}

func buildBootstrapDHCP(site domain.Site) (bootstrapDHCP, error) {
	_, network, err := net.ParseCIDR(site.Bootstrap.Network)
	if err != nil || network.IP.To4() == nil {
		return bootstrapDHCP{}, fmt.Errorf("site %q: bootstrap network must be a valid IPv4 CIDR for DHCP generation", site.Name)
	}
	networkIP := network.IP.To4()
	ones, bits := network.Mask.Size()
	if bits != 32 || ones >= 31 {
		return bootstrapDHCP{}, fmt.Errorf("site %q: bootstrap network must have at least two usable IPv4 addresses", site.Name)
	}
	first := ipToUint32(networkIP) + 1
	last := ipToUint32(networkIP) + (1 << uint(bits-ones)) - 2
	reserved := make(map[uint32]struct{})
	if site.Bootstrap.Gateway != "" {
		gateway := net.ParseIP(site.Bootstrap.Gateway).To4()
		if gateway == nil || ipToUint32(gateway) < first || ipToUint32(gateway) > last {
			return bootstrapDHCP{}, fmt.Errorf("site %q: bootstrap gateway %s is not a usable DHCP host address", site.Name, site.Bootstrap.Gateway)
		}
		reserved[ipToUint32(gateway)] = struct{}{}
	}
	reservations := make([]bootstrapReservation, 0)
	seenDeviceIPs := make(map[uint32]string)
	for _, device := range site.Devices {
		if device.Management.IPv4 == "" {
			continue
		}
		ip := net.ParseIP(device.Management.IPv4).To4()
		if ip == nil || !network.Contains(ip) {
			continue
		}
		value := ipToUint32(ip)
		if value < first || value > last {
			return bootstrapDHCP{}, fmt.Errorf("site %q, device %q: management address %s is not a usable DHCP host address", site.Name, device.Name, device.Management.IPv4)
		}
		if site.Bootstrap.Gateway != "" && ipToUint32(net.ParseIP(site.Bootstrap.Gateway).To4()) == value {
			return bootstrapDHCP{}, fmt.Errorf("site %q, device %q: management address %s conflicts with the bootstrap gateway", site.Name, device.Name, device.Management.IPv4)
		}
		if previous, exists := seenDeviceIPs[value]; exists && previous != device.Name {
			return bootstrapDHCP{}, fmt.Errorf("site %q: devices %q and %q share management address %s", site.Name, previous, device.Name, device.Management.IPv4)
		}
		seenDeviceIPs[value] = device.Name
		reserved[value] = struct{}{}
		for _, rawMAC := range device.Identity.MACs {
			mac, parseErr := net.ParseMAC(rawMAC)
			if parseErr != nil {
				return bootstrapDHCP{}, fmt.Errorf("site %q, device %q: invalid MAC address %q", site.Name, device.Name, rawMAC)
			}
			reservations = append(reservations, bootstrapReservation{MAC: strings.ToLower(mac.String()), IP: device.Management.IPv4, Hostname: device.Name})
		}
	}
	sort.Slice(reservations, func(i, j int) bool {
		if reservations[i].IP != reservations[j].IP {
			return reservations[i].IP < reservations[j].IP
		}
		return reservations[i].MAC < reservations[j].MAC
	})
	reservedValues := make([]uint32, 0, len(reserved))
	for address := range reserved {
		if address >= first && address <= last {
			reservedValues = append(reservedValues, address)
		}
	}
	sort.Slice(reservedValues, func(i, j int) bool { return reservedValues[i] < reservedValues[j] })
	pools := make([]bootstrapPool, 0, len(reservedValues)+1)
	cursor := first
	for _, address := range reservedValues {
		if address > cursor {
			pools = append(pools, bootstrapPool{Start: uintToIP(cursor).String(), End: uintToIP(address - 1).String()})
		}
		if address >= cursor {
			cursor = address + 1
		}
	}
	if cursor <= last {
		pools = append(pools, bootstrapPool{Start: uintToIP(cursor).String(), End: uintToIP(last).String()})
	}
	reservedAddresses := make([]string, 0, len(reservedValues))
	for _, address := range reservedValues {
		reservedAddresses = append(reservedAddresses, uintToIP(address).String())
	}
	options := bootstrapDHCPOptions{DNS: []string{}, NextServerSource: "agent_runtime", IPXEScript: "pxe/ipxe/bootstrap.ipxe"}
	for _, device := range site.Devices {
		method := strings.ToLower(strings.TrimSpace(string(device.Provisioning.Method)))
		if method == string(domain.ProvisioningMethodCiscoAutoInstall) || method == string(domain.ProvisioningMethodCiscoIOSXEAutoInstall) {
			options.CiscoAutoInstall = true
			options.BootMode = "cisco-autoinstall"
		} else if method == string(domain.ProvisioningMethodFortiManagerZTP) || method == string(domain.ProvisioningMethodFortiOSFortiManager) {
			if site.Bootstrap.FortiManager == nil {
				return bootstrapDHCP{}, fmt.Errorf("site %q: FortiManager ZTP requires bootstrap.fortimanager.ip and bootstrap.fortimanager.domain", site.Name)
			}
			options.FortiManagerIP = site.Bootstrap.FortiManager.IP
			options.FortiManagerDomain = site.Bootstrap.FortiManager.Domain
			options.BootMode = "fortimanager-ztp"
		}
	}
	if bootstrapServiceEnabled(site, "tftp") {
		options.TFTPDirectory = "tftp"
		if !options.CiscoAutoInstall && options.FortiManagerIP == "" {
			options.BootMode = "ipxe"
			options.BootFilename = "undionly.kpxe"
		}
	}
	if options.CiscoAutoInstall && (!bootstrapServiceEnabled(site, "dhcp") || !bootstrapServiceEnabled(site, "tftp")) {
		return bootstrapDHCP{}, fmt.Errorf("site %q: Cisco IOS AutoInstall requires both DHCP and TFTP services", site.Name)
	}
	return bootstrapDHCP{
		Version: 1, Service: "dhcp", Enabled: bootstrapServiceEnabled(site, "dhcp"), Site: site.Name,
		Network: site.Bootstrap.Network, Gateway: site.Bootstrap.Gateway, Pools: pools,
		ReservedAddresses: reservedAddresses, Reservations: reservations,
		Options: options,
	}, nil
}

func buildBootstrapDNS(site domain.Site) (bootstrapDNS, error) {
	siteLabel, err := dnsLabel(site.Name)
	if err != nil {
		return bootstrapDNS{}, fmt.Errorf("site %q: %w", site.Name, err)
	}
	zone := siteLabel + ".infraflow.local"
	records := make([]bootstrapDNSRecord, 0)
	reverseRecords := make([]bootstrapDNSRecord, 0)
	seenLabels := make(map[string]string)
	for _, device := range site.Devices {
		if device.Management.IPv4 == "" {
			continue
		}
		label, labelErr := dnsLabel(device.Name)
		if labelErr != nil {
			return bootstrapDNS{}, fmt.Errorf("site %q, device %q: %w", site.Name, device.Name, labelErr)
		}
		if previous, exists := seenLabels[label]; exists && previous != device.Name {
			return bootstrapDNS{}, fmt.Errorf("site %q: device DNS names %q and %q collide as %q", site.Name, previous, device.Name, label)
		}
		seenLabels[label] = device.Name
		fqdn := label + "." + zone
		records = append(records, bootstrapDNSRecord{Name: fqdn, Type: "A", Value: device.Management.IPv4, TTL: 60})
		reverseRecords = append(reverseRecords, bootstrapDNSRecord{Name: reverseIPv4(device.Management.IPv4), Type: "PTR", Value: fqdn, TTL: 60})
	}
	sort.Slice(records, func(i, j int) bool { return records[i].Name < records[j].Name })
	sort.Slice(reverseRecords, func(i, j int) bool { return reverseRecords[i].Name < reverseRecords[j].Name })
	return bootstrapDNS{Version: 1, Service: "dns", Enabled: bootstrapServiceEnabled(site, "dns"), Site: site.Name, Zone: zone, TTL: 60, Records: records, ReverseRecords: reverseRecords}, nil
}

func buildBootstrapTFTP(site domain.Site, extraFiles []string) bootstrapTFTP {
	allowed := []bootstrapTFTPFile{
		{Path: "undionly.kpxe", Source: "agent_runtime"},
		{Path: "ipxe.efi", Source: "agent_runtime"},
	}
	for _, name := range extraFiles {
		allowed = append(allowed, bootstrapTFTPFile{Path: name, Source: "agent_runtime"})
	}
	return bootstrapTFTP{
		Version: 1, Service: "tftp", Enabled: bootstrapServiceEnabled(site, "tftp"), Site: site.Name,
		RootDirectory: "tftp", ReadOnly: true,
		AllowedFiles: allowed,
	}
}

func autoInstallTFTPFiles(site domain.Site) []string {
	if !siteHasCiscoAutoInstall(site) {
		return nil
	}
	files := []string{"network-confg"}
	devices := append([]domain.Device(nil), site.Devices...)
	sort.Slice(devices, func(i, j int) bool { return devices[i].Name < devices[j].Name })
	for _, device := range devices {
		if isCiscoAutoInstallMethod(device.Provisioning.Method) {
			files = append(files, device.Name+"-confg")
		}
	}
	return files
}

func siteHasCiscoAutoInstall(site domain.Site) bool {
	for _, device := range site.Devices {
		if isCiscoAutoInstallMethod(device.Provisioning.Method) {
			return true
		}
	}
	return false
}

func isCiscoAutoInstallMethod(method domain.ProvisioningMethod) bool {
	value := strings.ToLower(strings.TrimSpace(string(method)))
	return value == string(domain.ProvisioningMethodCiscoAutoInstall) || value == string(domain.ProvisioningMethodCiscoIOSXEAutoInstall)
}

func buildCiscoAutoInstallArtifacts(site domain.Site, templates []templateCandidate, inputHash string) ([]bootstrapPendingFile, []Artifact, error) {
	if !siteHasCiscoAutoInstall(site) {
		return nil, nil, nil
	}
	if templates == nil {
		return nil, nil, fmt.Errorf("site %q: load native templates for Cisco AutoInstall", site.Name)
	}
	if !bootstrapServiceEnabled(site, "dhcp") || !bootstrapServiceEnabled(site, "tftp") {
		return nil, nil, fmt.Errorf("site %q: Cisco IOS AutoInstall requires both DHCP and TFTP services", site.Name)
	}
	if _, _, err := net.ParseCIDR(site.Bootstrap.Network); err != nil {
		return nil, nil, fmt.Errorf("site %q: Cisco IOS AutoInstall requires a valid DHCP bootstrap network", site.Name)
	}
	_, network, _ := net.ParseCIDR(site.Bootstrap.Network)
	devices := append([]domain.Device(nil), site.Devices...)
	sort.Slice(devices, func(i, j int) bool { return devices[i].Name < devices[j].Name })
	var networkConfig strings.Builder
	files := make([]bootstrapPendingFile, 0, len(devices)+1)
	artifacts := make([]Artifact, 0, len(devices)+1)
	for _, device := range devices {
		if !isCiscoAutoInstallMethod(device.Provisioning.Method) {
			continue
		}
		if _, err := nativeProfileFor(device); err != nil {
			return nil, nil, err
		}
		ip := net.ParseIP(device.Management.IPv4).To4()
		if ip == nil || !network.Contains(ip) {
			return nil, nil, fmt.Errorf("site %q, device %q: Cisco AutoInstall requires management.ipv4 inside the DHCP bootstrap network", site.Name, device.Name)
		}
		if len(device.Identity.MACs) == 0 {
			return nil, nil, fmt.Errorf("site %q, device %q: Cisco AutoInstall requires an Ethernet MAC for a deterministic DHCP reservation", site.Name, device.Name)
		}
		for _, mac := range device.Identity.MACs {
			parsed, err := net.ParseMAC(mac)
			if err != nil || len(parsed) != 6 {
				return nil, nil, fmt.Errorf("site %q, device %q: Cisco AutoInstall requires a valid Ethernet MAC address", site.Name, device.Name)
			}
		}
		if !nativeHostname.MatchString(device.Name) {
			return nil, nil, fmt.Errorf("site %q, device %q: Cisco AutoInstall hostname is not a safe IOS hostname", site.Name, device.Name)
		}
		profile, err := nativeProfileFor(device)
		if err != nil {
			return nil, nil, err
		}
		rendered, err := renderNativeArtifact(device, profile, templates)
		if err != nil {
			return nil, nil, err
		}
		networkConfig.WriteString("ip host ")
		networkConfig.WriteString(device.Name)
		networkConfig.WriteByte(' ')
		networkConfig.WriteString(ip.String())
		networkConfig.WriteByte('\n')
		path := filepath.ToSlash(filepath.Join(site.Name, "bootstrap", "tftp", device.Name+"-confg"))
		data := []byte(rendered.contents)
		files = append(files, bootstrapPendingFile{path: path, data: data})
		artifacts = append(artifacts, Artifact{Type: "bootstrap_cisco_autoinstall_config", Path: path, InputHash: inputHash, OutputHash: protocol.SHA256(data)})
	}
	if len(artifacts) == 0 {
		return nil, nil, fmt.Errorf("site %q: AutoInstall method did not select a supported Cisco IOS device", site.Name)
	}
	path := filepath.ToSlash(filepath.Join(site.Name, "bootstrap", "tftp", "network-confg"))
	data := []byte(networkConfig.String())
	files = append(files, bootstrapPendingFile{path: path, data: data})
	artifacts = append(artifacts, Artifact{Type: "bootstrap_cisco_autoinstall_network", Path: path, InputHash: inputHash, OutputHash: protocol.SHA256(data)})
	return files, artifacts, nil
}

func buildBootstrapPXE(site domain.Site) bootstrapPXE {
	return bootstrapPXE{
		Version: 1, Service: "pxe", Enabled: bootstrapPXEEnabled(site), Site: site.Name,
		TFTPDirectory: "tftp", HTTPDirectory: "pxe", Scripts: []string{"ipxe/bootstrap.ipxe", "ipxe/menu.ipxe"}, Images: []string{},
	}
}

func bootstrapPXEEnabled(site domain.Site) bool {
	if enabled, exists := site.Services["pxe"]; exists {
		return enabled
	}
	if enabled, exists := site.Services["ipxe"]; exists {
		return enabled
	}
	return false
}

func bootstrapServiceEnabled(site domain.Site, service string) bool {
	enabled, exists := site.Services[service]
	if !exists {
		return false
	}
	return enabled
}

func renderBootstrapJSON(templatePath string, value any) ([]byte, error) {
	payload, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode JSON payload: %w", err)
	}
	data, err := renderBootstrapTemplate(templatePath, map[string]string{"Payload": string(payload)})
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

func renderBootstrapTemplate(templatePath string, data any) ([]byte, error) {
	templateBytes, err := bootstrapTemplates.ReadFile(templatePath)
	if err != nil {
		return nil, fmt.Errorf("read template: %w", err)
	}
	parsed, err := template.New(filepath.Base(templatePath)).Option("missingkey=error").Parse(string(templateBytes))
	if err != nil {
		return nil, fmt.Errorf("parse template: %w", err)
	}
	var output bytes.Buffer
	if err := parsed.Execute(&output, data); err != nil {
		return nil, fmt.Errorf("execute template: %w", err)
	}
	return output.Bytes(), nil
}

func dnsLabel(value string) (string, error) {
	var builder strings.Builder
	lastHyphen := false
	for _, character := range strings.ToLower(value) {
		switch {
		case character >= 'a' && character <= 'z', character >= '0' && character <= '9':
			builder.WriteRune(character)
			lastHyphen = false
		default:
			if builder.Len() > 0 && !lastHyphen {
				builder.WriteByte('-')
				lastHyphen = true
			}
		}
	}
	label := strings.Trim(builder.String(), "-")
	if label == "" {
		return "", fmt.Errorf("name cannot be converted to a DNS label")
	}
	return label, nil
}

func reverseIPv4(value string) string {
	ip := net.ParseIP(value).To4()
	return fmt.Sprintf("%d.%d.%d.%d.in-addr.arpa", ip[3], ip[2], ip[1], ip[0])
}

func ipToUint32(ip net.IP) uint32 {
	return uint32(ip[0])<<24 | uint32(ip[1])<<16 | uint32(ip[2])<<8 | uint32(ip[3])
}

func uintToIP(value uint32) net.IP {
	return net.IPv4(byte(value>>24), byte(value>>16), byte(value>>8), byte(value))
}
