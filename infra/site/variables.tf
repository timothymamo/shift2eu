# Inputs the maintainer gives on the admin machine (terraform.tfvars, git-ignored, or TF_VAR_...). IDs
# have no default, so no real account or project ID is ever committed.

variable "organization_id" {
  description = "The Scaleway organization that holds the site's project (a UUID)."
  type        = string
  nullable    = false

  validation {
    condition     = can(regex("^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$", var.organization_id))
    error_message = "organization_id must be a Scaleway organization ID, a lower-case UUID."
  }
}
