// Package planner contains the application-level plan builder. It only
// depends on domain types and has no delivery or infrastructure concerns.
package planner

import (
	"fmt"
	"sort"
	"strings"

	"infraflow/internal/domain"
)

func Build(infrastructure domain.Infrastructure) domain.Plan {
	plan := domain.Plan{Version: "v1", Status: "ready"}
	sites := append([]domain.Site(nil), infrastructure.Sites...)
	sort.Slice(sites, func(i, j int) bool { return sites[i].Name < sites[j].Name })

	registry := infrastructure.CapabilityRegistry
	templateRegistry := infrastructure.TemplateRegistry
	for _, site := range sites {
		inventoryID := "inventory/" + site.Name
		topologyID := "topology/" + site.Name
		plan.Tasks = append(plan.Tasks,
			domain.Task{ID: inventoryID, Site: site.Name, Action: "generate_inventory", Status: "ready"},
			domain.Task{ID: topologyID, Site: site.Name, Action: "generate_topology", Status: "ready"},
		)

		devices := append([]domain.Device(nil), site.Devices...)
		sort.Slice(devices, func(i, j int) bool { return devices[i].Name < devices[j].Name })
		for _, device := range devices {
			reason := "capability registry is UNKNOWN; no verified provisioning adapter is registered; device changes are unavailable"
			if device.Vendor == "" || device.Model == "" {
				reason = "vendor and model are required for capability lookup; device changes are unavailable"
			} else {
				state, evidence := registry.Resolve(device)
				if state.IsUsable() {
					reason = fmt.Sprintf("capability %s is usable for %s/%s/%s; evidence: %s", state, strings.TrimSpace(device.Vendor), strings.TrimSpace(device.Family), strings.TrimSpace(device.Model), evidence)
				} else {
					reason = fmt.Sprintf("capability registry reports %s for %s/%s/%s; device changes are unavailable. %s", state, strings.TrimSpace(device.Vendor), strings.TrimSpace(device.Family), strings.TrimSpace(device.Model), evidence)
				}
				if strings.TrimSpace(device.Provisioning.Method) != "" {
					reason = fmt.Sprintf("capability registry reports %s for provisioning method %q on %s/%s/%s; device changes are unavailable. %s",
						state, device.Provisioning.Method, strings.TrimSpace(device.Vendor), strings.TrimSpace(device.Family), strings.TrimSpace(device.Model), evidence)
				}

				if template, err := templateRegistry.Resolve(device, strings.TrimSpace(device.Provisioning.TemplateVersion)); err == nil {
					templateReason := fmt.Sprintf("template %s version %s is known for %s/%s/%s; hash %s; provenance: %s. execution remains blocked until the capability is LAB-VERIFIED with evidence",
						template.ID, template.Version, strings.TrimSpace(device.Vendor), strings.TrimSpace(device.Family), strings.TrimSpace(device.Model), template.Hash, template.Evidence)
					reason = reason + "; " + templateReason
				} else if strings.TrimSpace(device.Provisioning.TemplateVersion) != "" {
					reason = reason + "; template resolution failed for " + strings.TrimSpace(device.Vendor) + "/" + strings.TrimSpace(device.Family) + "/" + strings.TrimSpace(device.Model) + " version " + strings.TrimSpace(device.Provisioning.TemplateVersion) + ": " + err.Error()
				}
			}
			plan.Tasks = append(plan.Tasks, domain.Task{
				ID:           "provision/" + site.Name + "/" + device.Name,
				Site:         site.Name,
				Target:       device.Name,
				Action:       "provision_device",
				Status:       "blocked",
				Reason:       reason,
				Dependencies: []string{inventoryID, topologyID},
			})
			plan.Status = "blocked"
		}
	}
	return plan
}
