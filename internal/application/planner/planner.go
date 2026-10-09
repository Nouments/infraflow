// Package planner contains the application-level plan builder. It only
// depends on domain types and has no delivery or infrastructure concerns.
package planner

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/url"
	"sort"
	"strings"

	"infraflow/internal/domain"
)

func Build(infrastructure domain.Infrastructure) domain.Plan {
	lifecycle := domain.NewLifecycleState()
	if err := lifecycle.Transition(domain.StatePlanned); err != nil {
		panic(err)
	}
	sites := canonicalSites(infrastructure.Sites)
	canonicalIntent, _ := json.Marshal(struct {
		Sites []domain.Site `json:"sites,omitempty"`
	}{Sites: sites})
	intentHash := sha256.Sum256(canonicalIntent)
	plan := domain.Plan{
		ID:        "plan/sha256-" + hex.EncodeToString(intentHash[:]),
		Version:   "v1",
		Status:    "PLANNED",
		Lifecycle: lifecycle,
	}
	if len(sites) > 0 {
		plan.Site = sites[0].Name
	}

	for _, site := range sites {
		for _, device := range site.Devices {
			if device.Network == nil || len(device.Network.Interfaces) == 0 {
				continue
			}
			method := strings.TrimSpace(device.Provisioning.Method)
			if method == "" {
				if profile, ok := domain.VendorProfileFor(device); ok {
					method = profile.DefaultMethod
				}
			}
			if method == "" {
				method = ""
			}
			taskID := "plan/" + url.PathEscape(site.Name) + "/" + url.PathEscape(device.Name) + "/configure_interfaces"
			plan.Tasks = append(plan.Tasks, domain.Task{
				ID:     taskID,
				Site:   site.Name,
				Target: device.Name,
				Action: "configure_interfaces",
				Method: method,
				Status: "PLANNED",
			})
		}
	}
	return plan
}

func canonicalSites(input []domain.Site) []domain.Site {
	sites := make([]domain.Site, len(input))
	for index, source := range input {
		site := source
		site.ID = ""
		site.Devices = make([]domain.Device, len(source.Devices))
		for deviceIndex, sourceDevice := range source.Devices {
			device := sourceDevice
			device.ID = ""
			device.Identity.MACs = append([]string(nil), sourceDevice.Identity.MACs...)
			sort.Strings(device.Identity.MACs)
			if sourceDevice.Network != nil {
				network := *sourceDevice.Network
				network.Interfaces = append([]domain.NetworkInterface(nil), sourceDevice.Network.Interfaces...)
				sortByCanonicalJSON(network.Interfaces)
				network.Routes = append([]domain.StaticRoute(nil), sourceDevice.Network.Routes...)
				sortByCanonicalJSON(network.Routes)
				network.NAT.Source = append([]domain.SourceNATRule(nil), sourceDevice.Network.NAT.Source...)
				for ruleIndex := range network.NAT.Source {
					network.NAT.Source[ruleIndex].FortinetServices = sortedStrings(network.NAT.Source[ruleIndex].FortinetServices)
				}
				sortByCanonicalJSON(network.NAT.Source)
				network.NAT.Destination = append([]domain.DestinationNATRule(nil), sourceDevice.Network.NAT.Destination...)
				for ruleIndex := range network.NAT.Destination {
					network.NAT.Destination[ruleIndex].FortinetServices = sortedStrings(network.NAT.Destination[ruleIndex].FortinetServices)
				}
				sortByCanonicalJSON(network.NAT.Destination)
				device.Network = &network
			}
			site.Devices[deviceIndex] = device
		}
		sortByCanonicalJSON(site.Devices)
		site.Links = append([]domain.Link(nil), source.Links...)
		for linkIndex := range site.Links {
			site.Links[linkIndex].ID = ""
			site.Links[linkIndex].Endpoints = append([]domain.Endpoint(nil), source.Links[linkIndex].Endpoints...)
			sortByCanonicalJSON(site.Links[linkIndex].Endpoints)
		}
		sortByCanonicalJSON(site.Links)
		sites[index] = site
	}
	sortByCanonicalJSON(sites)
	return sites
}

func sortedStrings(input []string) []string {
	result := append([]string(nil), input...)
	sort.Strings(result)
	return result
}

func sortByCanonicalJSON[T any](values []T) {
	sort.Slice(values, func(i, j int) bool {
		left, _ := json.Marshal(values[i])
		right, _ := json.Marshal(values[j])
		return string(left) < string(right)
	})
}
