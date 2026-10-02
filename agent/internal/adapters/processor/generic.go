package processor

import (
	"encoding/json"
	"io"
	"strings"

	"infraflow/pkg/protocol"
)

type Generic struct{}

type inventoryDocument struct {
	Site    string `json:"site"`
	Devices []struct {
		Name string `json:"name"`
	} `json:"devices"`
}

type topologyDocument struct {
	Site  string `json:"site"`
	Nodes []struct {
		Name string `json:"name"`
	} `json:"nodes"`
	Edges []struct {
		A struct {
			Device string `json:"device"`
		} `json:"a"`
		B struct {
			Device string `json:"device"`
		} `json:"b"`
	} `json:"edges"`
}

func (Generic) Process(artifact protocol.Artifact, data io.Reader) protocol.ArtifactResult {
	result := protocol.ArtifactResult{Path: artifact.Path, OutputHash: artifact.OutputHash, Status: protocol.StatusFailed}
	site, _, _ := strings.Cut(artifact.Path, "/")
	switch artifact.Type {
	case "inventory":
		var inventory inventoryDocument
		if err := json.NewDecoder(data).Decode(&inventory); err != nil || inventory.Site != site {
			result.Message = "inventory artifact is invalid"
			return result
		}
		seenDevices := make(map[string]struct{}, len(inventory.Devices))
		for _, device := range inventory.Devices {
			if strings.TrimSpace(device.Name) == "" {
				result.Message = "inventory contains a device without a name"
				return result
			}
			if _, exists := seenDevices[device.Name]; exists {
				result.Message = "inventory contains duplicate device names"
				return result
			}
			seenDevices[device.Name] = struct{}{}
		}
		result.Status = protocol.StatusCompleted
		result.Message = "inventory imported into local agent state"
	case "topology":
		var topology topologyDocument
		if err := json.NewDecoder(data).Decode(&topology); err != nil || topology.Site != site {
			result.Message = "topology artifact is invalid"
			return result
		}
		devices := make(map[string]struct{}, len(topology.Nodes))
		for _, node := range topology.Nodes {
			if strings.TrimSpace(node.Name) == "" {
				result.Message = "topology contains a node without a name"
				return result
			}
			devices[node.Name] = struct{}{}
		}
		for _, edge := range topology.Edges {
			if _, exists := devices[edge.A.Device]; !exists {
				result.Message = "topology edge references an unknown device"
				return result
			}
			if _, exists := devices[edge.B.Device]; !exists {
				result.Message = "topology edge references an unknown device"
				return result
			}
		}
		result.Status = protocol.StatusCompleted
		result.Message = "topology imported into local agent state"
	default:
		result.Status = protocol.StatusBlocked
		result.Message = "agent has no executor for this artifact type"
	}
	return result
}
