locals {
  infrastructure = {
    "environment" = var.environment
    "input_hash"  = "76200265cec3b6e89dd0e91d14834619bb39f6dfb09c00d36a67b91d1f4a6760"
    "sites" = {
      "site-c" = {
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
            "name"  = "edge-c"
            "provisioning" = {
              "method" = ""
            }
            "role"   = "edge-firewall"
            "vendor" = "fortinet"
          }
        ]
        "id"    = "site-c"
        "links" = []
        "mode"  = ""
        "name"  = "site-c"
        "services" = {
          "dhcp" = false
          "pxe"  = false
          "tftp" = false
        }
      }
    }
  }
}
