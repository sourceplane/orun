terraform {
  required_version = ">= 1.6.0"
}

locals {
  stack_name  = "cluster-addons"
  owner       = "platform"
  environment = "shared-services"
}

output "stack_name" {
  value = local.stack_name
}

output "owner" {
  value = local.owner
}

output "environment" {
  value = local.environment
}