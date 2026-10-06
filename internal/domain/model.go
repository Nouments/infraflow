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
			return cap.State, cap.Evidence
		}
		if cap, ok := methods["*"]; ok {
			return cap.State, cap.Evidence
		}
	}
	return CapabilityUnknown, "capability registry has no verified entry for vendor/family/model and provisioning method"
}

type Infrastructure struct {
	CapabilityRegistry CapabilityRegistry `json:"capability_registry,omitempty"`
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
	ID           string       `yaml:"id,omitempty" json:"id,omitempty"`
	Name         string       `yaml:"name" json:"name"`
	Role         string       `yaml:"role,omitempty" json:"role,omitempty"`
	Vendor       string       `yaml:"vendor,omitempty" json:"vendor,omitempty"`
	Family       string       `yaml:"family,omitempty" json:"family,omitempty"`
	Model        string       `yaml:"model,omitempty" json:"model,omitempty"`
	Identity     Identity     `yaml:"identity,omitempty" json:"identity,omitempty"`
	Management   Management   `yaml:"management,omitempty" json:"management,omitempty"`
	Provisioning Provisioning `yaml:"provisioning,omitempty" json:"provisioning,omitempty"`
}

type Identity struct {
	Serial string   `yaml:"serial,omitempty" json:"serial,omitempty"`
	MACs   []string `yaml:"macs,omitempty" json:"macs,omitempty"`
}

type Management struct {
	IPv4 string `yaml:"ipv4,omitempty" json:"ipv4,omitempty"`
}

type Provisioning struct {
	Method string `yaml:"method,omitempty" json:"method,omitempty"`
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
