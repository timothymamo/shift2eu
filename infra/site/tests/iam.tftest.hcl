# The deploy key's IAM scope (issue #422, PRD #297 "Hosting decisions" 5): Object Storage cannot be named in
# an IAM rule, so the policy grants only Object Storage object reads and writes, and only in the project
# that holds the site's bucket. An organization-wide rule, another permission set or a second principal
# would let the deploy key reach every project of the organization.
#
# `mock_provider` replaces the Scaleway provider in every test file, so `tofu test` needs no credential and
# contacts nothing: a mocked apply never leaves the process. The mock generates a random string for a
# resource's computed `id`, and the provider's schema validates the ids it is given (a UUID), so the mocks
# below give each resource a fixed, valid UUID. The assertions read the configuration's own values.
#
# The issue's behaviour 6 - no scaleway_iam_api_key resource anywhere - cannot be expressed here: a test
# file may only reference resources the configuration declares, so an assertion cannot read "absent". The
# static check at the top of dev/infra-check.sh holds it instead.

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
}

run "the_deploy_policy_is_an_application_policy_scoped_to_the_site_project" {
  command = apply

  assert {
    condition     = scaleway_iam_policy.deploy.application_id == scaleway_iam_application.deploy.id
    error_message = "scaleway_iam_policy.deploy must belong to scaleway_iam_application.deploy (application_id = scaleway_iam_application.deploy.id): the deploy key's permissions are the application's."
  }

  assert {
    condition     = scaleway_iam_policy.deploy.user_id == null && scaleway_iam_policy.deploy.group_id == null
    error_message = "scaleway_iam_policy.deploy must grant only the deploy application: user_id and group_id must be unset, or a human's own permissions carry into the deploy key."
  }

  assert {
    condition     = length(scaleway_iam_policy.deploy.rule) == 1
    error_message = "scaleway_iam_policy.deploy must have exactly one rule: a second rule could add permission sets, a principal or a scope outside the site's project."
  }

  assert {
    condition     = toset(scaleway_iam_policy.deploy.rule[0].permission_set_names) == toset(["ObjectStorageObjectsWrite", "ObjectStorageObjectsRead"])
    error_message = "scaleway_iam_policy.deploy's rule must grant exactly ObjectStorageObjectsWrite and ObjectStorageObjectsRead: any other permission set widens the deploy key (ObjectStorageBucketsWrite or ObjectStorageFullAccess would let it change the bucket's configuration, ObjectStorageObjectsDelete would let it delete the site)."
  }

  assert {
    condition     = try(length(scaleway_iam_policy.deploy.rule[0].project_ids), 0) == 1 && try(tolist(scaleway_iam_policy.deploy.rule[0].project_ids)[0], null) == scaleway_account_project.site.id
    error_message = "scaleway_iam_policy.deploy's rule must name exactly one project, scaleway_account_project.site.id, the project that holds only the site's bucket."
  }

  assert {
    condition     = scaleway_iam_policy.deploy.rule[0].organization_id == null
    error_message = "scaleway_iam_policy.deploy's rule must not set organization_id: organization-wide scope would give the deploy key every project of the organization (PRD #297 hosting decision 5)."
  }
}
