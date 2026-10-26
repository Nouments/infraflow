package catalog

import (
	"embed"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

//go:embed data/*.yaml resources/README.md
var catalogFiles embed.FS

type Record struct {
	ID               string     `yaml:"id" json:"id"`
	Manufacturer     string     `yaml:"manufacturer" json:"manufacturer"`
	Product          string     `yaml:"product" json:"product"`
	Model            string     `yaml:"model" json:"model"`
	Category         string     `yaml:"category" json:"category"`
	OperatingSystems []OS       `yaml:"operating_systems" json:"operating_systems"`
	Interfaces       []string   `yaml:"interfaces" json:"interfaces"`
	InterfacesState  string     `yaml:"interfaces_state" json:"interfaces_state"`
	Features         []string   `yaml:"features" json:"features"`
	FeaturesState    string     `yaml:"features_state" json:"features_state"`
	TemplateIDs      []string   `yaml:"template_ids" json:"template_ids"`
	Documentation    []Resource `yaml:"documentation" json:"documentation"`
	Images           []Resource `yaml:"images" json:"images"`
	Source           string     `yaml:"source" json:"source"`
	CheckedAt        string     `yaml:"checked_at" json:"checked_at"`
	ValidationStatus string     `yaml:"validation_status" json:"validation_status"`
	ValidationNotes  string     `yaml:"validation_notes" json:"validation_notes"`
}

type OS struct {
	Name               string   `yaml:"name" json:"name"`
	Versions           []string `yaml:"versions" json:"versions"`
	CompatibilityState string   `yaml:"compatibility_state" json:"compatibility_state"`
}

type Resource struct {
	Title  string `yaml:"title" json:"title"`
	URL    string `yaml:"url" json:"url"`
	Local  string `yaml:"local" json:"local"`
	Status string `yaml:"status" json:"status"`
}

type Catalog struct {
	records []Record
}

func Load() (*Catalog, error) {
	paths, err := fs.Glob(catalogFiles, "data/*.yaml")
	if err != nil {
		return nil, fmt.Errorf("list embedded hardware catalog: %w", err)
	}
	sort.Strings(paths)
	catalog := &Catalog{records: make([]Record, 0, len(paths))}
	seenIDs := make(map[string]struct{}, len(paths))
	for _, path := range paths {
		data, err := catalogFiles.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read hardware record %q: %w", path, err)
		}
		decoder := yaml.NewDecoder(strings.NewReader(string(data)))
		decoder.KnownFields(true)
		var record Record
		if err := decoder.Decode(&record); err != nil {
			return nil, fmt.Errorf("decode hardware record %q: %w", path, err)
		}
		if err := validate(record); err != nil {
			return nil, fmt.Errorf("validate hardware record %q: %w", path, err)
		}
		normalizedID := strings.ToLower(record.ID)
		if _, exists := seenIDs[normalizedID]; exists {
			return nil, fmt.Errorf("duplicate hardware catalog id %q", record.ID)
		}
		seenIDs[normalizedID] = struct{}{}
		if err := validateResourceFiles(catalogFiles, record); err != nil {
			return nil, fmt.Errorf("validate resources for hardware record %q: %w", path, err)
		}
		catalog.records = append(catalog.records, record)
	}
	return catalog, nil
}

func (catalog *Catalog) FindByID(id string) (Record, bool) {
	for _, record := range catalog.records {
		if strings.EqualFold(record.ID, strings.TrimSpace(id)) {
			return clone(record), true
		}
	}
	return Record{}, false
}

func (catalog *Catalog) Search(manufacturer, model string) []Record {
	var matches []Record
	for _, record := range catalog.records {
		if manufacturer != "" && !strings.EqualFold(record.Manufacturer, strings.TrimSpace(manufacturer)) {
			continue
		}
		if model != "" && !strings.EqualFold(record.Model, strings.TrimSpace(model)) {
			continue
		}
		matches = append(matches, clone(record))
	}
	return matches
}

