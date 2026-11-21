# The Edge Services pipeline that serves the site (issue #422, PRD #297 "Hosting decisions" 1, 9 and 10).
# Edge Services serves only a subdomain, so one pipeline serves www.shift2eu.com with a managed Let's
# Encrypt certificate (decision 10's CAA record), fronted by the site bucket. All stages belong to the one
# pipeline, each names the one before it by id - the head stage enters at DNS, then TLS, then cache, then
# the backend - and the domain's traffic reaches it through the TLS stage: a certificate that is not in the
# domain's chain does not serve the domain. Every resource of the pipeline is subscribed in the site's
# project, the deploy key's boundary, and the plan is the Starter one decision 9 priced.
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

run "one_pipeline_serves_www_from_the_site_bucket" {
  command = apply

  assert {
    condition = alltrue([
      for pipeline_id in [
        scaleway_edge_services_backend_stage.site.pipeline_id,
        scaleway_edge_services_cache_stage.site.pipeline_id,
        scaleway_edge_services_tls_stage.site.pipeline_id,
        scaleway_edge_services_dns_stage.site.pipeline_id,
      ] : pipeline_id == scaleway_edge_services_pipeline.site.id
    ])
    error_message = "the backend, cache, TLS and DNS stages must all belong to scaleway_edge_services_pipeline.site: the Starter plan includes one pipeline, and a stage in another pipeline does not serve www.shift2eu.com (PRD #297 hosting decision 9)."
  }

  assert {
    condition     = toset(flatten([try(scaleway_edge_services_dns_stage.site.fqdns, [])])) == toset(["www.shift2eu.com"])
    error_message = "scaleway_edge_services_dns_stage.site's fqdns must be exactly [\"www.shift2eu.com\"]: Edge Services serves the www subdomain only, and the apex shift2eu.com is a redirect served outside Edge Services (PRD #297 hosting decision 1)."
  }

  assert {
    condition     = scaleway_edge_services_dns_stage.site.tls_stage_id == scaleway_edge_services_tls_stage.site.id
    error_message = "scaleway_edge_services_dns_stage.site must be served through scaleway_edge_services_tls_stage.site (tls_stage_id = its id): otherwise the managed certificate is not in the chain that answers www.shift2eu.com."
  }

  assert {
    condition     = scaleway_edge_services_tls_stage.site.managed_certificate == true
    error_message = "scaleway_edge_services_tls_stage.site must set managed_certificate = true: Scaleway then generates and renews the Let's Encrypt certificate for www.shift2eu.com, which PRD #297 hosting decision 10's CAA record pins (a certificate from Secret Manager would be billed and would need a secret in the state)."
  }

  assert {
    condition     = try(length(scaleway_edge_services_backend_stage.site.s3_backend_config), 0) == 1 && try(scaleway_edge_services_backend_stage.site.s3_backend_config[0].bucket_name, null) == scaleway_object_bucket.site.name && try(scaleway_edge_services_backend_stage.site.s3_backend_config[0].bucket_region, null) == scaleway_object_bucket.site.region
    error_message = "scaleway_edge_services_backend_stage.site must have exactly one s3_backend_config naming the site bucket and the bucket's region (bucket_name = scaleway_object_bucket.site.name, bucket_region = scaleway_object_bucket.site.region): the pipeline serves that bucket and nothing else."
  }
}

# The request's whole path, stage by stage: DNS (www.shift2eu.com) -> TLS (the managed certificate) -> cache ->
# backend (the site bucket in website mode), entered through the head stage. Each stage names the one before it by
# id, so a stage chained to another pipeline's stage, or a stage nothing points at, answers nothing. Everything
# the pipeline is made of is subscribed in the site's project, so no Edge Services resource lands in the
# organization's default project; the head stage names no project of its own (the provider's schema has none:
# it belongs to the pipeline's). The Starter plan is the one PRD #297 hosting decision 9 priced.
run "the_chain_covers_every_stage_and_names_the_site_project" {
  command = apply

  assert {
    condition     = scaleway_edge_services_head_stage.site.head_stage_id == scaleway_edge_services_dns_stage.site.id
    error_message = "scaleway_edge_services_head_stage.site must enter the pipeline at scaleway_edge_services_dns_stage.site (head_stage_id = its id): the DNS stage is where www.shift2eu.com's traffic arrives, and a head stage pointing at another stage serves another path."
  }

  assert {
    condition     = scaleway_edge_services_cache_stage.site.backend_stage_id == scaleway_edge_services_backend_stage.site.id
    error_message = "scaleway_edge_services_cache_stage.site must fetch from scaleway_edge_services_backend_stage.site (backend_stage_id = its id): the cache in front of another backend, or of none, does not serve the site bucket."
  }

  assert {
    condition     = scaleway_edge_services_tls_stage.site.cache_stage_id == scaleway_edge_services_cache_stage.site.id
    error_message = "scaleway_edge_services_tls_stage.site must terminate TLS in front of scaleway_edge_services_cache_stage.site (cache_stage_id = its id): a TLS stage chained to no cache stage leaves the managed certificate out of the chain that answers www.shift2eu.com."
  }

  assert {
    condition     = try(length(scaleway_edge_services_backend_stage.site.s3_backend_config), 0) == 1 && try(scaleway_edge_services_backend_stage.site.s3_backend_config[0].is_website, false)
    error_message = "scaleway_edge_services_backend_stage.site's s3_backend_config must set is_website = true: Edge Services then reads the bucket in website mode, so / and every directory serve their index.html instead of a bare object listing (the bucket's index document)."
  }

  assert {
    condition     = scaleway_edge_services_plan.site.name == "starter"
    error_message = "scaleway_edge_services_plan.site must be the \"starter\" plan: it is the one PRD #297 hosting decision 9 priced (one pipeline, 100 GB of cache egress a month, managed certificates), and another plan is a cost nobody decided."
  }

  assert {
    condition = alltrue([
      for project_id in [
        scaleway_edge_services_plan.site.project_id,
        scaleway_edge_services_pipeline.site.project_id,
        scaleway_edge_services_backend_stage.site.project_id,
        scaleway_edge_services_cache_stage.site.project_id,
        scaleway_edge_services_tls_stage.site.project_id,
        scaleway_edge_services_dns_stage.site.project_id,
      ] : project_id == scaleway_account_project.site.id
    ])
    error_message = "the plan, the pipeline and the backend, cache, TLS and DNS stages must all be subscribed in scaleway_account_project.site.id (project_id = its id): that project is the deploy key's boundary, and a resource in the organization's default project is one the site's IAM policy does not scope."
  }
}
