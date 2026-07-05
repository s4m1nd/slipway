terraform {
  required_version = ">= 1.5.0"

  required_providers {
    hcloud = {
      source  = "hetznercloud/hcloud"
      version = ">= 1.58.0, < 2.0.0"
    }
  }
}

provider "hcloud" {}
