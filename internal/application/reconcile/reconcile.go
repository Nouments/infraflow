// Package reconcile compares desired and observed JSON-compatible state.
// It never mutates either input and does not perform any external action.
package reconcile

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"infraflow/pkg/protocol"
)

const maxStateBytes = 2 << 20

type DifferenceKind string

const (
	DifferenceChanged DifferenceKind = "changed"
	DifferenceMissing DifferenceKind = "missing"
	DifferenceExtra   DifferenceKind = "extra"
)

type Difference struct {
	Path string         `json:"path"`
	Kind DifferenceKind `json:"kind"`
}

type Report struct {
	Drift        bool         `json:"drift"`
	DesiredHash  string       `json:"desired_hash"`
	ObservedHash string       `json:"observed_hash"`
	Differences  []Difference `json:"differences,omitempty"`
}

func Compare(desired, observed any) (Report, error) {
	desiredValue, desiredBytes, err := normalize(desired)
	if err != nil {
		return Report{}, fmt.Errorf("normalize desired state: %w", err)
	}
	observedValue, observedBytes, err := normalize(observed)
	if err != nil {
		return Report{}, fmt.Errorf("normalize observed state: %w", err)
	}
	report := Report{
		DesiredHash:  protocol.SHA256(desiredBytes),
		ObservedHash: protocol.SHA256(observedBytes),
	}
	differences := make([]Difference, 0)
	compareValues(desiredValue, observedValue, "$", &differences)
	report.Differences = differences
	report.Drift = len(differences) > 0
	return report, nil
}

func normalize(value any) (any, []byte, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, nil, err
	}
	if len(encoded) > maxStateBytes {
		return nil, nil, fmt.Errorf("state exceeds %d bytes", maxStateBytes)
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.UseNumber()
	var normalized any
	if err := decoder.Decode(&normalized); err != nil {
		return nil, nil, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return nil, nil, fmt.Errorf("state contains trailing JSON")
	}
	return normalized, encoded, nil
}

func compareValues(desired, observed any, path string, differences *[]Difference) {
	if desiredMap, ok := desired.(map[string]any); ok {
		if observedMap, ok := observed.(map[string]any); ok {
			keys := make([]string, 0, len(desiredMap)+len(observedMap))
			seen := make(map[string]struct{}, len(desiredMap)+len(observedMap))
			for key := range desiredMap {
				seen[key] = struct{}{}
				keys = append(keys, key)
			}
			for key := range observedMap {
				if _, exists := seen[key]; !exists {
					keys = append(keys, key)
				}
			}
			sort.Strings(keys)
			for _, key := range keys {
				desiredEntry, desiredExists := desiredMap[key]
				observedEntry, observedExists := observedMap[key]
				switch {
				case !desiredExists:
					*differences = append(*differences, Difference{Path: childPath(path, key), Kind: DifferenceExtra})
				case !observedExists:
					*differences = append(*differences, Difference{Path: childPath(path, key), Kind: DifferenceMissing})
				default:
					compareValues(desiredEntry, observedEntry, childPath(path, key), differences)
				}
			}
			return
		}
		*differences = append(*differences, Difference{Path: path, Kind: DifferenceChanged})
		return
	}
	if desiredList, ok := desired.([]any); ok {
		if observedList, ok := observed.([]any); ok {
			limit := len(desiredList)
			if len(observedList) > limit {
				limit = len(observedList)
			}
			for index := 0; index < limit; index++ {
				itemPath := path + "[" + strconv.Itoa(index) + "]"
				switch {
				case index >= len(desiredList):
					*differences = append(*differences, Difference{Path: itemPath, Kind: DifferenceExtra})
				case index >= len(observedList):
					*differences = append(*differences, Difference{Path: itemPath, Kind: DifferenceMissing})
				default:
					compareValues(desiredList[index], observedList[index], itemPath, differences)
				}
			}
			return
		}
		*differences = append(*differences, Difference{Path: path, Kind: DifferenceChanged})
		return
	}
	if !reflect.DeepEqual(desired, observed) {
		*differences = append(*differences, Difference{Path: path, Kind: DifferenceChanged})
	}
}

func childPath(parent, key string) string {
	if isSimpleKey(key) {
		return parent + "." + key
	}
	encoded, _ := json.Marshal(key)
	return parent + "[" + string(encoded) + "]"
}

func isSimpleKey(value string) bool {
	if value == "" {
		return false
	}
	for _, character := range value {
		if !(character == '_' || character == '-' || character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9') {
			return false
		}
	}
	return !strings.HasPrefix(value, "[")
}
