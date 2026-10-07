package domain

import "strings"

type CapabilityState string

const (
	CapabilityImplemented  CapabilityState = "IMPLEMENTED"
	CapabilityLabVerified  CapabilityState = "LAB-VERIFIED"
	CapabilityExperimental CapabilityState = "EXPERIMENTAL"
	CapabilityUnverified   CapabilityState = "UNVERIFIED"
	CapabilityUnsupported  CapabilityState = "UNSUPPORTED"
	CapabilityUnknown      CapabilityState = "UNKNOWN"
)

func (state CapabilityState) IsUsable() bool {
	return state == CapabilityImplemented || state == CapabilityLabVerified
}

type DeviceCapability struct {
	Method     string          `json:"method"`
	State      CapabilityState `json:"state"`
	Evidence   string          `json:"evidence,omitempty"`
	LastSeenAt string          `json:"last_seen_at,omitempty"`
}

type CapabilityRegistry struct {
	Entries map[string]map[string]DeviceCapability `json:"entries,omitempty"`
}

func (r CapabilityRegistry) Resolve(device Device) (CapabilityState, string) {
	if len(r.Entries) == 0 {
		return CapabilityUnknown, "capability registry is empty; no verified adapter evidence"
	}

	method := strings.TrimSpace(device.Provisioning.Method)
	if method == "" {
		method = "default"
	}
	key := strings.ToLower(strings.TrimSpace(device.Vendor) + ":" + strings.TrimSpace(device.Family) + ":" + strings.TrimSpace(device.Model))
	if key == ":" || strings.TrimSpace(device.Vendor) == "" || strings.TrimSpace(device.Model) == "" {
		return CapabilityUnknown, "vendor and model are required for capability lookup"
	}

	if methods, ok := r.Entries[key]; ok {
		if cap, ok := methods[method]; ok {
			return resolveCapability(cap)
		}
		if cap, ok := methods["*"]; ok {
			return resolveCapability(cap)
		}
	}
	return CapabilityUnknown, "capability registry has no verified entry for vendor/family/model and provisioning method"
}

func resolveCapability(cap DeviceCapability) (CapabilityState, string) {
	if cap.State.IsUsable() && strings.TrimSpace(cap.Evidence) == "" {
		return CapabilityUnknown, "usable capability requires recorded evidence of real verification"
	}
	return cap.State, cap.Evidence
}

type Infrastructure struct {
	CapabilityRegistry CapabilityRegistry `json:"capability_registry,omitempty"`
	TemplateRegistry   TemplateRegistry   `json:"template_registry,omitempty"`
	Sites              []Site             `json:"sites"`
}

type Site struct {
	ID        string          `yaml:"id,omitempty" json:"id,omitempty"`
	Name      string          `yaml:"name" json:"name"`
	Mode      string          `yaml:"mode,omitempty" json:"mode,omitempty"`
	Bootstrap Bootstrap       `yaml:"bootstrap,omitempty" json:"bootstrap,omitempty"`
	Services  map[string]bool `yaml:"services,omitempty" json:"services,omitempty"`
	Devices   []Device        `yaml:"devices,omitempty" json:"devices,omitempty"`
	Links     []Link          `yaml:"links,omitempty" json:"links,omitempty"`
}

type Bootstrap struct {
	Network string `yaml:"network,omitempty" json:"network,omitempty"`
	Gateway string `yaml:"gateway,omitempty" json:"gateway,omitempty"`
}

type Device struct {
	ID           string         `yaml:"id,omitempty" json:"id,omitempty"`
	Name         string         `yaml:"name" json:"name"`
	Role         string         `yaml:"role,omitempty" json:"role,omitempty"`
	Vendor       string         `yaml:"vendor,omitempty" json:"vendor,omitempty"`
	Family       string         `yaml:"family,omitempty" json:"family,omitempty"`
	Model        string         `yaml:"model,omitempty" json:"model,omitempty"`
	Identity     Identity       `yaml:"identity,omitempty" json:"identity,omitempty"`
	Management   Management     `yaml:"management,omitempty" json:"management,omitempty"`
	Provisioning Provisioning   `yaml:"provisioning,omitempty" json:"provisioning,omitempty"`
	Network      *DeviceNetwork `yaml:"network,omitempty" json:"network,omitempty"`
}

type Identity struct {
	Serial string   `yaml:"serial,omitempty" json:"serial,omitempty"`
	MACs   []string `yaml:"macs,omitempty" json:"macs,omitempty"`
}

type Management struct {
	IPv4 string `yaml:"ipv4,omitempty" json:"ipv4,omitempty"`
}

