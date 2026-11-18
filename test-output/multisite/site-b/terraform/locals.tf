locals {
  infrastructure = {
    "environment" = var.environment
    "input_hash"  = "058e72f07609ed5ccfcb3f68dbc5f0112822a92458e5d8bbd47a15ace277a084"
    "sites" = {
      "site-b" = {
        "bootstrap" = {
          "gateway" = ""
          "network" = "192.0.2.0/24"
        }
        "devices" = [
          {
            "family" = "routeros"
            "id"     = ""
            "identity" = {
              "macs"   = []
              "serial" = ""
            }
            "management" = {
              "ipv4" = ""
            }
            "model" = ""
            "name"  = "edge-b"
            "provisioning" = {
              "method" = ""
            }
            "role"   = "edge-router"
            "vendor" = "mikrotik"
          }
        ]
        "id"    = "site-b"
        "links" = []
        "mode"  = ""
        "name"  = "site-b"
        "services" = {
          "dhcp" = false
          "pxe"  = false
          "tftp" = false
        }
      }
    }
  }
}
