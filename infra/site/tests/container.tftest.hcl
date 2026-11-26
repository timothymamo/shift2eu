# The site's front (PRD #297 milestone 4, ADR 0045): one Serverless Containers namespace in the site's
# project, the one public container that runs the pinned Caddy image with the repository's Caddyfile, and the
# two custom domains the Runbook creates in a later apply. Edge Services stays behind it as the only reader of
# the private bucket: the container's UPSTREAM is the pipeline's own endpoint, so the front adds no reader of
# the bucket and no second copy of the site.
#
# The cost pins (ADR 0045, *The container*): max_scale 1, 140 mvCPU and 268 MB (about 0.268 GB) of memory bound the
# worst case to one instance; min_scale 0 keeps the site in the free tier, at the price of a cold start on the first
# visit after an idle period. The memory pin is 268000000 bytes, the value the platform kept, not 268435456 (256 MiB):
# asked for 268435456, the platform kept 268000000 (observed 2026-11-25, issue #596), so every plan after an apply
# would show that change. That the platform keeps whole decimal megabytes is no documented rule, and the pin holds the
# answer an apply gave rather than that rule. The deprecated spellings stay out:
# memory_limit is in megabytes and http_option is the older spelling of https_connections_only.
#
# `mock_provider` replaces the Scaleway provider, so `tofu test` needs no credential and contacts nothing. The
# mock's computed ids must be the values the configuration needs at plan time (the container's required
# namespace_id, the namespace's id) and must satisfy the provider's own validators (UUIDs), hence the fixed
# values below.

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
      id           = "77777777-7777-7777-7777-777777777777"
      default_fqdn = "33333333-3333-3333-3333-333333333333.svc.edge.scw.cloud"
    }
  }
  mock_resource "scaleway_container_namespace" {
    defaults = {
      id = "99999999-9999-9999-9999-999999999999"
    }
  }
  mock_resource "scaleway_container" {
    defaults = {
      id = "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
    }
  }
}

variables {
  organization_id = "00000000-0000-0000-0000-000000000000"
  admin_principal = "user_id:88888888-8888-8888-8888-888888888888"
}

# The namespace is the site's, in the region the bucket and the pipeline are in: a namespace in another
# project puts the front outside the deploy key's boundary and the site's IAM policy, and one in another region
# puts it in a region PRD #297 hosting decision 9 did not price.
run "one_container_namespace_in_the_site_project_and_region" {
  command = plan

  assert {
    condition     = scaleway_container_namespace.site.project_id == scaleway_account_project.site.id
    error_message = "scaleway_container_namespace.site must be subscribed in scaleway_account_project.site.id (project_id = its id): that project is the deploy key's boundary, and the front belongs to the site (ADR 0045)."
  }

  assert {
    condition     = scaleway_container_namespace.site.region == var.region
    error_message = "scaleway_container_namespace.site must be in var.region: PRD #297 hosting decision 9 priced Amsterdam, and the bucket and the pipeline the container fetches from are there."
  }
}

