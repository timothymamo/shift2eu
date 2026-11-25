# What the Runbook's hand steps need after an apply: no secret, only IDs and endpoints.

output "project_id" {
  description = "The site's project: the scope of the deploy key and of the canary bucket."
  value       = scaleway_account_project.site.id
}

output "deploy_application_id" {
  description = "The deploy application, for the hand-made API key (scw iam api-key create application-id=...)."
  value       = scaleway_iam_application.deploy.id
}

output "bucket_name" {
  description = "The site's bucket."
  value       = scaleway_object_bucket.site.name
}

output "bucket_website_endpoint" {
  description = "The bucket website endpoint, one of the origins Hosting decision 11 lists (with public_read it serves the site too)."
  value       = scaleway_object_bucket_website_configuration.site.website_endpoint
}

output "pipeline_id" {
  description = "The Edge Services pipeline, for the cache purge in the Runbook's recovery step."
  value       = scaleway_edge_services_pipeline.site.id
}

output "edge_endpoint" {
  description = "The pipeline's own endpoint, the container's UPSTREAM: the one name Edge Services answers (ADR 0045)."
  value       = scaleway_edge_services_dns_stage.site.default_fqdn
}

output "container_namespace_id" {
  description = "The container's namespace, where R8's probe containers run and are deleted again."
  value       = scaleway_container_namespace.site.id
}

output "container_endpoint" {
  description = "The container's default endpoint (<namespace>-<name>.functions.fnc.<region>.scw.cloud), the target of the www CNAME and the apex ALIAS at Namecheap (R8); empty while the container does not exist yet, so R4's refresh-only does not fail after the first apply's precondition FAIL."
  value       = try(scaleway_container.site.public_endpoint, "")
}

output "front_domains" {
  description = "The custom domains on the container: none until front_domains is true (R8)."
  value       = sort([for domain in scaleway_container_domain.front : domain.hostname])
}
