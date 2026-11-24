# The Edge Services pipeline that serves the site (issue #422, PRD #297 "Hosting decisions" 1, 9 and 10,
# milestone 4 and ADR 0045). Edge Services stays the only reader of the private bucket: the front is the
# Serverless Container of container.tftest.hcl, which reverse-proxies the pipeline's own endpoint, so the
# pipeline serves no custom domain of its own any more - its www.shift2eu.com DNS stage would answer the
# bucket beside Caddy's redirects and headers, which is what ADR 0045 removed. The pipeline's stages belong to
# the one pipeline, each names the one before it by id - the head stage enters at DNS, then cache, then the
# backend - and with no domain the DNS stage's fqdns is empty and it hands the request to the cache stage
# directly: no TLS stage remains in the chain. Every resource of the pipeline is subscribed in the site's
# project, the deploy key's boundary, and the plan is the Starter one decision 9 priced.
#
# The removal sits behind pipeline_www (a bool, false by default): the run below, with it true, pins the
# milestone 3 chain that R8's rollback restores - the DNS stage's www fqdn and its TLS stage - so the rollback
# is a variable, not a rewrite of this file.
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

run "one_pipeline_serves_the_site_bucket_without_a_domain_or_a_tls_stage" {
  command = apply

  assert {
    condition = alltrue([
      for pipeline_id in [
        scaleway_edge_services_backend_stage.site.pipeline_id,
        scaleway_edge_services_cache_stage.site.pipeline_id,
        scaleway_edge_services_dns_stage.site.pipeline_id,
      ] : pipeline_id == scaleway_edge_services_pipeline.site.id
    ])
    error_message = "the backend, cache and DNS stages must all belong to scaleway_edge_services_pipeline.site: the Starter plan includes one pipeline, and a stage in another pipeline serves nothing the container fetches (PRD #297 hosting decision 9)."
  }

  assert {
    condition     = length(try(scaleway_edge_services_dns_stage.site.fqdns, [])) == 0
    error_message = "scaleway_edge_services_dns_stage.site's fqdns must be empty: the pipeline serves no custom domain, since www.shift2eu.com and shift2eu.com are the container's custom domains and Caddy, not Edge Services, answers them (ADR 0045, *The upstream*)."
  }

  assert {
    condition     = length(scaleway_edge_services_tls_stage.site) == 0
    error_message = "the pipeline must hold no scaleway_edge_services_tls_stage: with pipeline_www at its default false no custom domain enters the pipeline, so there is no certificate to terminate and the chain is DNS -> cache -> backend (ADR 0045, *The upstream*). The stage is gated by pipeline_www, so R8's rollback can bring it back - see the run below."
  }

  assert {
    condition     = try(scaleway_edge_services_dns_stage.site.cache_stage_id, null) == scaleway_edge_services_cache_stage.site.id
    error_message = "scaleway_edge_services_dns_stage.site must hand every request to scaleway_edge_services_cache_stage.site (cache_stage_id = its id): with no custom domain there is no certificate to terminate, so the chain is DNS -> cache -> backend, and a DNS stage still pointing at a TLS stage either fails the apply or serves through a certificate nothing needs."
  }

  assert {
    condition     = try(length(scaleway_edge_services_backend_stage.site.s3_backend_config), 0) == 1 && try(scaleway_edge_services_backend_stage.site.s3_backend_config[0].bucket_name, null) == scaleway_object_bucket.site.name && try(scaleway_edge_services_backend_stage.site.s3_backend_config[0].bucket_region, null) == scaleway_object_bucket.site.region
    error_message = "scaleway_edge_services_backend_stage.site must have exactly one s3_backend_config naming the site bucket and the bucket's region (bucket_name = scaleway_object_bucket.site.name, bucket_region = scaleway_object_bucket.site.region): the pipeline serves that bucket and nothing else."
  }
}

