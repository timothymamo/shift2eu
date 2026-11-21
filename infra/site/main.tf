# The site's Scaleway hosting (PRD #297 milestone 3, issue #422, ADR 0039): one project, its bucket, the
# deploy application and Edge Services' pipeline for www.shift2eu.com. The deploy API key, the DNS records
# and the GitHub `site` environment are made by hand (the PRD's Runbook): no secret reaches the state.
#
# Object Storage's API is scoped by project, so every child of the bucket, and every Edge Services
# resource, names the site's project itself; the provider's default project is never used.

# The project that holds only the site's bucket (and the pipeline serving it). Object Storage cannot be
# named in an IAM rule, so this project is the deploy key's boundary (Hosting decision 5).
resource "scaleway_account_project" "site" {
  name        = "shift2eu-site"
  description = "www.shift2eu.com: the site's bucket and its Edge Services pipeline only (infra/site)."
}

# --- Object Storage ---------------------------------------------------------------------------------------

# Private by default: no acl (the attribute is deprecated, and a public ACL would make every object
# public outside the bucket policy). Versioning lets the maintainer roll back an overwritten page.
# force_destroy stays unset, so a destroy never deletes a bucket that still holds the site.
resource "scaleway_object_bucket" "site" {
  name       = var.bucket_name
  project_id = scaleway_account_project.site.id
  region     = var.region

  versioning {
    enabled = true
  }
}

# Bucket website: Edge Services' backend reads the bucket in website mode, so / and every directory serve
# their index.html. No error document: the site ships no error page, and a missing path answers the
# endpoint's own error.
resource "scaleway_object_bucket_website_configuration" "site" {
  bucket     = scaleway_object_bucket.site.name
  project_id = scaleway_account_project.site.id
  region     = var.region

  index_document {
    suffix = "index.html"
  }
}

# The bucket's only read and write rule (Hosting decisions 5 and 11). Version 2023-04-17 allows only what
# it lists. A bucket policy limits even its owner, so the first statement keeps the maintainer's own
# principal (never "*") in full control of the bucket OpenTofu manages. The deploy application may write
# and read objects and list the bucket, nothing else. Edge Services has no bucket-policy principal (a
# principal is a user_id, an application_id or "*"), so the site's objects are public read (decision 11's
# fallback).
#
# It is applied after the website configuration: enabling bucket website may apply Scaleway's default
# policy, and this one must be the one that stays.
resource "scaleway_object_bucket_policy" "site" {
  bucket     = scaleway_object_bucket.site.name
  project_id = scaleway_account_project.site.id
  region     = var.region

  policy = jsonencode({
    Version = "2023-04-17"
    Id      = "shift2eu-site"
    Statement = [
      {
        Sid       = "AdminManagesTheBucket"
        Effect    = "Allow"
        Principal = { SCW = var.admin_principal }
        Action    = ["s3:*"]
        Resource  = [scaleway_object_bucket.site.name, "${scaleway_object_bucket.site.name}/*"]
      },
      {
        Sid       = "DeployWritesAndReadsObjects"
        Effect    = "Allow"
        Principal = { SCW = "application_id:${scaleway_iam_application.deploy.id}" }
        Action    = ["s3:PutObject", "s3:GetObject"]
        Resource  = ["${scaleway_object_bucket.site.name}/*"]
      },
      {
        Sid       = "DeployListsTheBucket"
        Effect    = "Allow"
        Principal = { SCW = "application_id:${scaleway_iam_application.deploy.id}" }
        Action    = ["s3:ListBucket"]
        Resource  = [scaleway_object_bucket.site.name]
      },
      {
        Sid       = "EveryoneReadsTheSite"
        Effect    = "Allow"
        Principal = "*"
        Action    = ["s3:GetObject"]
        Resource  = ["${scaleway_object_bucket.site.name}/*"]
      },
    ]
  })

  depends_on = [scaleway_object_bucket_website_configuration.site]
}

