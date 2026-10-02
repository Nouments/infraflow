package reconcile

import (
	"strings"
	"testing"

	"infraflow/pkg/protocol"
)

func TestCompareReportsDeterministicDesiredObservedDrift(t *testing.T) {
	desired := map[string]any{
		"hostname":   "R2",
		"management": map[string]any{"ipv4": "10.0.0.2"},
		"interfaces": []any{"ether1", "ether2"},
	}
	observed := map[string]any{
		"hostname":   "R2",
		"management": map[string]any{"ipv4": "10.0.0.3"},
		"interfaces": []any{"ether1", "ether3", "ether4"},
		"unexpected": true,
	}
	report, err := Compare(desired, observed)
	if err != nil {
		t.Fatal(err)
	}
	if !report.Drift || !protocol.IsSHA256(report.DesiredHash) || !protocol.IsSHA256(report.ObservedHash) {
		t.Fatalf("unexpected report: %#v", report)
	}
	want := []Difference{
		{Path: "$.interfaces[1]", Kind: DifferenceChanged},
		{Path: "$.interfaces[2]", Kind: DifferenceExtra},
		{Path: "$.management.ipv4", Kind: DifferenceChanged},
		{Path: "$.unexpected", Kind: DifferenceExtra},
	}
	if len(report.Differences) != len(want) {
		t.Fatalf("unexpected differences: %#v", report.Differences)
	}
	for index := range want {
		if report.Differences[index] != want[index] {
			t.Fatalf("difference %d: got %#v want %#v", index, report.Differences[index], want[index])
		}
	}
}

func TestCompareEqualStateHasNoDriftAndStableHashes(t *testing.T) {
	first, err := Compare(map[string]any{"b": 2, "a": 1}, map[string]any{"a": 1, "b": 2})
	if err != nil {
		t.Fatal(err)
	}
	second, err := Compare(map[string]any{"a": 1, "b": 2}, map[string]any{"b": 2, "a": 1})
	if err != nil {
		t.Fatal(err)
	}
	if first.Drift || len(first.Differences) != 0 || first.DesiredHash != second.DesiredHash || first.ObservedHash != second.ObservedHash {
		t.Fatalf("equal state was not deterministic: %#v %#v", first, second)
	}
}

func TestCompareRejectsUnsupportedOrOversizedState(t *testing.T) {
	if _, err := Compare(make(chan int), nil); err == nil {
		t.Fatal("expected unsupported state to be rejected")
	}
	if _, err := Compare(map[string]string{"payload": strings.Repeat("x", maxStateBytes)}, nil); err == nil {
		t.Fatal("expected oversized state to be rejected")
	}
}

func TestCompareEscapesComplexObjectKeys(t *testing.T) {
	report, err := Compare(map[string]string{"a.b": "desired"}, map[string]string{"a.b": "observed"})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Differences) != 1 || report.Differences[0].Path != `$["a.b"]` {
		t.Fatalf("unexpected escaped path: %#v", report.Differences)
	}
}
