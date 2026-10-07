// Package planner contains the application-level plan builder. It only
// depends on domain types and has no delivery or infrastructure concerns.
package planner

import (
	"sort"
	"strings"

	"infraflow/internal/domain"
)

func Build(infrastructure domain.Infrastructure) domain.Plan {
	plan := domain.Plan{ID: "plan", Version: "v1", Status: "PLANNED"}
	sites := append([]domain.Site(nil), infrastructure.Sites...)
	sort.Slice(sites, func(i, j int) bool { return sites[i].Name < sites[j].Name })

	planIDParts := make([]string, 0, len(sites))
	for _, site := range sites {
		planIDParts = append(planIDParts, site.Name)
		devices := append([]domain.Device(nil), site.Devices...)
		sort.Slice(devices, func(i, j int) bool { return devices[i].Name < devices[j].Name })
		for _, device := range devices {
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
				method = "unknown"
			}
			taskID := "plan/" + site.Name + "/" + device.Name + "/configure_interfaces"
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
	if len(planIDParts) > 0 {
		plan.ID = "plan/" + strings.Join(planIDParts, "/")
		plan.Site = planIDParts[0]
	}
	return plan
}
