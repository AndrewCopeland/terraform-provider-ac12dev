# terraform-provider-ac12dev

Terraform provider for the [ac12.dev](https://ac12.dev) PaaS platform.

**Status: local development / baking phase.** Not yet published to the Terraform
Registry.

---

## Local Development Setup

### 1. Install dependencies and build

```bash
cd terraform-provider
go mod tidy      # downloads dependencies and generates go.sum
go build -o terraform-provider-ac12dev .
```

### 2. Configure Terraform dev overrides

Add this to `~/.terraformrc` so Terraform picks up the local binary instead of
trying to download from the registry:

```hcl
provider_installation {
  dev_overrides {
    "registry.terraform.io/ac12dev/ac12dev" = "/path/to/ac12dev/terraform-provider"
  }

  direct {}
}
```

Replace the path with the absolute path to the `terraform-provider/` directory
(where you just built the binary). You can get this with `pwd` inside that
folder.

### 3. Run an example

```bash
cd terraform-provider/examples

# terraform init is NOT needed when using dev_overrides
terraform plan \
  -var="ac12dev_username=myuser" \
  -var="ac12dev_private_key_path=~/.ac12/keys/default.pem" \
  -var="project_id=185df16d-debd-4f0b-ae47-7a755bb5dbc6"
```

> **Note:** When using dev_overrides, skip `terraform init` — it will warn about
> it but still work.

---

## Provider Configuration

```hcl
provider "ac12dev" {
  server      = "https://ac12.dev"          # optional, default: https://ac12.dev
  username    = "myuser"
  private_key = file("~/.ac12/keys/default.pem")
  project_id  = "185df16d-..."              # optional default project for all resources
}
```

**Environment variables** (all optional overrides):

| Env var               | Corresponds to |
| --------------------- | -------------- |
| `AC12DEV_SERVER`      | `server`       |
| `AC12DEV_USERNAME`    | `username`     |
| `AC12DEV_PRIVATE_KEY` | `private_key`  |
| `AC12DEV_PROJECT_ID`  | `project_id`   |

### Finding your private key

Your key is stored by the CLI at `~/.ac12/keys/<profile>.pem` or in
`~/.ac12/credentials.json` (under the `private_key` field). You can also export
it:

```bash
cat ~/.ac12/credentials.json | jq -r .private_key
```

---

## Resources

### `ac12dev_service`

Deploys a Docker container service. **Creating deploys it; updating
force-redeploys** (picks up new image tags etc.).

```hcl
resource "ac12dev_service" "api" {
  name  = "my-api"
  image = "my-api:latest"
  port  = 8000

  env = {
    APP_ENV     = "production"
    DB_PASSWORD = "secret:DB_PASSWORD"   # platform secret reference
  }

  daemon = true   # default: true
}
```

| Attribute    | Required | Description                      |
| ------------ | -------- | -------------------------------- |
| `name`       | yes      | Service name (immutable)         |
| `image`      | yes      | Docker image                     |
| `port`       | no       | Container port                   |
| `env`        | no       | Environment variables map        |
| `daemon`     | no       | Persistent daemon (default true) |
| `project_id` | no       | Overrides provider default       |

Computed: `id`, `status`, `url`, `created_at`, `updated_at`

---

### `ac12dev_domain`

Maps a subdomain to a service or file path.

```hcl
resource "ac12dev_domain" "api" {
  subdomain      = "my-api"
  target_type    = "service"
  target_service = ac12dev_service.api.name
}

resource "ac12dev_domain" "docs" {
  subdomain   = "docs"
  target_type = "file"
  target_path = "docs/index.html"
}
```

Computed: `id`, `url`, `created_at`, `updated_at`

---

### `ac12dev_cron_job`

Schedules HTTP calls to a service.

```hcl
resource "ac12dev_cron_job" "nightly" {
  name           = "nightly-cleanup"
  schedule       = "0 3 * * *"
  target_service = ac12dev_service.api.name
  target_path    = "/admin/cleanup"
  http_method    = "POST"
}
```

Computed: `id`, `last_run_at`, `last_status`, `created_at`, `updated_at`

---

### `ac12dev_file`

Uploads a file to the project. Supports inline text content or a local file.

```hcl
# Inline text
resource "ac12dev_file" "readme" {
  path      = "README.md"
  content   = "# My Project"
  is_public = true
}

# Local file (use source_hash to detect file changes)
resource "ac12dev_file" "logo" {
  path        = "assets/logo.png"
  source      = "./assets/logo.png"
  source_hash = filemd5("./assets/logo.png")
  is_public   = true
}
```

Computed: `id`, `content_type`, `size_bytes`, `url`, `created_at`, `updated_at`

---

### `ac12dev_secret`

Stores an encrypted secret. The value is **write-only** — the platform never
returns it. It is kept in Terraform state (sensitive).

```hcl
resource "ac12dev_secret" "api_key" {
  name  = "STRIPE_API_KEY"
  value = var.stripe_key
}
```

Reference in a service env: `"secret:STRIPE_API_KEY"`

---

### `ac12dev_project`

Creates a new project. Usually you'll use the default project and skip this.

```hcl
resource "ac12dev_project" "staging" {
  name        = "staging"
  description = "Staging environment"
}
```

---

## Data Sources

### `data.ac12dev_project`

```hcl
# Look up by ID
data "ac12dev_project" "mine" {
  id = "185df16d-debd-4f0b-ae47-7a755bb5dbc6"
}

# Or by name
data "ac12dev_project" "mine" {
  name = "my-project"
}

output "project_slug" {
  value = data.ac12dev_project.mine.slug
}
```

---

## What's not in scope for Terraform

These are better done with the CLI (imperative operations):

- **Building Docker images** — `ssh ... docker build ...`
- **Viewing logs** — `ac12 service logs <name>`
- **Manual cron triggers** — `ac12 cron trigger <id>`
- **Email operations** — `ac12 email ...`
- **IAM** — coming once the IAM refactor is complete

---

## Publishing (future)

When ready to publish:

1. Move to its own repo: `terraform-provider-ac12dev`
2. Register at [registry.terraform.io](https://registry.terraform.io/publish)
3. Set up `goreleaser` with GPG signing
4. Tag a release — registry picks it up automatically
