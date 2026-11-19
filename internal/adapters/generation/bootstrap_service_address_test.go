package generator

import (
	"net"
	"strings"
	"testing"

	"infraflow/internal/domain"
)

func autoInstallSite(serviceAddress string) domain.Site {
	return domain.Site{
		Name:      "ztp-lab",
		Bootstrap: domain.Bootstrap{Network: "192.0.2.0/24", ServiceAddress: serviceAddress},
		Services:  map[string]bool{"dhcp": true, "tftp": true},
		Devices: []domain.Device{{
			Name: "edge1", Role: "router", Vendor: "cisco", Family: "ios", Model: "7200", OSVersion: "15.3(3)XB12",
			Provisioning: domain.Provisioning{Method: domain.ProvisioningMethodCiscoAutoInstall},
			Identity:     domain.Identity{MACs: []string{"ca:01:00:00:00:01"}},
			Management:   domain.Management{IPv4: "192.0.2.10"},
		}},
	}
}

func TestBootstrapServiceAddressIsExcludedFromDHCPPools(t *testing.T) {
	config, err := buildBootstrapDHCP(autoInstallSite("192.0.2.2"))
	if err != nil {
		t.Fatal(err)
	}
	for _, pool := range config.Pools {
		if inRange("192.0.2.2", pool.Start, pool.End) || inRange("192.0.2.10", pool.Start, pool.End) {
			t.Fatalf("pool %s-%s contains a reserved address", pool.Start, pool.End)
		}
	}
	if _, err := buildBootstrapDHCP(autoInstallSite("192.0.2.10")); err == nil || !strings.Contains(err.Error(), "service_address") {
		t.Fatalf("service address conflicting with a device was accepted: %v", err)
	}
	if _, err := buildBootstrapDHCP(autoInstallSite("198.51.100.1")); err == nil {
		t.Fatal("service address outside the bootstrap network was accepted")
	}
}

func inRange(ip, start, end string) bool {
	value := ipToUint32(net.ParseIP(ip).To4())
	return value >= ipToUint32(net.ParseIP(start).To4()) && value <= ipToUint32(net.ParseIP(end).To4())
}