# One public container in that namespace, sized by the cost pins, with no platform redirect of its own: plain
# HTTP reaches Caddy, which answers every request itself (ADR 0045, *The container*, and its table), and the
# platform's default public endpoint is what the Runbook's DNS records point at.
run "one_public_container_sized_to_the_cost_pins" {
  command = plan

  assert {
    condition     = scaleway_container.site.namespace_id == scaleway_container_namespace.site.id
    error_message = "scaleway_container.site must live in scaleway_container_namespace.site (namespace_id = its id): the namespace is the site project's, and a container in another namespace is one the site's configuration did not create."
  }

  assert {
    condition     = scaleway_container.site.port == 8080 && scaleway_container.site.privacy == "public"
    error_message = "scaleway_container.site must serve on port 8080 and be public: Caddy listens on :8080 (the Caddyfile's site address), and the site is a public website (ADR 0045)."
  }

  assert {
    condition     = scaleway_container.site.min_scale == 0
    error_message = "scaleway_container.site must set min_scale = 0: with no instance kept warm the site stays in the free tier (ADR 0045, *The container*), and R8 warms the container before the live checks run."
  }

  assert {
    condition     = scaleway_container.site.max_scale == 1
    error_message = "scaleway_container.site must set max_scale = 1: it is the cost pin ADR 0045 prices at about EUR 2.12 a month in the worst case, and a second instance is a cost nobody decided (dev/infra-rules.sh refuses one in any scaleway_container)."
  }

  assert {
    condition     = scaleway_container.site.cpu_limit == 140
    error_message = "scaleway_container.site must set cpu_limit = 140 (mvCPU): it is the cost pin ADR 0045 prices, and dev/infra-rules.sh refuses another value."
  }

  assert {
    condition     = scaleway_container.site.memory_limit_bytes == 268000000
    error_message = "scaleway_container.site must set memory_limit_bytes = 268000000 (268 MB, about 0.268 GB, the value the platform kept when asked for 268435456): it is the pin ADR 0045 bounds the cost with (priced at 0.25 GB; about EUR 2.22 billed at 268 MB), and memory_limit, the deprecated spelling in megabytes, stays unset. 268435456 (256 MiB) came back as 268000000 (observed 2026-11-25, issue #596), so every plan after an apply would show the change."
  }

  assert {
    condition     = scaleway_container.site.https_connections_only == false
    error_message = "scaleway_container.site must set https_connections_only = false (plain HTTP enabled): with the platform redirecting, http://www.shift2eu.com and http://shift2eu.com would get the platform's 301 on the same host instead of Caddy's 308 to https://www.shift2eu.com, which is the front ADR 0045 decides and tests."
  }

  assert {
    condition     = scaleway_container.site.enable_default_public_endpoint == true
    error_message = "scaleway_container.site must set enable_default_public_endpoint = true: the Runbook's CNAME and ALIAS records point at the container's default endpoint, the only name the platform gives it without a custom domain."
  }
}

# The pinned image by digest, the command and arguments that put the repository's Caddyfile in front of Caddy,
# and the upstream: the Caddyfile is a file in the repository, passed at start, so no image is built and no
# registry key exists (ADR 0045, *The image and its configuration*). The environment variables are capped at
# 1000 characters, so the Caddyfile travels as the shell's $1, never in one.
#
# The Caddyfile's bytes are not compared here: dev/infra-rules.sh refuses a file() call in a test file
# ([file-function]), since what a plan reads from the machine that runs it must be nothing. What this file pins is
# the shape: three arguments, the shell's $0 and the Caddyfile it writes out of $1, which holds the two decisions
# ADR 0045 takes for it (auto_https off and the reverse proxy to https://{$UPSTREAM}). The byte-for-byte
# comparison with infra/site/Caddyfile is the configuration test ADR 0045 gives it, which reads both files.
run "the_container_runs_the_pinned_image_with_the_repositorys_caddyfile" {
  command = plan

  assert {
    condition     = scaleway_container.site.image == "docker.io/library/caddy:2.11.9-alpine@sha256:003a97808e781aa9f7e75cc2f43d34d461099031faf5e20faefb9f61126836f0"
    error_message = "scaleway_container.site.image must be the Caddy release pinned by digest (docker.io/library/caddy:2.11.9-alpine@sha256:003a9780...36f0): a tag alone is whatever the registry serves that day, and Renovate moves this one pin (ADR 0045, and the custom manager test in test/lint)."
  }

  assert {
    condition     = length(scaleway_container.site.command) == 2 && scaleway_container.site.command[0] == "/bin/sh" && scaleway_container.site.command[1] == "-c"
    error_message = "scaleway_container.site.command must be [\"/bin/sh\", \"-c\"]: the image's busybox shell writes the Caddyfile out of its first argument and then runs Caddy (ADR 0045, *The image and its configuration*)."
  }

  assert {
    condition     = length(scaleway_container.site.args) == 3 && scaleway_container.site.args[1] == "caddy"
    error_message = "scaleway_container.site.args must hold three arguments, the shell's $0 being \"caddy\" and its $1 (the third argument) the Caddyfile: the shell writes that one out, and nothing else is passed to the container (ADR 0045)."
  }

  assert {
    condition     = strcontains(scaleway_container.site.args[2], "auto_https off") && strcontains(scaleway_container.site.args[2], "reverse_proxy https://{$UPSTREAM}")
    error_message = "scaleway_container.site.args[2] must be the repository's Caddyfile, holding its global auto_https off and its reverse_proxy to https://{$UPSTREAM}: a container that runs anything else serves something a review of infra/site/Caddyfile did not see (ADR 0045, *The image and its configuration*)."
  }

  assert {
    condition     = alltrue([for part in ["> /tmp/Caddyfile", "exec caddy run --adapter caddyfile --config /tmp/Caddyfile"] : strcontains(scaleway_container.site.args[0], part)])
    error_message = "scaleway_container.site.args[0] must write the Caddyfile it is given to /tmp/Caddyfile and exec Caddy on that file: the platform caps a container's environment variables at 1000 characters, so the configuration travels as an argument, and the container runs the repository's file byte for byte (ADR 0045)."
  }

  assert {
    condition     = try(scaleway_container.site.environment_variables["UPSTREAM"], null) == scaleway_edge_services_dns_stage.site.default_fqdn
    error_message = "scaleway_container.site's UPSTREAM environment variable must be scaleway_edge_services_dns_stage.site.default_fqdn: Caddy reverse-proxies https://{$UPSTREAM}, the pipeline's own endpoint, which is the only reader of the private bucket (ADR 0045, *The upstream*)."
  }
}

