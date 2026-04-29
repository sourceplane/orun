terraform {
  required_version = ">= 1.6.0"

  required_providers {
    cloudflare = {
      source  = "cloudflare/cloudflare"
      version = "~> 4.32"
    }
  }
}

variable "account_id" {
  description = "Cloudflare account identifier used by the Pages project"
  type        = string
}

variable "project_name" {
  description = "Cloudflare Pages project name"
  type        = string
}

variable "production_branch" {
  description = "Git branch used for production deployments"
  type        = string
}

variable "repo_owner" {
  description = "GitHub owner connected to the Cloudflare Pages project"
  type        = string
}

variable "repo_name" {
  description = "GitHub repository connected to the Cloudflare Pages project"
  type        = string
}

variable "build_command" {
  description = "Build command Cloudflare Pages runs for the site"
  type        = string
}

variable "destination_dir" {
  description = "Directory Cloudflare Pages publishes after the build"
  type        = string
}

variable "root_dir" {
  description = "Repository-root relative directory Cloudflare Pages builds from"
  type        = string
}

resource "cloudflare_pages_project" "admin_console" {
  account_id        = var.account_id
  name              = var.project_name
  production_branch = var.production_branch

  source {
    type = "github"

    config {
      owner                         = var.repo_owner
      repo_name                     = var.repo_name
      production_branch             = var.production_branch
      deployments_enabled           = true
      production_deployment_enabled = true
      preview_deployment_setting    = "all"
      pr_comments_enabled           = true
    }
  }

  build_config {
    build_caching   = true
    build_command   = var.build_command
    destination_dir = var.destination_dir
    root_dir        = var.root_dir
  }
}

output "pages_project_name" {
  value       = cloudflare_pages_project.admin_console.name
  description = "Cloudflare Pages project name"
}

output "pages_subdomain" {
  value       = cloudflare_pages_project.admin_console.subdomain
  description = "Cloudflare Pages subdomain assigned to the project"
}