package executors

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"

	"infraflow/internal/domain"
	"infraflow/pkg/protocol"
)

var (
	ErrTargetNotFound            = errors.New("target device is not authorized or not found")
	ErrTargetVendorUnsupported   = errors.New("target device vendor is not supported by Cisco IOS executor")
	ErrTargetInvalidManagementIP = errors.New("target device has an invalid management IP address")
)

// AuthorizedDevice represents an authorized network target with validated connection parameters.
type AuthorizedDevice struct {
	Site         string `json:"site"`
	DeviceID     string `json:"device_id"`
	Name         string `json:"name"`
	Vendor       string `json:"vendor"`
	Family       string `json:"family,omitempty"`
	Model        string `json:"model,omitempty"`
	ManagementIP string `json:"management_ip"`
	SSHPort      int    `json:"ssh_port"`
}

// DeviceTargetResolver defines the contract for resolving and authorizing targets.
// It ensures that execution tasks cannot connect to arbitrary network endpoints.
type DeviceTargetResolver interface {
	ResolveDevice(ctx context.Context, site, deviceID string) (AuthorizedDevice, error)
}

// StaticDeviceResolver provides targets from an explicit in-memory list.
type StaticDeviceResolver struct {
	devices map[string]AuthorizedDevice
}

// NewStaticDeviceResolver creates a static device resolver.
func NewStaticDeviceResolver(devices []AuthorizedDevice) (*StaticDeviceResolver, error) {
	m := make(map[string]AuthorizedDevice, len(devices))
	for _, dev := range devices {
		validated, err := validateAuthorizedDevice(dev)
		if err != nil {
			return nil, err
		}
		key := strings.ToLower(validated.Site + "/" + validated.DeviceID)
		m[key] = validated
	}
	return &StaticDeviceResolver{devices: m}, nil
}

// ResolveDevice looks up a device by site and device ID.
func (r *StaticDeviceResolver) ResolveDevice(ctx context.Context, site, deviceID string) (AuthorizedDevice, error) {
	if ctx != nil && ctx.Err() != nil {
		return AuthorizedDevice{}, ctx.Err()
	}
	key := strings.ToLower(strings.TrimSpace(site) + "/" + strings.TrimSpace(deviceID))
	dev, exists := r.devices[key]
	if !exists {
		return AuthorizedDevice{}, fmt.Errorf("%w: %s/%s", ErrTargetNotFound, site, deviceID)
	}
	return dev, nil
}

// ArtifactInventoryDeviceResolver resolves authorized devices by inspecting published <site>/inventory.json
// artifacts in the provider's artifact directory.
type ArtifactInventoryDeviceResolver struct {
	artifactDir string
}

// NewArtifactInventoryDeviceResolver creates an inventory resolver based on the artifact storage directory.
func NewArtifactInventoryDeviceResolver(artifactDir string) *ArtifactInventoryDeviceResolver {
	return &ArtifactInventoryDeviceResolver{artifactDir: artifactDir}
}

