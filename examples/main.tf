terraform {
  required_providers {
    ac12dev = {
      # For local dev: this is overridden by dev_overrides in ~/.terraformrc
      # See terraform-provider/README.md for setup instructions.
      source  = "registry.terraform.io/ac12dev/ac12dev"
      version = "~> 0.1"
    }
  }
}

provider "ac12dev" {
  server      = "https://ac12.dev"
  username    = var.ac12dev_username
  private_key = file(var.ac12dev_private_key_path)
  project_id  = var.project_id
}

variable "ac12dev_username" {
  description = "ac12.dev username"
  type        = string
}

variable "ac12dev_private_key_path" {
  description = "Path to your Ed25519 private key PEM file"
  type        = string
  default     = "~/.ac12/keys/default.pem"
}

variable "project_id" {
  description = "ac12.dev project ID"
  type        = string
}

# ── Data source: look up the project ────────────────────────────────────────

data "ac12dev_project" "this" {
  id = var.project_id
}

output "project_slug" {
  value = data.ac12dev_project.this.slug
}

# ── Secret ───────────────────────────────────────────────────────────────────

resource "ac12dev_secret" "db_password" {
  name  = "DB_PASSWORD"
  value = var.db_password
}

variable "db_password" {
  description = "Database password"
  type        = string
  sensitive   = true
}

# ── Service ───────────────────────────────────────────────────────────────────

resource "ac12dev_service" "api" {
  name  = "my-api"
  image = "my-api:latest"
  port  = 8000

  env = {
    ENVIRONMENT = "production"
    DB_URL      = "sqlite:///data/app.db"
    # Reference the secret — the platform resolves it at deploy time
    DB_PASSWORD = "secret:${ac12dev_secret.db_password.name}"
  }

  daemon = true
}

# ── Domain ───────────────────────────────────────────────────────────────────

resource "ac12dev_domain" "api" {
  subdomain      = "my-api"
  target_type    = "service"
  target_service = ac12dev_service.api.name
}

output "api_url" {
  value = ac12dev_domain.api.url
}

# ── Static file ───────────────────────────────────────────────────────────────

resource "ac12dev_file" "index" {
  path      = "index.html"
  content   = "<html><body><h1>Hello from Terraform</h1></body></html>"
  is_public = true
}

# Or from a local file:
# resource "ac12dev_file" "logo" {
#   path        = "assets/logo.png"
#   source      = "./assets/logo.png"
#   source_hash = filemd5("./assets/logo.png")
#   is_public   = true
# }

# ── Cron job ─────────────────────────────────────────────────────────────────

resource "ac12dev_cron_job" "cleanup" {
  name           = "cleanup"
  schedule       = "0 2 * * *" # daily at 2am UTC
  target_service = ac12dev_service.api.name
  target_path    = "/tasks/cleanup"
  http_method    = "POST"
}

# ── Outputs ───────────────────────────────────────────────────────────────────

output "service_status" {
  value = ac12dev_service.api.status
}

output "service_url" {
  value = ac12dev_service.api.url
}
