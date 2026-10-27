package catalog

import (
	"strings"
	"testing"
	"testing/fstest"
)

func TestLoadAndSearchEmbeddedHardwareCatalog(t *testing.T) {
	loaded, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	record, ok := loaded.FindByID("CISCO.ISR-1100")
	if !ok {
		t.Fatal("catalog lookup by stable id failed")
	}
	if record.Model != "isr-1100" || record.Category != "router" {
		t.Fatalf("unexpected catalog record: %#v", record)
	}
	matches := loaded.Search("cisco", "ISR-1100")
	if len(matches) != 1 || matches[0].ID != record.ID {
		t.Fatalf("model search returned %#v", matches)
	}
	if record.ValidationStatus != "partial" || record.Documentation[0].Status != "reference_only" || record.Documentation[0].Local != "" {
		t.Fatalf("catalog overstates local or verification status: %#v", record)
	}
}

func TestValidateRejectsUnavailableLocalDatasheetClaim(t *testing.T) {
	record := Record{
		ID: "test.router", Manufacturer: "Test", Product: "Router", Model: "test",
		Category: "router", Source: "source", CheckedAt: "2026-10-26",
		ValidationStatus: "partial", ValidationNotes: "test record",
		Documentation: []Resource{{Title: "datasheet", URL: "https://example.invalid", Status: "available_locally"}},
	}
	if err := validate(record); err == nil {
		t.Fatal("expected local datasheet without a local path to be rejected")
	}
}

func TestValidateResourceFilesRejectsMissingLocalDatasheet(t *testing.T) {
	record := Record{
		Documentation: []Resource{{Title: "datasheet", Local: "datasheets/router.pdf", Status: "available_locally"}},
	}
	err := validateResourceFiles(fstest.MapFS{
		"resources/README.md": &fstest.MapFile{Data: []byte("no local datasheets")},
	}, record)
	if err == nil || !strings.Contains(err.Error(), "local resource") {
		t.Fatal("expected absent local datasheet to be rejected")
	}
}

func TestValidateResourceFilesRequiresNonEmptyReadableFile(t *testing.T) {
	record := Record{
		Documentation: []Resource{{Title: "datasheet", Local: "datasheets/router.pdf", Status: "available_locally"}},
	}
	for _, test := range []struct {
		name string
		data []byte
		want string
	}{
		{name: "empty", data: nil, want: "empty"},
		{name: "readable", data: []byte("%PDF-1.7 local fixture"), want: ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			files := fstest.MapFS{
				"resources/datasheets/router.pdf": &fstest.MapFile{Data: test.data},
				"resources/README.md":             &fstest.MapFile{Data: []byte("resource fixture")},
			}
			err := validateResourceFiles(files, record)
			if test.want == "" && err != nil {
				t.Fatalf("readable non-empty resource was rejected: %v", err)
			}
			if test.want != "" && (err == nil || !strings.Contains(err.Error(), test.want)) {
				t.Fatalf("expected error containing %q, got %v", test.want, err)
			}
		})
	}
}