// ResolveDevice reads <site>/inventory.json to check if the target exists and is authorized.
func (r *ArtifactInventoryDeviceResolver) ResolveDevice(ctx context.Context, site, deviceID string) (AuthorizedDevice, error) {
	if ctx != nil && ctx.Err() != nil {
		return AuthorizedDevice{}, ctx.Err()
	}
	cleanSite := strings.TrimSpace(site)
	cleanDevice := strings.TrimSpace(deviceID)
	if !protocol.ValidSiteName(cleanSite) || !protocol.ValidSiteName(cleanDevice) {
		return AuthorizedDevice{}, fmt.Errorf("%w: site or device identifier is invalid", ErrTargetNotFound)
	}

	inventoryPath := filepath.Join(r.artifactDir, cleanSite, "inventory.json")
	data, err := os.ReadFile(inventoryPath)
	if err != nil {
		if os.IsNotExist(err) {
			return AuthorizedDevice{}, fmt.Errorf("%w: inventory for site %q not published", ErrTargetNotFound, cleanSite)
		}
		return AuthorizedDevice{}, fmt.Errorf("read site inventory: %w", err)
	}

	var payload struct {
		Site    string          `json:"site"`
		Devices []domain.Device `json:"devices"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return AuthorizedDevice{}, fmt.Errorf("decode site inventory: %w", err)
	}
	if !strings.EqualFold(strings.TrimSpace(payload.Site), cleanSite) || len(payload.Devices) == 0 {
		return AuthorizedDevice{}, fmt.Errorf("site inventory is inconsistent or contains no devices")
	}

	aliases := make(map[string]int, len(payload.Devices)*2)
	var matched *domain.Device
	for deviceIndex, d := range payload.Devices {
		id := strings.TrimSpace(d.ID)
		name := strings.TrimSpace(d.Name)
		if id == "" {
			id = name
		}
		if !protocol.ValidSiteName(id) || !protocol.ValidSiteName(name) {
			return AuthorizedDevice{}, fmt.Errorf("site inventory contains an invalid device identity")
		}
		for _, alias := range []string{id, name} {
			key := strings.ToLower(alias)
			if previous, exists := aliases[key]; exists && previous != deviceIndex {
				return AuthorizedDevice{}, fmt.Errorf("site inventory contains duplicate device identities")
			}
			aliases[key] = deviceIndex
		}
		if strings.EqualFold(id, cleanDevice) || strings.EqualFold(name, cleanDevice) {
			if matched != nil {
				return AuthorizedDevice{}, fmt.Errorf("site inventory contains ambiguous device identities")
			}
			device := d
			matched = &device
		}
	}
	if matched != nil {
		dev := AuthorizedDevice{
			Site:         cleanSite,
			DeviceID:     cleanDevice,
			Name:         strings.TrimSpace(matched.Name),
			Vendor:       matched.Vendor,
			Family:       matched.Family,
			Model:        matched.Model,
			ManagementIP: matched.Management.IPv4,
			SSHPort:      22,
		}
		return validateAuthorizedDevice(dev)
	}
	return AuthorizedDevice{}, fmt.Errorf("%w: %s/%s", ErrTargetNotFound, cleanSite, cleanDevice)
}

// CompositeDeviceResolver checks a series of resolvers in order until a target is resolved.
type CompositeDeviceResolver struct {
	resolvers []DeviceTargetResolver
}

// NewCompositeDeviceResolver builds a composite resolver.
func NewCompositeDeviceResolver(resolvers ...DeviceTargetResolver) *CompositeDeviceResolver {
	return &CompositeDeviceResolver{resolvers: resolvers}
}

// ResolveDevice queries each resolver sequentially.
func (c *CompositeDeviceResolver) ResolveDevice(ctx context.Context, site, deviceID string) (AuthorizedDevice, error) {
	for _, resolver := range c.resolvers {
		if resolver == nil {
			continue
		}
		dev, err := resolver.ResolveDevice(ctx, site, deviceID)
		if err == nil {
			return dev, nil
		}
		if !errors.Is(err, ErrTargetNotFound) {
			return AuthorizedDevice{}, err
		}
	}
	return AuthorizedDevice{}, fmt.Errorf("%w: %s/%s", ErrTargetNotFound, site, deviceID)
}

func validateAuthorizedDevice(dev AuthorizedDevice) (AuthorizedDevice, error) {
	dev.Site = strings.TrimSpace(dev.Site)
	dev.DeviceID = strings.TrimSpace(dev.DeviceID)
	dev.Name = strings.TrimSpace(dev.Name)
	dev.Vendor = strings.ToLower(strings.TrimSpace(dev.Vendor))
	dev.Family = strings.ToLower(strings.TrimSpace(dev.Family))
	dev.Model = strings.TrimSpace(dev.Model)
	dev.ManagementIP = strings.TrimSpace(dev.ManagementIP)

	if dev.Site == "" || dev.DeviceID == "" {
		return AuthorizedDevice{}, fmt.Errorf("%w: site and device_id are required", ErrTargetNotFound)
	}
	if dev.Vendor != "cisco" {
		return AuthorizedDevice{}, fmt.Errorf("%w: expected cisco, got %q", ErrTargetVendorUnsupported, dev.Vendor)
	}
	ip := net.ParseIP(dev.ManagementIP)
	if ip == nil || ip.To4() == nil || ip.IsUnspecified() || ip.IsLoopback() || ip.IsMulticast() || ip.Equal(net.IPv4bcast) {
		return AuthorizedDevice{}, fmt.Errorf("%w: %q", ErrTargetInvalidManagementIP, dev.ManagementIP)
	}
	if dev.SSHPort == 0 {
		dev.SSHPort = 22
	}
	if dev.SSHPort < 1 || dev.SSHPort > 65535 {
		return AuthorizedDevice{}, fmt.Errorf("invalid SSH port %d", dev.SSHPort)
	}
	return dev, nil
}
