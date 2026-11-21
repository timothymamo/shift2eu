# The OpenTofu and provider versions, pinned exactly (issue #422, rule 7): Renovate bumps them, the
# provider's hashes in .terraform.lock.hcl with it. required_version equals infra/tofu.yaml's tofuVersion,
# the release dev/tofu.sh runs (dev/infra-check.sh fails when they differ).
terraform {
  required_version = "1.13.9"

  required_providers {
    scaleway = {
      source  = "scaleway/scaleway"
      version = "2.93.0"
    }
  }

  # The state lives in a private, versioned Scaleway bucket outside the site's project, never in git and
  # never local (dev/infra-check.sh refuses any other backend). Partial configuration: only settings that
  # hold no name, ID or secret are here. The bucket, region and endpoint come from a backend file the
  # maintainer keeps out of git (backend.hcl.example), and the credentials from the maintainer's own
  # environment on the admin machine:
  #   dev/tofu.sh -chdir=infra/site init -backend-config=backend.hcl
  # make infra-check runs init -backend=false, which reads none of this and contacts no bucket.
  backend "s3" {
    key = "site/terraform.tfstate"

    # State locking with a lock object written with If-None-Match, which Scaleway Object Storage supports
    # since 2026-05-26 (its changelog: "Conditional writes and Terraform state locking are now supported").
    # No DynamoDB table: Scaleway has none. ADR 0039.
    use_lockfile = true

    # Scaleway's S3 API is not AWS: no STS, no account ID, no AWS region list, no AWS checksums.
    skip_credentials_validation = true
    skip_region_validation      = true
    skip_requesting_account_id  = true
    skip_s3_checksum            = true
  }
}
