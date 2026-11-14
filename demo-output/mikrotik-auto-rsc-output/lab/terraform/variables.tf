variable "environment" {
  type        = string
  description = "Logical environment label for this generated declaration."
  default     = "generated"

  validation {
    condition     = trimspace(var.environment) != ""
    error_message = "environment must not be empty."
  }
}
