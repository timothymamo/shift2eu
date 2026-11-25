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

variable "public_read" {
  description = "Whether the bucket policy lets everyone (\"*\") read the site's objects, Hosting decision 11's fallback. Off by default: the Runbook applies it off, checks whether Edge Services serves the private objects (R5), and sets it true in terraform.tfvars only if it does not."
  type        = bool
  nullable    = false
  default     = false
}

variable "front_domains" {
  description = "Whether the site's container serves its two custom domains, www.shift2eu.com and shift2eu.com (ADR 0045). Off by default: Scaleway checks each name's DNS record before it issues the certificate, so the Runbook sets this true in terraform.tfvars only after R8's www CNAME and apex ALIAS point at the container's endpoint."
  type        = bool
  nullable    = false
  default     = false
}

variable "pipeline_www" {
  description = "Whether the Edge Services pipeline serves www.shift2eu.com itself, through its TLS stage and managed certificate: milestone 3's chain, before the container fronted the site (ADR 0045). Off by default, the end state, in which the pipeline answers only its default endpoint for the container. The Runbook's R8 rollback sets it true in terraform.tfvars and re-runs R4."
  type        = bool
  nullable    = false
  default     = false
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