# The request's whole path, stage by stage: DNS -> cache -> backend (the site bucket in website mode), entered
# through the head stage. Each stage names the one before it by id, so a stage chained to another pipeline's
# stage, or a stage nothing points at, serves nothing. Everything the pipeline is made of is subscribed in the
# site's project, so no Edge Services resource lands in the organization's default project; the head stage
# names no project of its own (the provider's schema has none: it belongs to the pipeline's). The Starter plan
# is the one PRD #297 hosting decision 9 priced.
run "the_chain_covers_every_stage_and_names_the_site_project" {
  command = apply

  assert {
    condition     = scaleway_edge_services_head_stage.site.head_stage_id == scaleway_edge_services_dns_stage.site.id
    error_message = "scaleway_edge_services_head_stage.site must enter the pipeline at scaleway_edge_services_dns_stage.site (head_stage_id = its id): the DNS stage is where the container's own requests arrive, and a head stage pointing at another stage serves another path."
  }

  assert {
    condition     = scaleway_edge_services_cache_stage.site.backend_stage_id == scaleway_edge_services_backend_stage.site.id
    error_message = "scaleway_edge_services_cache_stage.site must fetch from scaleway_edge_services_backend_stage.site (backend_stage_id = its id): the cache in front of another backend, or of none, does not serve the site bucket."
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
        scaleway_edge_services_dns_stage.site.project_id,
      ] : project_id == scaleway_account_project.site.id
    ])
    error_message = "the plan, the pipeline and the backend, cache and DNS stages must all be subscribed in scaleway_account_project.site.id (project_id = its id): that project is the deploy key's boundary, and a resource in the organization's default project is one the site's IAM policy does not scope."
  }
}

# pipeline_www = true brings the pipeline's www custom domain and its TLS stage back (ADR 0045, *The
# upstream*; R8's rollback): the milestone 3 chain, DNS -> TLS -> cache -> backend, with the managed
# certificate at the TLS stage. The stage's certificate is requested only when the stage is created, so its
# lifecycle block sets create_before_destroy = true - a lifecycle block is nothing a run can assert, and
# dev/infra-rules.sh holds the one scaleway_edge_services_tls_stage "site" to it ([tls-stage-lifecycle],
# issue #541). The stage is gated by pipeline_www, so its absence above and its presence here are one
# setting's two states: each run reads its own.
run "pipeline_www_restores_the_www_chain" {
  command = apply

  variables {
    pipeline_www = true
  }

  assert {
    condition     = length(scaleway_edge_services_tls_stage.site) == 1
    error_message = "with pipeline_www = true the pipeline must hold exactly one scaleway_edge_services_tls_stage named site: www is a custom domain of the pipeline again, so the certificate that serves it has a stage to be terminated at (ADR 0045, *The upstream*)."
  }

  assert {
    condition     = try(scaleway_edge_services_tls_stage.site[0].managed_certificate, false)
    error_message = "scaleway_edge_services_tls_stage.site must set managed_certificate = true: Scaleway issues and renews the www certificate (PRD #297 hosting decision 9), and an unmanaged one would need a certificate and a key in the state."
  }

  assert {
    condition     = try(scaleway_edge_services_tls_stage.site[0].cache_stage_id, null) == scaleway_edge_services_cache_stage.site.id
    error_message = "scaleway_edge_services_tls_stage.site must terminate in front of scaleway_edge_services_cache_stage.site (cache_stage_id = its id): the TLS stage of the milestone 3 chain hands the request to the cache, which fetches from the backend, and a TLS stage in front of another stage serves another path."
  }

  assert {
    condition     = try(scaleway_edge_services_dns_stage.site.tls_stage_id, null) == scaleway_edge_services_tls_stage.site[0].id
    error_message = "with pipeline_www = true scaleway_edge_services_dns_stage.site must enter the chain at the TLS stage (tls_stage_id = its id): the www fqdn is terminated there, and a DNS stage left pointing at the cache stage would answer https for www with no certificate of its own."
  }

  assert {
    condition     = try(length(scaleway_edge_services_dns_stage.site.fqdns) == 1 && contains(scaleway_edge_services_dns_stage.site.fqdns, "www.shift2eu.com"), false)
    error_message = "with pipeline_www = true scaleway_edge_services_dns_stage.site's fqdns must be [\"www.shift2eu.com\"]: it is the one custom domain the pipeline served before ADR 0045, and the Rollback gives it back so www reaches the bucket through the pipeline again."
  }
}
