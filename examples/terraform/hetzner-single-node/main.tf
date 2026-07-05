locals {
  configured_ssh_public_key = trimspace(var.ssh_public_key)
  existing_ssh_key_name     = trimspace(var.existing_ssh_key_name)
  file_ssh_public_key       = trimspace(try(file(pathexpand(var.ssh_public_key_path)), ""))
  ssh_public_key            = local.configured_ssh_public_key != "" ? local.configured_ssh_public_key : local.file_ssh_public_key
  use_existing_ssh_key      = local.existing_ssh_key_name != ""
  admin_ssh_public_key      = local.use_existing_ssh_key ? data.hcloud_ssh_key.admin[0].public_key : local.ssh_public_key
  ssh_key_ids               = local.use_existing_ssh_key ? [data.hcloud_ssh_key.admin[0].id] : [hcloud_ssh_key.admin[0].id]
  common_labels = merge(
    {
      managed_by = "terraform"
      project    = var.name
      tool       = "slipway"
    },
    var.labels,
  )
}

data "hcloud_ssh_key" "admin" {
  count = local.use_existing_ssh_key ? 1 : 0
  name  = local.existing_ssh_key_name
}

resource "hcloud_ssh_key" "admin" {
  count      = local.use_existing_ssh_key ? 0 : 1
  name       = "${var.name}-${var.admin_user}"
  public_key = local.ssh_public_key
  labels     = local.common_labels

  lifecycle {
    precondition {
      condition     = local.ssh_public_key != ""
      error_message = "Set ssh_public_key or set ssh_public_key_path to an existing .pub file."
    }
  }
}

resource "hcloud_firewall" "slipway" {
  name   = "${var.name}-firewall"
  labels = local.common_labels

  rule {
    direction   = "in"
    protocol    = "tcp"
    port        = "22"
    source_ips  = var.admin_cidrs
    description = "SSH from admin CIDRs"
  }

  rule {
    direction   = "in"
    protocol    = "tcp"
    port        = "80"
    source_ips  = ["0.0.0.0/0", "::/0"]
    description = "HTTP for Slipway-managed Caddy"
  }

  rule {
    direction   = "in"
    protocol    = "tcp"
    port        = "443"
    source_ips  = ["0.0.0.0/0", "::/0"]
    description = "HTTPS for Slipway-managed Caddy"
  }
}

resource "hcloud_server" "app" {
  name        = var.name
  image       = var.image
  server_type = var.server_type
  location    = var.location
  backups     = var.enable_backups
  labels      = local.common_labels

  ssh_keys     = local.ssh_key_ids
  firewall_ids = [hcloud_firewall.slipway.id]

  user_data = templatefile("${path.module}/cloud-init.yaml.tftpl", {
    admin_user     = var.admin_user
    ssh_public_key = local.admin_ssh_public_key
  })

  public_net {
    ipv4_enabled = true
    ipv6_enabled = true
  }
}
