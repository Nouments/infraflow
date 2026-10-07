package domain

import (
	"fmt"
	"strings"
)

type CapabilityState string

type LifecycleStateName string

type Provenance string

const (
	CapabilityImplemented  CapabilityState = "IMPLEMENTED"
	CapabilityLabVerified  CapabilityState = "LAB-VERIFIED"
	CapabilityExperimental CapabilityState = "EXPERIMENTAL"
	CapabilityUnverified   CapabilityState = "UNVERIFIED"
	CapabilityUnsupported  CapabilityState = "UNSUPPORTED"
	CapabilityUnknown      CapabilityState = "UNKNOWN"
)

const (
	StateDesired   LifecycleStateName = "DESIRED"
	StatePlanned   LifecycleStateName = "PLANNED"
	StateGenerated LifecycleStateName = "GENERATED"
	StateExecuted  LifecycleStateName = "EXECUTED"
	StateVerified  LifecycleStateName = "VERIFIED"
	StateObserved  LifecycleStateName = "OBSERVED"
)

const (
	ProvenanceDesired  Provenance = "DESIRED"
	ProvenanceObserved Provenance = "OBSERVED"
	ProvenanceInferred Provenance = "INFERRED"
)

func (state CapabilityState) IsUsable() bool {
	return state == CapabilityImplemented || state == CapabilityLabVerified
}

type Observation struct {
	Source     string     `json:"source,omitempty"`
	Provenance Provenance `json:"provenance"`
	Value      string     `json:"value,omitempty"`
	Evidence   string     `json:"evidence,omitempty"`
}

type LifecycleState struct {
	Desired         bool         `json:"desired"`
	Planned         bool         `json:"planned"`
	Generated       bool         `json:"generated"`
	Executed        bool         `json:"executed"`
	Verified        bool         `json:"verified"`
	Observed        bool         `json:"observed"`
	LastObservation *Observation `json:"last_observation,omitempty"`
}

func NewLifecycleState() LifecycleState {
	return LifecycleState{Desired: true}
}

func (s *LifecycleState) HasObserved() bool {
	return s != nil && s.Observed && s.LastObservation != nil && s.LastObservation.Provenance == ProvenanceObserved
}

func (s *LifecycleState) Transition(next LifecycleStateName) error {
	if s == nil {
		return nil
	}

	switch next {
	case StatePlanned:
		if s.Planned {
			return nil
		}
		if !s.Desired {
			return fmt.Errorf("planned state requires desired state to exist")
		}
		s.Planned = true
		return nil
	case StateGenerated:
		if s.Generated {
			return nil
		}
		if !s.Planned {
			return fmt.Errorf("generated state requires prior planned state")
		}
		s.Generated = true
		return nil
	case StateExecuted:
		if s.Executed {
			return nil
		}
		if !s.Generated {
			return fmt.Errorf("executed state requires prior generated state")
		}
		s.Executed = true
		return nil
	case StateVerified:
		if s.Verified {
			return nil
		}
		if !s.Executed {
			return fmt.Errorf("verified state requires prior executed state")
		}
		s.Verified = true
		return nil
	case StateObserved:
		if s.Observed {
			return nil
		}
		s.Observed = true
		return nil
	default:
		return fmt.Errorf("unsupported lifecycle state %q", next)
	}
}

func (s *LifecycleState) RecordObservation(value, evidence string, provenance Provenance, source string) (*Observation, error) {
	if s == nil {
		return nil, fmt.Errorf("lifecycle state is nil")
	}

	if provenance == "" {
		return nil, fmt.Errorf("observation provenance is required")
	}

	obs := &Observation{Source: source, Provenance: provenance, Value: value, Evidence: evidence}
	if provenance == ProvenanceInferred {
		s.LastObservation = obs
		return obs, nil
	}
	if provenance == ProvenanceObserved {
		s.Observed = true
		s.LastObservation = obs
		return obs, nil
	}
	if provenance == ProvenanceDesired {
		s.LastObservation = obs
		return obs, nil
	}

	return nil, fmt.Errorf("unsupported observation provenance %q", provenance)
}

type DeviceCapability struct {
	Method     string          `json:"method"`
	State      CapabilityState `json:"state"`
	Evidence   string          `json:"evidence,omitempty"`
	LastSeenAt string          `json:"last_seen_at,omitempty"`
}

// VendorProfile describes the known vendor/family configuration surface without
// claiming lab-proven execution. The status remains UNVERIFIED until a real device
// test has been recorded.
type VendorProfile struct {
	Vendor        string            `json:"vendor"`
	Family        string            `json:"family"`
	Model         string            `json:"model,omitempty"`
	DefaultMethod string            `json:"default_method"`
	Methods       map[string]string `json:"methods,omitempty"`
	Status        CapabilityState   `json:"status"`
	Evidence      string            `json:"evidence"`
}

func VendorProfileFor(device Device) (VendorProfile, bool) {
	vendor := strings.ToLower(strings.TrimSpace(device.Vendor))
	family := strings.ToLower(strings.TrimSpace(device.Family))
	model := strings.TrimSpace(device.Model)

	switch {
	case vendor == "cisco" && (family == "iosxe" || family == "ios-xe" || family == "ios_xe"):
		return VendorProfile{
			Vendor:        "cisco",
			Family:        "iosxe",
			Model:         model,
			DefaultMethod: "netconf",
			Methods: map[string]string{
				"netconf": "iosxe NETCONF management path",
				"ssh":     "IOS XE CLI management path",
			},
			Status:   CapabilityUnverified,
			Evidence: "Vendor family recognized; no real Cisco IOS XE device execution or observed state recorded yet.",
		}, true
	case vendor == "mikrotik" && family == "routeros":
		return VendorProfile{
			Vendor:        "mikrotik",
			Family:        "routeros",
			Model:         model,
			DefaultMethod: "api",
			Methods: map[string]string{
				"api": "RouterOS API via community.routeros",
				"ssh": "RouterOS SSH/CLI management path",
			},
			Status:   CapabilityUnverified,
			Evidence: "Vendor family recognized; no real MikroTik RouterOS lab verification or observed state recorded yet.",
		}, true
	case (vendor == "fortinet" || vendor == "fortigate") && (family == "fortios" || family == "fortigate"):
		return VendorProfile{
			Vendor:        "fortinet",
			Family:        "fortios",
			Model:         model,
			DefaultMethod: "https",
			Methods: map[string]string{
				"https": "FortiGate HTTPS API path",
				"ssh":   "FortiOS SSH/CLI management path",
			},
			Status:   CapabilityUnverified,
			Evidence: "Vendor family recognized; no real FortiGate/FortiOS lab verification or observed state recorded yet.",
		}, true
	default:
		return VendorProfile{}, false
	}
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
