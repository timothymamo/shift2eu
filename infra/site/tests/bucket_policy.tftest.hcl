# The site bucket's read and write rule (issue #422, PRD #297 "Hosting decisions" 5 and 11). A Scaleway
# bucket policy allows only what it lists, so this file pins every grant: the deploy application writes and
# reads the site's objects, everyone may read them at most, and nothing else - no delete, no bucket
# configuration, no second bucket - is allowed to either. The principals are pinned whole as well: every
# statement names one (`*` or one SCW entry holding one string), and the statements together name exactly the
# maintainer's own principal (`var.admin_principal`, the owner must not be locked out of the bucket it
# manages), the deploy application's, and `*` only while var.public_read is on. No other user, application or
# organization is granted anything.
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

  # The statements above are constrained one kind at a time, which leaves a grant to anyone the three kinds do
  # not name - a second ID, an organization, an object of two principals - unread. So the whole document is
  # constrained too: every principal is one value (the string `*`, or an SCW entry holding a string, never a
  # list of two IDs), and the principals of every statement together are exactly the two the off-by-default
  # configuration writes: the maintainer and the deploy application, and nobody else.
  assert {
    condition = alltrue([
      for s in flatten([jsondecode(scaleway_object_bucket_policy.site.policy).Statement]) :
      try(s.Principal, null) == "*" ? true : (
        try(length(keys(s.Principal)), 0) == 1 &&
        try(keys(s.Principal)[0], null) == "SCW" &&
        # tostring succeeds on a string and fails on a list, which is the shape a second ID arrives in.
        can(tostring(s.Principal.SCW))
      )
    ])
    error_message = "every statement of scaleway_object_bucket_policy.site.policy must name its principal as a single value: the string `*`, or one SCW entry holding one string (exactly `var.admin_principal` or `application_id:<scaleway_iam_application.deploy.id>`). A principal list holds a second ID beside the one the statement is about, and a policy reader then cannot tell who the statement grants."
  }

  assert {
    condition = toset(flatten([
      for s in flatten([jsondecode(scaleway_object_bucket_policy.site.policy).Statement]) :
      try(s.Principal, null) == "*" ? ["*"] : [try(tostring(s.Principal.SCW), "<no SCW principal>")]
    ])) == toset([var.admin_principal, "application_id:${scaleway_iam_application.deploy.id}"])
    error_message = "the principals of scaleway_object_bucket_policy.site.policy's statements together must be exactly var.admin_principal and \"application_id:${scaleway_iam_application.deploy.id}\" while public_read is off (its default): a bucket policy allows only what it lists, so any other principal - an organization, a second application, a second user - is a grant nobody decided (PRD #297 hosting decision 5)."
  }
}

# Hosting decision 11's fallback, made explicit (var.public_read): off, nobody anonymous may read anything; on,
# exactly one statement lets everyone read the site's objects, and nothing else.
run "public_read_off_keeps_everyone_out" {
  command = apply

  variables {
    public_read = false
  }

  assert {
    condition = !contains(toset(flatten([
      for s in flatten([jsondecode(scaleway_object_bucket_policy.site.policy).Statement]) :
      try(s.Principal, null) == "*" ? ["*"] : [try(tostring(s.Principal.SCW), "<no SCW principal>")]
    ])), "*")
    error_message = "with public_read = false, no statement of scaleway_object_bucket_policy.site.policy may name the principal `*`: the Runbook applies the site with public read off, and anonymous callers get nothing until Edge Services has been seen to fail on a private object (PRD #297 hosting decision 11)."
  }
}

run "public_read_lets_everyone_read_the_site" {
  command = apply

  variables {
    public_read = true
  }

  assert {
    condition = length([
      for s in flatten([jsondecode(scaleway_object_bucket_policy.site.policy).Statement]) :
      s if try(s.Principal, null) == "*"
    ]) == 1
    error_message = "with public_read = true, scaleway_object_bucket_policy.site.policy must hold exactly one statement whose principal is `*`: one public-read grant, not none and not a second one that could carry another action or resource."
  }

  assert {
    condition = alltrue([
      for s in flatten([jsondecode(scaleway_object_bucket_policy.site.policy).Statement]) :
      try(s.Principal, null) != "*" ? true : (
        toset(flatten([try(s.Action, [])])) == toset(["s3:GetObject"]) &&
        toset(flatten([try(s.Resource, [])])) == toset(["${scaleway_object_bucket.site.name}/*"])
      )
    ])
    error_message = "with public_read = true, the statement whose principal is `*` must allow exactly s3:GetObject on exactly `${scaleway_object_bucket.site.name}/*`: anonymous callers read the site's objects, and never write, delete, list or administer the bucket or anything outside it."
  }

  assert {
    condition = toset(flatten([
      for s in flatten([jsondecode(scaleway_object_bucket_policy.site.policy).Statement]) :
      try(s.Principal, null) == "*" ? ["*"] : [try(tostring(s.Principal.SCW), "<no SCW principal>")]
    ])) == toset(["*", var.admin_principal, "application_id:${scaleway_iam_application.deploy.id}"])
    error_message = "with public_read = true, the principals of scaleway_object_bucket_policy.site.policy's statements together must be exactly `*`, var.admin_principal and \"application_id:${scaleway_iam_application.deploy.id}\": public read adds anonymous readers to the two principals the site already grants, and nobody else."
  }
}

# A bucket policy limits even the organization's owner, so the maintainer's own principal must be in it, and
# it must be named exactly: the SCW principal is `var.admin_principal` verbatim, never a prefix, an
# interpolation or `*` (which would give every caller full control of the bucket).
run "the_admin_statement_names_exactly_the_admin_principal" {
  command = apply

  assert {
    condition = anytrue([
      for s in flatten([jsondecode(scaleway_object_bucket_policy.site.policy).Statement]) :
      try(s.Principal.SCW, null) == var.admin_principal
    ])
    error_message = "scaleway_object_bucket_policy.site.policy must have a statement whose SCW principal is exactly var.admin_principal (\"${var.admin_principal}\"): the bucket policy limits the maintainer too, so without it an apply locks the owner out of the bucket it manages, and any other spelling of the principal - above all `*` - either matches nobody or everyone."
  }

  assert {
    condition = alltrue([
      for s in flatten([jsondecode(scaleway_object_bucket_policy.site.policy).Statement]) :
      try(s.Principal.SCW, null) != var.admin_principal ? true : (
        toset(flatten([try(s.Action, [])])) == toset(["s3:*"]) &&
        toset(flatten([try(s.Resource, [])])) == toset([scaleway_object_bucket.site.name, "${scaleway_object_bucket.site.name}/*"])
      )
    ])
    error_message = "the statement whose SCW principal is var.admin_principal must allow s3:* on both `${scaleway_object_bucket.site.name}` and `${scaleway_object_bucket.site.name}/*`: it is what keeps the maintainer in full control of the bucket OpenTofu manages, and a narrower grant would leave the next apply unable to reconfigure the bucket or the site's objects."
  }
}