type Provisioning struct {
	Method          string `yaml:"method,omitempty" json:"method,omitempty"`
	TemplateVersion string `yaml:"template_version,omitempty" json:"template_version,omitempty"`
}

type DeviceNetwork struct {
	VDOM       string             `yaml:"vdom,omitempty" json:"vdom,omitempty"`
	Interfaces []NetworkInterface `yaml:"interfaces,omitempty" json:"interfaces,omitempty"`
	Routes     []StaticRoute      `yaml:"routes,omitempty" json:"routes,omitempty"`
	NAT        NATConfig          `yaml:"nat,omitempty" json:"nat,omitempty"`
}

type NetworkInterface struct {
	Name        string `yaml:"name" json:"name"`
	Role        string `yaml:"role" json:"role"`
	NATSide     string `yaml:"nat_side,omitempty" json:"nat_side,omitempty"`
	IPv4Mode    string `yaml:"ipv4_mode" json:"ipv4_mode"`
	IPv4Address string `yaml:"ipv4_address,omitempty" json:"ipv4_address,omitempty"`
}

type StaticRoute struct {
	Destination      string `yaml:"destination" json:"destination"`
	NextHop          string `yaml:"next_hop" json:"next_hop"`
	Interface        string `yaml:"interface,omitempty" json:"interface,omitempty"`
	Distance         int    `yaml:"distance,omitempty" json:"distance,omitempty"`
	FortinetSequence int    `yaml:"fortinet_sequence,omitempty" json:"fortinet_sequence,omitempty"`
}

type NATConfig struct {
	Source      []SourceNATRule      `yaml:"source,omitempty" json:"source,omitempty"`
	Destination []DestinationNATRule `yaml:"destination,omitempty" json:"destination,omitempty"`
}

type SourceNATRule struct {
	Name             string   `yaml:"name" json:"name"`
	SourceCIDR       string   `yaml:"source_cidr" json:"source_cidr"`
	IngressInterface string   `yaml:"ingress_interface" json:"ingress_interface"`
	EgressInterface  string   `yaml:"egress_interface" json:"egress_interface"`
	Mode             string   `yaml:"mode" json:"mode"`
	PoolName         string   `yaml:"pool_name,omitempty" json:"pool_name,omitempty"`
	PoolStart        string   `yaml:"pool_start,omitempty" json:"pool_start,omitempty"`
	PoolEnd          string   `yaml:"pool_end,omitempty" json:"pool_end,omitempty"`
	PoolMask         string   `yaml:"pool_mask,omitempty" json:"pool_mask,omitempty"`
	FortinetServices []string `yaml:"fortinet_services,omitempty" json:"fortinet_services,omitempty"`
	FortinetPolicyID int      `yaml:"fortinet_policy_id,omitempty" json:"fortinet_policy_id,omitempty"`
}

type DestinationNATRule struct {
	Name             string   `yaml:"name" json:"name"`
	IngressInterface string   `yaml:"ingress_interface" json:"ingress_interface"`
	EgressInterface  string   `yaml:"egress_interface" json:"egress_interface"`
	ExternalAddress  string   `yaml:"external_address" json:"external_address"`
	Protocol         string   `yaml:"protocol" json:"protocol"`
	ExternalPort     int      `yaml:"external_port" json:"external_port"`
	InternalAddress  string   `yaml:"internal_address" json:"internal_address"`
	InternalPort     int      `yaml:"internal_port" json:"internal_port"`
	SourceCIDR       string   `yaml:"source_cidr,omitempty" json:"source_cidr,omitempty"`
	AllowAnySource   bool     `yaml:"allow_any_source,omitempty" json:"allow_any_source,omitempty"`
	FortinetServices []string `yaml:"fortinet_services,omitempty" json:"fortinet_services,omitempty"`
	FortinetPolicyID int      `yaml:"fortinet_policy_id,omitempty" json:"fortinet_policy_id,omitempty"`
}

type Link struct {
	ID        string     `yaml:"id,omitempty" json:"id,omitempty"`
	A         string     `yaml:"a,omitempty" json:"a,omitempty"`
	B         string     `yaml:"b,omitempty" json:"b,omitempty"`
	Network   string     `yaml:"network,omitempty" json:"network,omitempty"`
	Endpoints []Endpoint `yaml:"endpoints,omitempty" json:"endpoints,omitempty"`
}

type Endpoint struct {
	Device    string `yaml:"device" json:"device"`
	Interface string `yaml:"interface" json:"interface"`
}
