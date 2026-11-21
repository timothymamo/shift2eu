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
  description = "The bucket website endpoint, one of the origins Hosting decision 11 lists (public read serves the site there too)."
  value       = scaleway_object_bucket_website_configuration.site.website_endpoint
}

output "pipeline_id" {
  description = "The Edge Services pipeline, for the cache purge in the Runbook's recovery step."
  value       = scaleway_edge_services_pipeline.site.id
}

output "edge_endpoint" {
  description = "The pipeline's own endpoint, the target of the www CNAME at Namecheap."
  value       = scaleway_edge_services_dns_stage.site.default_fqdn
}
