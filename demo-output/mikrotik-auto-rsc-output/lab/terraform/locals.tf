locals {
  infrastructure = {
    "environment" = var.environment
    "input_hash"  = "325faf829441a6ca2bd4452c91e9fc9310e0e047d07b9fb48f03891fd402bfe9"
    "sites" = {
      "lab" = {
        "bootstrap" = {
          "gateway" = "198.51.100.254"
          "network" = "198.51.100.0/24"
        }
        "devices" = [
          {
            "family" = "routeros"
            "id"     = "r1"
            "identity" = {
              "macs"   = []
              "serial" = ""
            }
            "management" = {
              "ipv4" = ""
            }
            "model" = "RB750Gr3"
            "name"  = "edge-r1"
            "provisioning" = {
              "method" = "auto-rsc"
            }
            "role"   = ""
            "vendor" = "mikrotik"
          }
        ]
        "id"       = ""
        "links"    = []
        "mode"     = ""
        "name"     = "lab"
        "services" = {}
      }
    }
  }
}
