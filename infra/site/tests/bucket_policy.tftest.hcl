# The site bucket's read and write rule (issue #422, PRD #297 "Hosting decisions" 5 and 11). A Scaleway
# bucket policy allows only what it lists, so this file pins every grant: the deploy application writes and
# reads the site's objects, everyone may read them at most, and nothing else - no delete, no bucket
# configuration, no second bucket - is allowed to either. Statements for the maintainer's own principal may
# exist and are not forbidden (the owner must not be locked out), but they may not use the `*` principal.
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

# The statements of scaleway_object_bucket_policy.site.policy, decoded, always as a list: a policy with one
# statement may hold it as a bare object rather than a one-element list. `flatten` is the only way to
# normalise both in one expression, since a test file has no locals to hold the decoded document.
run "the_bucket_policy_is_the_recommended_version_and_only_allows" {
  command = apply

  assert {
    condition     = try(jsondecode(scaleway_object_bucket_policy.site.policy).Version, null) == "2023-04-17"
    error_message = "scaleway_object_bucket_policy.site.policy must be version \"2023-04-17\", Scaleway's recommended bucket-policy version (the deprecated \"2012-10-17\" does not support every action the site needs and is documented as such by the provider)."
  }

  assert {
    condition = alltrue([
      for s in flatten([jsondecode(scaleway_object_bucket_policy.site.policy).Statement]) :
      try(s.Effect, null) == "Allow"
    ])
    error_message = "every statement of scaleway_object_bucket_policy.site.policy must be an Allow: a Scaleway bucket policy allows only what it lists, and a Deny statement does nothing there."
  }

  assert {
    condition = alltrue([
      for s in flatten([jsondecode(scaleway_object_bucket_policy.site.policy).Statement]) :
      length(flatten([try(s.Resource, [])])) > 0 && alltrue([
        for r in flatten([try(s.Resource, [])]) :
        contains([scaleway_object_bucket.site.name, "${scaleway_object_bucket.site.name}/*"], r)
      ])
    ])
    error_message = "every statement of scaleway_object_bucket_policy.site.policy must name at least one resource, and only the site bucket or its objects (`scaleway_object_bucket.site.name` or `${scaleway_object_bucket.site.name}/*`): a resource outside the bucket has no business in its policy."
  }

  assert {
    condition = alltrue([
      for s in flatten([jsondecode(scaleway_object_bucket_policy.site.policy).Statement]) :
      !strcontains(jsonencode(try(s.Principal, null)), "\"*\"") || (
        toset(flatten([try(s.Action, [])])) == toset(["s3:GetObject"]) &&
        toset(flatten([try(s.Resource, [])])) == toset(["${scaleway_object_bucket.site.name}/*"])
      )
    ])
    error_message = "a statement of scaleway_object_bucket_policy.site.policy whose principal is `*` may allow only s3:GetObject, and only on the site bucket's objects (`${scaleway_object_bucket.site.name}/*`, PRD #297 hosting decision 11's public-read fallback): anonymous callers must never write or administer the bucket."
  }

  assert {
    condition = anytrue([
      for s in flatten([jsondecode(scaleway_object_bucket_policy.site.policy).Statement]) :
      strcontains(jsonencode(try(s.Principal, null)), "application_id:${scaleway_iam_application.deploy.id}") &&
      setintersection(toset(flatten([try(s.Action, [])])), toset(["s3:PutObject", "s3:GetObject"])) == toset(["s3:PutObject", "s3:GetObject"]) &&
      contains(toset(flatten([try(s.Resource, [])])), "${scaleway_object_bucket.site.name}/*")
    ])
    error_message = "scaleway_object_bucket_policy.site.policy must have a statement whose SCW principal is \"application_id:${scaleway_iam_application.deploy.id}\" (scaleway_iam_application.deploy) and which allows s3:PutObject and s3:GetObject on `${scaleway_object_bucket.site.name}/*`: the deploy key uploads the site through the bucket policy, and the IAM policy alone cannot name the bucket."
  }

  assert {
    condition = alltrue([
      for s in flatten([jsondecode(scaleway_object_bucket_policy.site.policy).Statement]) :
      (
        strcontains(jsonencode(try(s.Principal, null)), "\"*\"") ||
        strcontains(jsonencode(try(s.Principal, null)), "application_id:${scaleway_iam_application.deploy.id}")
      ) ? length(setsubtract(toset(flatten([try(s.Action, [])])), toset(["s3:PutObject", "s3:GetObject", "s3:ListBucket"]))) == 0 : true
    ])
    error_message = "no statement of scaleway_object_bucket_policy.site.policy may allow anything other than s3:PutObject, s3:GetObject or s3:ListBucket to `*` or to the deploy application (scaleway_iam_application.deploy): s3:*, s3:DeleteObject, s3:PutBucketPolicy and every other bucket-admin action would let the deploy key, or anyone, destroy or reconfigure the site (PRD #297 hosting decision 5)."
  }
}
