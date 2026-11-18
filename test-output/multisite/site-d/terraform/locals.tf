locals {
  infrastructure = {
    "environment" = var.environment
    "input_hash"  = "0af3e584e80c0428b856c857464acfb03b463a247e8c4aa3ef5e5bab3f432d98"
    "sites" = {
      "site-d" = {
        "bootstrap" = {
          "gateway" = ""
          "network" = "192.0.2.0/24"
        }
        "devices" = [
          {
            "family" = "fortios"
            "id"     = ""
            "identity" = {
              "macs"   = []
              "serial" = ""
            }
            "management" = {
              "ipv4" = ""
            }
            "model" = ""
            "name"  = "edge-d-firewall"
            "provisioning" = {
              "method" = ""
            }
            "role"   = "firewall"
            "vendor" = "fortinet"
          },
          {
            "family" = "iosxe"
            "id"     = ""
            "identity" = {
              "macs"   = []
              "serial" = ""
            }
            "management" = {
              "ipv4" = ""
            }
            "model" = ""
            "name"  = "edge-d-router"
            "provisioning" = {
              "method" = ""
            }
            "role"   = "router"
            "vendor" = "cisco"
          }
        ]
        "id"    = "site-d"
        "links" = []
        "mode"  = ""
        "name"  = "site-d"
        "services" = {
          "dhcp" = false
          "pxe"  = false
          "tftp" = false
        }
      }
    }
  }
}