# --- IAM: the deploy application -------------------------------------------------------------------------

# The release's deploy job uploads as this application. Its API key is created by hand (Runbook), so the
# secret never reaches the state; dev/infra-check.sh refuses a scaleway_iam_api_key anywhere in infra/.
resource "scaleway_iam_application" "deploy" {
  name        = "shift2eu-site-deploy"
  description = "The release's site deploy: writes and reads objects in the shift2eu-site project only (infra/site)."
}

# One rule, two permission sets, one project: no bucket configuration, no delete, no other project, no
# organization scope. The bucket policy narrows it further to the site bucket's objects.
resource "scaleway_iam_policy" "deploy" {
  name           = "shift2eu-site-deploy"
  description    = "Object writes and reads in the shift2eu-site project only (infra/site)."
  application_id = scaleway_iam_application.deploy.id

  rule {
    project_ids          = [scaleway_account_project.site.id]
    permission_set_names = ["ObjectStorageObjectsWrite", "ObjectStorageObjectsRead"]
  }
}

# --- Edge Services ----------------------------------------------------------------------------------------

# The Starter plan (Hosting decision 9): one pipeline, 100 GB of cache egress a month, managed
# certificates. Subscribed in the site's project, which holds the pipeline. The deploy key's permission
# sets hold no Edge Services permission, so the pipeline being in its project gives it nothing.
resource "scaleway_edge_services_plan" "site" {
  name       = "starter"
  project_id = scaleway_account_project.site.id
}

resource "scaleway_edge_services_pipeline" "site" {
  name        = "shift2eu-site"
  description = "www.shift2eu.com from the site bucket (infra/site)."
  project_id  = scaleway_account_project.site.id

  depends_on = [scaleway_edge_services_plan.site]
}

# The chain a request follows: DNS (www.shift2eu.com) -> TLS (managed Let's Encrypt certificate) -> cache
# -> backend (the site bucket in website mode). No WAF or route stage: one backend serves every path.
resource "scaleway_edge_services_backend_stage" "site" {
  pipeline_id = scaleway_edge_services_pipeline.site.id
  project_id  = scaleway_account_project.site.id

  s3_backend_config {
    bucket_name   = scaleway_object_bucket.site.name
    bucket_region = scaleway_object_bucket.site.region
    is_website    = true
  }

  depends_on = [scaleway_object_bucket_website_configuration.site]
}

# fallback_ttl applies only to a response without Cache-Control: the deploy sets Cache-Control on every
# object, and an object it missed is cached no longer than the HTML's max-age (300 seconds). No cookies:
# the site sets none, and a response to a request with cookies is never cached for others.
resource "scaleway_edge_services_cache_stage" "site" {
  pipeline_id      = scaleway_edge_services_pipeline.site.id
  project_id       = scaleway_account_project.site.id
  backend_stage_id = scaleway_edge_services_backend_stage.site.id
  fallback_ttl     = 300
  include_cookies  = false
}

resource "scaleway_edge_services_tls_stage" "site" {
  pipeline_id         = scaleway_edge_services_pipeline.site.id
  project_id          = scaleway_account_project.site.id
  cache_stage_id      = scaleway_edge_services_cache_stage.site.id
  managed_certificate = true
}

# Edge Services serves only a subdomain (Hosting decision 1): www, never the apex.
resource "scaleway_edge_services_dns_stage" "site" {
  pipeline_id  = scaleway_edge_services_pipeline.site.id
  project_id   = scaleway_account_project.site.id
  tls_stage_id = scaleway_edge_services_tls_stage.site.id
  fqdns        = ["www.shift2eu.com"]
}

# The DNS stage is where the pipeline's traffic enters.
resource "scaleway_edge_services_head_stage" "site" {
  pipeline_id   = scaleway_edge_services_pipeline.site.id
  head_stage_id = scaleway_edge_services_dns_stage.site.id
}