func validate(record Record) error {
	if strings.TrimSpace(record.ID) == "" || strings.TrimSpace(record.Manufacturer) == "" ||
		strings.TrimSpace(record.Product) == "" || strings.TrimSpace(record.Model) == "" {
		return fmt.Errorf("id, manufacturer, product, and model are required")
	}
	if record.Category != "router" && record.Category != "switch" && record.Category != "firewall" && record.Category != "other" {
		return fmt.Errorf("unsupported category %q", record.Category)
	}
	if record.InterfacesState != "verified" && record.InterfacesState != "unknown" && record.InterfacesState != "unverified" {
		return fmt.Errorf("unsupported interfaces_state %q", record.InterfacesState)
	}
	if record.FeaturesState != "verified" && record.FeaturesState != "unknown" && record.FeaturesState != "unverified" {
		return fmt.Errorf("unsupported features_state %q", record.FeaturesState)
	}
	if record.Source == "" || record.CheckedAt == "" || record.ValidationStatus == "" || record.ValidationNotes == "" {
		return fmt.Errorf("source, checked_at, validation_status, and validation_notes are required")
	}
	if _, err := time.Parse("2006-01-02", record.CheckedAt); err != nil {
		return fmt.Errorf("checked_at must use YYYY-MM-DD")
	}
	if record.ValidationStatus != "verified" && record.ValidationStatus != "partial" &&
		record.ValidationStatus != "unverified" && record.ValidationStatus != "unknown" {
		return fmt.Errorf("unsupported validation_status %q", record.ValidationStatus)
	}
	for _, os := range record.OperatingSystems {
		if os.Name == "" || os.CompatibilityState == "" {
			return fmt.Errorf("operating system name and compatibility_state are required")
		}
		if os.CompatibilityState != "verified" && os.CompatibilityState != "unknown" && os.CompatibilityState != "unverified" {
			return fmt.Errorf("unsupported OS compatibility state %q", os.CompatibilityState)
		}
	}
	for _, resource := range append(append([]Resource(nil), record.Documentation...), record.Images...) {
		if resource.Title == "" || resource.Status == "" {
			return fmt.Errorf("resource title and status are required")
		}
		if resource.URL == "" && resource.Local == "" {
			return fmt.Errorf("resource %q requires an official URL or local path", resource.Title)
		}
		if resource.Local != "" && resource.Status != "available_locally" {
			return fmt.Errorf("local resource %q must be marked available_locally", resource.Title)
		}
		if resource.Local == "" && resource.Status == "available_locally" {
			return fmt.Errorf("resource %q is marked local but has no local path", resource.Title)
		}
	}
	return nil
}

func validateResourceFiles(files fs.FS, record Record) error {
	resources, err := fs.Sub(files, "resources")
	if err != nil {
		return fmt.Errorf("open embedded resource directory: %w", err)
	}
	for _, resource := range append(append([]Resource(nil), record.Documentation...), record.Images...) {
		if resource.Local == "" {
			continue
		}
		clean := path.Clean(resource.Local)
		if clean != resource.Local || clean == "." || strings.HasPrefix(clean, "../") || path.IsAbs(clean) {
			return fmt.Errorf("resource %q has an unsafe local path", resource.Title)
		}
		info, err := fs.Stat(resources, clean)
		if err != nil {
			return fmt.Errorf("local resource %q is missing: %w", resource.Local, err)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("local resource %q is not a regular file", resource.Local)
		}
	}
	return nil
}

func clone(record Record) Record {
	record.OperatingSystems = append([]OS(nil), record.OperatingSystems...)
	for index := range record.OperatingSystems {
		record.OperatingSystems[index].Versions = append([]string(nil), record.OperatingSystems[index].Versions...)
	}
	record.Interfaces = append([]string(nil), record.Interfaces...)
	record.Features = append([]string(nil), record.Features...)
	record.TemplateIDs = append([]string(nil), record.TemplateIDs...)
	record.Documentation = append([]Resource(nil), record.Documentation...)
	record.Images = append([]Resource(nil), record.Images...)
	return record
}
