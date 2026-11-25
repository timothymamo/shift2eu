# The site's front (PRD #297 milestone 4, ADR 0045): one public Serverless Container running the official
# Caddy image, pinned by digest, with the Caddyfile beside this file. It serves www.shift2eu.com and the apex
# shift2eu.com, writes every redirect and security header, and reverse-proxies www from the Edge Services
# pipeline's own endpoint (main.tf), which stays the only reader of the private bucket. No image is built and
# no registry key exists: the configuration reaches the upstream image at start.

locals {
  # The image, by tag and by the digest of its multi-arch index. The -alpine index lists Linux images only
  # (caddy:<version> is the same linux/amd64 image, but its index lists Windows ones too); the image has
  # busybox's /bin/sh, which the command below runs. Renovate moves this one line (.github/renovate.json5).
  # renovate: datasource=docker depName=caddy
  caddy_image = "docker.io/library/caddy:2.11.9-alpine@sha256:003a97808e781aa9f7e75cc2f43d34d461099031faf5e20faefb9f61126836f0"

  # The names the container serves, each a custom domain with its own Let's Encrypt certificate: www (a
  # Namecheap CNAME to the container's endpoint) and the apex (an ALIAS at @), which Caddy redirects to www.
  front_hostnames = toset(["www.shift2eu.com", "shift2eu.com"])

  # The instances kept warm (ADR 0045, The container), written once: the container sets it and its
  # postcondition checks it, so the two cannot drift. dev/infra-rules.sh accepts only the literal 0 or 1.
  container_min_scale = 0
}

# The front's namespace, in the site's project and region: the project is the site's boundary, and the region
# is the one Hosting decision 9 priced.
resource "scaleway_container_namespace" "site" {
  name        = "shift2eu-site"
  description = "www.shift2eu.com and shift2eu.com: the site's Caddy front (infra/site)."
  project_id  = scaleway_account_project.site.id
  region      = var.region
}

# The cost pins (ADR 0045, The container; dev/infra-rules.sh refuses another value): one instance at most, of
# 140 mvCPU and 0.25 GB, about EUR 2.12 a month in the worst case. min_scale 0 keeps the site in the free tier
# with a cold start on the first visit after an idle period; R8 warms the container before the live checks.
# memory_limit (MB) and http_option are the deprecated spellings of memory_limit_bytes and
# https_connections_only, and are never set.
#
# Plain HTTP stays enabled (https_connections_only = false): with the platform's redirect, plain HTTP would get
# its 301 on the same host instead of Caddy's one 308 to https://www.shift2eu.com.
#
# The Caddyfile travels as the shell's $1, since the provider caps an environment variable at 1000
# characters: the shell writes it to /tmp/Caddyfile and execs Caddy on it, so the container runs this
# repository's file byte for byte. UPSTREAM, read by the Caddyfile as {$UPSTREAM}, is the pipeline's default
# endpoint (<pipeline id>.svc.edge.scw.cloud), so the file itself names no ID.
resource "scaleway_container" "site" {
  name         = "front"
  description  = "The site's Caddy front: redirects, security headers, and www from the Edge Services pipeline (infra/site)."
  namespace_id = scaleway_container_namespace.site.id
  region       = var.region

  image   = local.caddy_image
  command = ["/bin/sh", "-c"]
  args = [
    "printf '%s' \"$1\" > /tmp/Caddyfile && exec caddy run --adapter caddyfile --config /tmp/Caddyfile",
    "caddy",
    file("${path.module}/Caddyfile"),
  ]
  environment_variables = {
    UPSTREAM = scaleway_edge_services_dns_stage.site.default_fqdn
  }

  port                           = 8080
  privacy                        = "public"
  https_connections_only         = false
  enable_default_public_endpoint = true

  min_scale          = local.container_min_scale
  max_scale          = 1
  cpu_limit          = 140
  memory_limit_bytes = 268435456

  lifecycle {
    # Scaleway sets the DNS stage's default_fqdn a while after creating the stage, so on a first setup it may
    # still be empty when the container comes next: UPSTREAM would be "", and Caddy refuses
    # reverse_proxy https:// at start. Refuse the container then (issue #566); the rest of the apply stands,
    # and R4 creates the container in a second pass once edge_endpoint is set. An unknown value (a stage
    # created or replaced in the same plan) is checked at the apply.
    precondition {
      condition     = scaleway_edge_services_dns_stage.site.default_fqdn != null && scaleway_edge_services_dns_stage.site.default_fqdn != ""
      error_message = "The Edge DNS stage's default_fqdn is still empty, so the container's UPSTREAM would be \"\" and Caddy would refuse to start (ADR 0045). On a first setup, or after the DNS stage was replaced, this is expected: the rest of the apply stands; run R4's wait loop until edge_endpoint is set, then plan and apply again to create the container."
    }

    # The provider leaves false and 0 out of the create request (it reads them with GetOk), so the platform's
    # own default would decide them: check after the apply that it kept plain HTTP and the configured
    # min_scale.
    postcondition {
      condition     = self.https_connections_only == false
      error_message = "The container redirects plain HTTP itself (https_connections_only is true): http:// would get the platform's 301 on the same host instead of Caddy's 308 (ADR 0045). Set plain HTTP to enabled in the console, then plan again."
    }
    postcondition {
      condition     = self.min_scale == local.container_min_scale
      error_message = "The container's min_scale differs from local.container_min_scale, so it keeps warm a number of instances the configuration did not set (ADR 0045). Set min_scale to that value in the console, then plan again."
    }
  }
}

# The two custom domains, created only with front_domains = true: Scaleway checks the CNAME or ALIAS record and
# then issues the certificate, and fails the apply after three minutes when the record is missing, so the
# Runbook creates them in an apply of their own, after R8's records point at the container's endpoint.
resource "scaleway_container_domain" "front" {
  for_each = var.front_domains ? local.front_hostnames : toset([])

  container_id = scaleway_container.site.id
  hostname     = each.value
  region       = var.region
}
