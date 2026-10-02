package planner

import (
	"sort"

	"infraflow/internal/domain"
)

func Build(infrastructure domain.Infrastructure) domain.Plan {
	plan := domain.Plan{Version: "v1", Status: "ready"}
	sites := append([]domain.Site(nil), infrastructure.Sites...)
	sort.Slice(sites, func(i, j int) bool { return sites[i].Name < sites[j].Name })

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
			reason := "no verified provisioning adapter is registered; device changes are unavailable"
			if device.Vendor == "" || device.Model == "" {
				reason = "vendor and model are required for capability lookup; device changes are unavailable"
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
