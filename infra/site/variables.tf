# Inputs the maintainer gives on the admin machine (terraform.tfvars, git-ignored, or TF_VAR_...). IDs
# have no default, so no real account, project, user or application ID is ever committed.

variable "organization_id" {
  description = "The Scaleway organization that holds the site's project (a UUID)."
  type        = string
  nullable    = false

  validation {
    condition     = can(regex("^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$", var.organization_id))
    error_message = "organization_id must be a Scaleway organization ID, a lower-case UUID."
  }
}

variable "admin_principal" {
  description = "The bucket-policy principal OpenTofu runs as on the admin machine, the owner of the API key in SCW_ACCESS_KEY: \"user_id:<uuid>\" or \"application_id:<uuid>\". The bucket policy allows it every action on the site bucket, so an apply never locks the maintainer out of the bucket it manages."
  type        = string
  nullable    = false

  validation {
    condition     = can(regex("^(user_id|application_id):[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$", var.admin_principal))
    error_message = "admin_principal must be \"user_id:<uuid>\" or \"application_id:<uuid>\", a lower-case UUID: a bucket policy names a user or an application, and never \"*\" for the maintainer's access."
  }
}

variable "region" {
  description = "The Scaleway region of the site's bucket. Amsterdam by default, the region PRD #297 Hosting decision 9 priced."
  type        = string
  nullable    = false
  default     = "nl-ams"

  validation {
    condition     = contains(["fr-par", "nl-ams", "pl-waw"], var.region)
    error_message = "region must be a Scaleway Object Storage region: fr-par, nl-ams or pl-waw."
  }
}

variable "bucket_name" {
  description = "The site bucket's name. Bucket names are global across Scaleway: if this one is taken, choose another in terraform.tfvars."
  type        = string
  nullable    = false
  default     = "shift2eu-site-www"

  validation {
    condition     = can(regex("^[a-z0-9][a-z0-9-]{1,61}[a-z0-9]$", var.bucket_name))
    error_message = "bucket_name must be 3 to 63 lower-case letters, digits or hyphens, starting and ending with a letter or digit (no dot, so the bucket's TLS endpoints stay valid)."
  }
}