# The custom domains are created in a runbook step of their own, after the container is up and the CNAME and
# ALIAS records exist: Scaleway checks the record and issues the certificate, and fails the apply after three
# minutes when the record is missing. So they are off by default, and the first applies create none.
run "front_domains_off_creates_no_custom_domain" {
  command = plan

  assert {
    condition     = length(scaleway_container_domain.front) == 0
    error_message = "with front_domains off (the default) the configuration must create no scaleway_container_domain: a custom domain needs its DNS record first, so the runbook stages that apply (ADR 0045)."
  }
}

# The Runbook's second front apply: both names on the one container, so that https://www.shift2eu.com and
# https://shift2eu.com reach Caddy, which sends every other host and every plain-HTTP request to
# https://www.shift2eu.com.
run "front_domains_on_adds_www_and_the_apex_to_the_one_container" {
  command = plan

  variables {
    front_domains = true
  }

  assert {
    condition     = length(scaleway_container_domain.front) == 2 && toset([for domain in scaleway_container_domain.front : domain.hostname]) == toset(["www.shift2eu.com", "shift2eu.com"])
    error_message = "with front_domains on the configuration must create exactly two custom domains, www.shift2eu.com and shift2eu.com: the site is served on www, the apex redirects to it, and no other name is the site's (PRD #297 hosting decisions 1 and 3)."
  }

  assert {
    condition     = alltrue([for domain in scaleway_container_domain.front : domain.container_id == scaleway_container.site.id])
    error_message = "both custom domains must be on scaleway_container.site (container_id = its id): the apex redirects with the same Caddy configuration the www site is served with, so a certificate, a header or a rule never differs between the two names (ADR 0045)."
  }
}

# An empty upstream on a first setup (issue #566, point 1). The container's UPSTREAM is the DNS stage's
# default_fqdn, the pipeline's own endpoint; on a first apply the stage may still report it empty, and Caddy
# refuses `reverse_proxy https://` without a host, so a container created with an empty UPSTREAM starts, fails to
# load its configuration and answers nothing. The container holds a precondition for it (ADR 0045, *The
# upstream's precondition*), and this run is that first setup: the DNS stage's default_fqdn is empty, and the plan must fail on
# scaleway_container.site rather than create a container that cannot serve the site. The other runs above keep the
# mock's non-empty default_fqdn, so they are the second pass, in which the endpoint exists.
run "an_empty_upstream_fails_the_plan_on_the_container" {
  command = plan

  override_resource {
    target = scaleway_edge_services_dns_stage.site
    values = {
      id           = "77777777-7777-7777-7777-777777777777"
      default_fqdn = ""
    }
  }

  expect_failures = [scaleway_container.site]
}
