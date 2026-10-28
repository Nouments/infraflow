locals {
  infrastructure = {
    "environment" = var.environment
    "input_hash"  = "ac703f1ffdf7120e5e528d0107f23187cf6d37a1c8781e93375218c5f1ac71fd"
    "sites" = {
      "vendor-pilot" = {
        "bootstrap" = {
          "gateway" = "10.0.0.1"
          "network" = "10.0.0.0/24"
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
              "ipv4" = "192.0.2.10"
            }
            "model" = "isr-1100"
            "name"  = "CiscoR1"
            "provisioning" = {
              "method" = "ssh"
            }
            "role"   = "router"
            "vendor" = "cisco"
          },
          {
            "family" = "fortios"
            "id"     = ""
            "identity" = {
              "macs"   = []
              "serial" = ""
            }
            "management" = {
              "ipv4" = "203.0.113.10"
            }
            "model" = "unspecified"
            "name"  = "FortiGate1"
            "provisioning" = {
              "method" = "https"
            }
            "role"   = "firewall"
            "vendor" = "fortinet"
          },
          {
            "family" = "routeros"
            "id"     = ""
            "identity" = {
              "macs"   = []
              "serial" = ""
            }
            "management" = {
              "ipv4" = "198.51.100.10"
            }
            "model" = "RB750Gr3"
            "name"  = "MikroTikR1"
            "provisioning" = {
              "method" = "api"
            }
            "role"   = "router"
            "vendor" = "mikrotik"
          }
        ]
        "id"       = ""
        "links"    = []
        "mode"     = ""
        "name"     = "vendor-pilot"
        "services" = {}
      }
    }
  }
}
