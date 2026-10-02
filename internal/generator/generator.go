package generator

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"infraflow/internal/config"
	"infraflow/internal/domain"
	"infraflow/internal/safefs"
	"infraflow/pkg/protocol"
)

const Version = "0.1.0"
const TemplateVersion = "1"

type Artifact = protocol.Artifact

type Manifest struct {
	GeneratorVersion string     `json:"generator_version"`
	TemplateVersion  string     `json:"template_version"`
	InputHash        string     `json:"input_hash"`
	Artifacts        []Artifact `json:"artifacts"`
}

type inventory struct {
	Site    string          `json:"site"`
	Devices []domain.Device `json:"devices"`
}

type topology struct {
	Site  string         `json:"site"`
	Nodes []topologyNode `json:"nodes"`
	Edges []topologyEdge `json:"edges"`
}

type topologyNode struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Role         string `json:"role,omitempty"`
	Vendor       string `json:"vendor,omitempty"`
	Model        string `json:"model,omitempty"`
	ManagementIP string `json:"management_ip,omitempty"`
}

type topologyEdge struct {
	ID      string       `json:"id,omitempty"`
	A       topologyPort `json:"a"`
	B       topologyPort `json:"b"`
	Network string       `json:"network"`
}

type topologyPort struct {
	Device    string `json:"device"`
	Interface string `json:"interface"`
}

type pendingFile struct {
	path string
	data []byte
}

func Generate(infrastructure domain.Infrastructure, outputDirectory string) ([]Artifact, error) {
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
	files := make([]pendingFile, 0, len(canonical.Sites)*3)
	allArtifacts := make([]Artifact, 0, len(canonical.Sites)*2)

	for _, site := range canonical.Sites {
		inventoryBytes, err := marshal(inventory{Site: site.Name, Devices: site.Devices})
		if err != nil {
			return nil, err
		}
		topologyBytes, err := marshal(buildTopology(site))
		if err != nil {
			return nil, err
		}

		inventoryPath := filepath.ToSlash(filepath.Join(site.Name, "inventory.json"))
		topologyPath := filepath.ToSlash(filepath.Join(site.Name, "topology.json"))
		siteArtifacts := []Artifact{
			{Type: "inventory", Path: inventoryPath, InputHash: inputHash, OutputHash: protocol.SHA256(inventoryBytes)},
			{Type: "topology", Path: topologyPath, InputHash: inputHash, OutputHash: protocol.SHA256(topologyBytes)},
		}
		manifestBytes, err := marshal(Manifest{
			GeneratorVersion: Version,
			TemplateVersion:  TemplateVersion,
			InputHash:        inputHash,
			Artifacts:        siteArtifacts,
		})
		if err != nil {
			return nil, err
		}

		files = append(files,
			pendingFile{path: inventoryPath, data: inventoryBytes},
			pendingFile{path: topologyPath, data: topologyBytes},
			pendingFile{path: filepath.ToSlash(filepath.Join(site.Name, "manifest.json")), data: manifestBytes},
		)
		allArtifacts = append(allArtifacts, siteArtifacts...)
	}

	root, err := filepath.Abs(outputDirectory)
	if err != nil {
		return nil, fmt.Errorf("resolve output directory: %w", err)
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, fmt.Errorf("create output directory: %w", err)
	}
	for _, site := range canonical.Sites {
		sitePath := filepath.Join(root, site.Name)
		if info, err := os.Lstat(sitePath); err == nil && info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("refusing to write through symlink %q", sitePath)
		} else if err != nil && !os.IsNotExist(err) {
			return nil, fmt.Errorf("inspect output path %q: %w", sitePath, err)
		}
		if err := os.MkdirAll(sitePath, 0o755); err != nil {
			return nil, fmt.Errorf("create site output directory: %w", err)
		}
	}
	for _, file := range files {
		if err := safefs.AtomicWrite(root, file.path, file.data, 0o644); err != nil {
			return nil, fmt.Errorf("write artifact %q: %w", file.path, err)
		}
	}
	return allArtifacts, nil
}

func canonicalize(infrastructure domain.Infrastructure) domain.Infrastructure {
	result := domain.Infrastructure{Sites: append([]domain.Site(nil), infrastructure.Sites...)}
	sort.Slice(result.Sites, func(i, j int) bool { return result.Sites[i].Name < result.Sites[j].Name })
	for siteIndex := range result.Sites {
		site := &result.Sites[siteIndex]
		site.Devices = append([]domain.Device(nil), site.Devices...)
		sort.Slice(site.Devices, func(i, j int) bool { return site.Devices[i].Name < site.Devices[j].Name })
		for deviceIndex := range site.Devices {
			device := &site.Devices[deviceIndex]
			device.Identity.MACs = append([]string(nil), device.Identity.MACs...)
			sort.Strings(device.Identity.MACs)
		}
		site.Links = append([]domain.Link(nil), site.Links...)
		sort.Slice(site.Links, func(i, j int) bool {
			if site.Links[i].ID != site.Links[j].ID {
				return site.Links[i].ID < site.Links[j].ID
			}
			if site.Links[i].A != site.Links[j].A {
				return site.Links[i].A < site.Links[j].A
			}
			if site.Links[i].B != site.Links[j].B {
				return site.Links[i].B < site.Links[j].B
			}
			return site.Links[i].Network < site.Links[j].Network
		})
	}
	return result
}

func buildTopology(site domain.Site) topology {
	result := topology{Site: site.Name}
	for _, device := range site.Devices {
		id := device.ID
		if id == "" {
			id = device.Name
		}
		result.Nodes = append(result.Nodes, topologyNode{
			ID: id, Name: device.Name, Role: device.Role, Vendor: device.Vendor,
			Model: device.Model, ManagementIP: device.Management.IPv4,
		})
	}
	for _, link := range site.Links {
		result.Edges = append(result.Edges, topologyEdge{
			ID: link.ID, A: parsePort(link.A), B: parsePort(link.B), Network: link.Network,
		})
	}
	return result
}

func parsePort(value string) topologyPort {
	device, iface, _ := strings.Cut(value, ":")
	return topologyPort{Device: device, Interface: iface}
}

func marshal(value any) ([]byte, error) {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode artifact: %w", err)
	}
	return append(data, '\n'), nil
}
