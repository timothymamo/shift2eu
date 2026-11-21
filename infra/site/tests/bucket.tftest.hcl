# The site's bucket (issue #422, PRD #297 "Hosting decisions" 5 and 11): it lives in the project created for
# it alone, keeps every version, and serves index.html as the website's index document. The bucket's read
# rule is the bucket policy's (bucket_policy.tftest.hcl), and that no ACL decides it either is pinned where
# the configuration text is read: dev/infra-check.sh refuses any scaleway_object_bucket_acl and any `acl`
# argument, even "private" (ADR 0039). The deprecated acl attribute is deliberately not read here: mocked,
# it could only repeat what the configuration did not set, never what the API would.
#
# `mock_provider` replaces the Scaleway provider, so `tofu test` needs no credential and contacts nothing.
# The mock's computed ids must satisfy the provider's own validators (UUIDs), hence the fixed values below.

mock_provider "scaleway" {
  mock_resource "scaleway_account_project" {
    defaults = {
      id = "11111111-1111-1111-1111-111111111111"
    }
  }
  mock_resource "scaleway_iam_application" {
    defaults = {
      id = "22222222-2222-2222-2222-222222222222"
    }
  }
  mock_resource "scaleway_edge_services_pipeline" {
    defaults = {
      id = "33333333-3333-3333-3333-333333333333"
    }
  }
  mock_resource "scaleway_edge_services_backend_stage" {
    defaults = {
      id = "44444444-4444-4444-4444-444444444444"
    }
  }
  mock_resource "scaleway_edge_services_cache_stage" {
    defaults = {
      id = "55555555-5555-5555-5555-555555555555"
    }
  }
  mock_resource "scaleway_edge_services_tls_stage" {
    defaults = {
      id = "66666666-6666-6666-6666-666666666666"
    }
  }
  mock_resource "scaleway_edge_services_dns_stage" {
    defaults = {
      id = "77777777-7777-7777-7777-777777777777"
    }
  }
}

variables {
  organization_id = "00000000-0000-0000-0000-000000000000"
  admin_principal = "user_id:88888888-8888-8888-8888-888888888888"
}

run "the_bucket_belongs_to_the_site_project_and_keeps_its_versions" {
  command = apply

  assert {
    condition     = scaleway_object_bucket.site.project_id == scaleway_account_project.site.id
    error_message = "scaleway_object_bucket.site must live in scaleway_account_project.site.id: the IAM policy's project scope is the bucket's only boundary, and Object Storage cannot be named in an IAM rule (PRD #297 hosting decision 5)."
  }

  assert {
    condition     = try(length(scaleway_object_bucket.site.versioning), 0) == 1 && try(scaleway_object_bucket.site.versioning[0].enabled, false)
    error_message = "scaleway_object_bucket.site must set `versioning { enabled = true }`: without versioning a page a leaked deploy key overwrites cannot be rolled back (PRD #297 hosting decision 5)."
  }
}

run "the_website_configuration_serves_index_html_from_the_site_bucket" {
  command = apply

  assert {
    condition     = contains([scaleway_object_bucket.site.name, scaleway_object_bucket.site.id], scaleway_object_bucket_website_configuration.site.bucket)
    error_message = "scaleway_object_bucket_website_configuration.site must target the site bucket (bucket = scaleway_object_bucket.site.name, or its id)."
  }

  assert {
    condition     = try(length(scaleway_object_bucket_website_configuration.site.index_document), 0) == 1 && try(scaleway_object_bucket_website_configuration.site.index_document[0].suffix, null) == "index.html"
    error_message = "scaleway_object_bucket_website_configuration.site needs `index_document { suffix = \"index.html\" }`: a bucket website answers / with its index document, and the site's entry page is index.html."
  }
}
