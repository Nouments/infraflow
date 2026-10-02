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
	DNS              []string `json:"dns_servers"`
	NextServer       string   `json:"next_server"`
	NextServerSource string   `json:"next_server_source"`
	BootMode         string   `json:"boot_mode"`
	BootFilename     string   `json:"boot_filename"`
	TFTPDirectory    string   `json:"tftp_directory"`
	IPXEScript       string   `json:"ipxe_script"`
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

// GenerateBootstrap creates provider-side static bootstrap artifacts. It does
// not start DHCP/DNS/TFTP/HTTP, access devices, or include credentials.
func GenerateBootstrap(infrastructure domain.Infrastructure, outputDirectory string) ([]Artifact, error) {
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
	files := make([]bootstrapPendingFile, 0, len(canonical.Sites)*6)
	artifacts := make([]Artifact, 0, len(canonical.Sites)*6)
	for _, site := range canonical.Sites {
		siteArtifactStart := len(artifacts)
		dhcp, err := buildBootstrapDHCP(site)
		if err != nil {
			return nil, err
		}
		dns, err := buildBootstrapDNS(site)
		if err != nil {
			return nil, err
		}
		tftp := buildBootstrapTFTP(site)
		pxe := buildBootstrapPXE(site)

		jsonFiles := []struct {
			name     string
			template string
			value    any
			typeName string
		}{
			{"dhcp/config.json", "templates/bootstrap/dhcp/config.json.tmpl", dhcp, "bootstrap_dhcp"},
			{"dns/config.json", "templates/bootstrap/dns/config.json.tmpl", dns, "bootstrap_dns"},
			{"tftp/config.json", "templates/bootstrap/tftp/config.json.tmpl", tftp, "bootstrap_tftp"},
			{"pxe/metadata.json", "templates/bootstrap/pxe/metadata.json.tmpl", pxe, "bootstrap_pxe"},
		}
		for _, item := range jsonFiles {
			data, err := renderBootstrapJSON(item.template, item.value)
			if err != nil {
				return nil, fmt.Errorf("render %s for site %q: %w", item.name, site.Name, err)
			}
			appendBootstrapArtifact(&files, &artifacts, site.Name, item.name, item.typeName, data, inputHash)
		}

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
				return nil, fmt.Errorf("render %s for site %q: %w", item.name, site.Name, err)
			}
			appendBootstrapArtifact(&files, &artifacts, site.Name, item.name, item.typeName, data, inputHash)
		}
		siteArtifacts := append([]Artifact(nil), artifacts[siteArtifactStart:]...)
		manifest, err := marshal(Manifest{
			GeneratorVersion: Version,
			TemplateVersion:  TemplateVersion,
			InputHash:        inputHash,
			Artifacts:        siteArtifacts,
		})
		if err != nil {
			return nil, fmt.Errorf("render bootstrap manifest for site %q: %w", site.Name, err)
		}
		files = append(files, bootstrapPendingFile{
			path: filepath.ToSlash(filepath.Join(site.Name, "manifest.json")),
			data: manifest,
		})
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
			return nil, fmt.Errorf("write bootstrap artifact %q: %w", file.path, err)
		}
	}
	return artifacts, nil
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
		if gateway != nil {
			reserved[ipToUint32(gateway)] = struct{}{}
		}
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
	return bootstrapDHCP{
		Version: 1, Service: "dhcp", Enabled: bootstrapServiceEnabled(site, "dhcp"), Site: site.Name,
		Network: site.Bootstrap.Network, Gateway: site.Bootstrap.Gateway, Pools: pools,
		ReservedAddresses: reservedAddresses, Reservations: reservations,
		Options: bootstrapDHCPOptions{
			DNS: []string{}, NextServer: "", NextServerSource: "agent_runtime", BootMode: "ipxe",
			BootFilename: "undionly.kpxe", TFTPDirectory: "tftp", IPXEScript: "pxe/ipxe/bootstrap.ipxe",
		},
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

func buildBootstrapTFTP(site domain.Site) bootstrapTFTP {
	return bootstrapTFTP{
		Version: 1, Service: "tftp", Enabled: bootstrapServiceEnabled(site, "tftp"), Site: site.Name,
		RootDirectory: "tftp", ReadOnly: true,
		AllowedFiles: []bootstrapTFTPFile{
			{Path: "undionly.kpxe", Source: "agent_runtime"},
			{Path: "ipxe.efi", Source: "agent_runtime"},
		},
	}
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
	return true
}

func bootstrapServiceEnabled(site domain.Site, service string) bool {
	enabled, exists := site.Services[service]
	if !exists {
		return true
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
