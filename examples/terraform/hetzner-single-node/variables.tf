variable "name" {
  description = "Base name for the Hetzner resources."
  type        = string
  default     = "slipway-demo"

  validation {
    condition     = can(regex("^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$", var.name))
    error_message = "name must be a DNS-safe lowercase hostname segment."
  }
}

variable "location" {
  description = "Hetzner Cloud location for the server."
  type        = string
  default     = "hel1"
}

variable "server_type" {
  description = "Hetzner Cloud server type."
  type        = string
  default     = "cx23"
}

variable "image" {
  description = "Hetzner Cloud image name."
  type        = string
  default     = "ubuntu-24.04"
}

variable "admin_user" {
  description = "Linux user created for SSH and Slipway operations."
  type        = string
  default     = "deploy"

  validation {
    condition     = can(regex("^[a-z_][a-z0-9_-]*[$]?$", var.admin_user))
    error_message = "admin_user must be a valid Linux user name."
  }
}

variable "ssh_public_key" {
  description = "SSH public key content authorized for the admin user. Overrides ssh_public_key_path when set."
  type        = string
  default     = ""
}

variable "ssh_public_key_path" {
  description = "Path to the SSH public key authorized for the admin user. Used only when ssh_public_key is empty."
  type        = string
  default     = "~/.ssh/id_ed25519.pub"
}

variable "existing_ssh_key_name" {
  description = "Existing Hetzner Cloud SSH key name to attach to the server instead of creating a new key."
  type        = string
  default     = ""
}

variable "admin_cidrs" {
  description = "CIDR ranges allowed to reach SSH on port 22."
  type        = list(string)

  validation {
    condition     = length(var.admin_cidrs) > 0 && alltrue([for cidr in var.admin_cidrs : can(cidrhost(cidr, 0))])
    error_message = "admin_cidrs must contain at least one valid IPv4 or IPv6 CIDR."
  }
}

variable "enable_backups" {
  description = "Enable Hetzner Cloud backups for the server."
  type        = bool
  default     = false
}

variable "labels" {
  description = "Extra labels applied to created Hetzner resources."
  type        = map(string)
  default     = {}
}
