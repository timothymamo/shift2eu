# The inputs' own validation (issue #422): infra/site has no default for organization_id, and a value that
# is not a Scaleway organization UUID must be refused before any resource is planned, so a typo cannot
# create the site's project in the wrong organization. `mock_provider` keeps the run offline: nothing here
# contacts Scaleway, and a plan against the mock needs no credential.
#
# The plan run plans the whole configuration, so the mock gives every resource's computed id a fixed, valid
# UUID: the provider's schema validates the ids it is given, and the mock's own random strings are not
# UUIDs. Each test file's mocks stand alone.

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

run "plans_with_the_provider_mocked" {
  command = plan
}

run "refuses_an_organization_id_that_is_not_a_uuid" {
  command = plan

  variables {
    organization_id = "not-a-uuid"
  }

  expect_failures = [var.organization_id]
}
