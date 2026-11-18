locals {
  infrastructure = {
    "environment" = var.environment
    "input_hash"  = "f155f062d2a2edee98375ac6b402d2a401107e5aee1c8bb10addf6573d94fb11"
    "sites" = {
      "site-a" = {
        "bootstrap" = {
          "gateway" = ""
          "network" = "192.0.2.0/24"
        }
        "devices" = [
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
            "name"  = "edge-a"
            "provisioning" = {
              "method" = ""
            }
            "role"   = "edge-router"
            "vendor" = "cisco"
          }
        ]
        "id"    = "site-a"
        "links" = []
        "mode"  = ""
        "name"  = "site-a"
        "services" = {
          "dhcp" = false
          "pxe"  = false
          "tftp" = false
        }
      }
    }
  }
}
