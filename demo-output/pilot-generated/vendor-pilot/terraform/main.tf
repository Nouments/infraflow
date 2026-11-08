# This configuration is intentionally data-only.
# It does not declare provider resources and does not provision devices.
locals {
  site_names = sort(keys(local.infrastructure.sites))
}
