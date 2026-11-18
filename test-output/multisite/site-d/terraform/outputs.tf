output "infraflow_input_hash" {
  description = "SHA-256 of the normalized InfraFlow input."
  value       = local.infrastructure.input_hash
}

output "infraflow_site_names" {
  description = "Sites present in the normalized InfraFlow input."
  value       = local.site_names
}

output "infraflow_declaration" {
  description = "The normalized desired state represented as Terraform data."
  value       = local.infrastructure
}
