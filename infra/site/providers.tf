# The Scaleway provider reads its credentials from the maintainer's environment (SCW_ACCESS_KEY,
# SCW_SECRET_KEY) on the admin machine; nothing here holds or names one. The offline tests replace it with
# mock_provider, so make infra-check and CI configure no real provider. No provider project_id: every
# resource that takes one names the site's project itself (main.tf), so nothing lands in the
# organization's default project.
provider "scaleway" {
  organization_id = var.organization_id
  region          = var.region
}
