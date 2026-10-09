package domain

import (
	"fmt"
	"regexp"
	"strings"
)

var sha256Pattern = regexp.MustCompile(`^[0-9a-fA-F]{64}$`)

// ProviderTemplate describes a provider-specific template without claiming
// that its adapter or execution path is implemented.
type ProviderTemplate struct {
	ID       string `json:"id" yaml:"id"`
	Vendor   string `json:"vendor" yaml:"vendor"`
	Family   string `json:"family" yaml:"family"`
	Model    string `json:"model" yaml:"model"`
	Version  string `json:"version" yaml:"version"`
	Hash     string `json:"hash" yaml:"hash"`
	Evidence string `json:"evidence" yaml:"evidence"`
}

// TemplateRegistry contains only metadata-backed template declarations.
type TemplateRegistry struct {
	Entries map[string]ProviderTemplate `json:"entries,omitempty" yaml:"entries,omitempty"`
}

// Resolve returns a template only when its identifier, version, context,
// hash, and provenance evidence are all complete and matching.
func (r TemplateRegistry) Resolve(device Device, version string) (ProviderTemplate, error) {
	requestedVersion := strings.TrimSpace(version)
	if requestedVersion == "" {
		return ProviderTemplate{}, fmt.Errorf("template resolution requires a non-empty version")
	}

	vendor := strings.ToLower(strings.TrimSpace(device.Vendor))
	family := strings.ToLower(strings.TrimSpace(device.Family))
	model := strings.ToLower(strings.TrimSpace(device.Model))
	if vendor == "" || family == "" || model == "" {
		return ProviderTemplate{}, fmt.Errorf("template resolution requires vendor, family, and model")
	}

	for _, template := range r.Entries {
		if !matchesTemplateContext(template, vendor, family, model) || template.Version != requestedVersion {
			continue
		}
		if err := validateProviderTemplate(template); err != nil {
			return ProviderTemplate{}, fmt.Errorf("invalid template %q: %w", template.ID, err)
		}
		return template, nil
	}

	return ProviderTemplate{}, fmt.Errorf("template has no verified metadata for %s/%s/%s version %s", vendor, family, model, requestedVersion)
}

func matchesTemplateContext(template ProviderTemplate, vendor, family, model string) bool {
	return strings.EqualFold(strings.TrimSpace(template.Vendor), vendor) &&
		strings.EqualFold(strings.TrimSpace(template.Family), family) &&
		strings.EqualFold(strings.TrimSpace(template.Model), model)
}

func validateProviderTemplate(template ProviderTemplate) error {
	if strings.TrimSpace(template.ID) == "" {
		return fmt.Errorf("missing template id")
	}
	if strings.TrimSpace(template.Vendor) == "" || strings.TrimSpace(template.Family) == "" || strings.TrimSpace(template.Model) == "" {
		return fmt.Errorf("missing vendor, family, or model")
	}
	if strings.TrimSpace(template.Version) == "" {
		return fmt.Errorf("missing version")
	}
	if !sha256Pattern.MatchString(strings.TrimSpace(template.Hash)) {
		return fmt.Errorf("hash must be a 64-character SHA-256 hexadecimal value")
	}
	if strings.TrimSpace(template.Evidence) == "" {
		return fmt.Errorf("missing provenance evidence")
	}
	return nil
}
