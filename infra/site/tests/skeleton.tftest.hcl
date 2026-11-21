# A placeholder until the tests of the site's resources replace it (issue #422): it proves tofu test runs
# offline, with the Scaleway provider mocked, so no credential is needed and nothing contacts Scaleway.
mock_provider "scaleway" {}

variables {
  organization_id = "00000000-0000-0000-0000-000000000000"
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
