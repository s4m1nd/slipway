output "server_ipv4" {
  description = "Public IPv4 address for the Slipway host."
  value       = hcloud_server.app.ipv4_address
}

output "server_ipv6" {
  description = "Public IPv6 address for the Slipway host."
  value       = hcloud_server.app.ipv6_address
}

output "ssh_target" {
  description = "SSH target for checking the provisioned host."
  value       = "${var.admin_user}@${hcloud_server.app.ipv4_address}"
}

output "slipway_server_yaml" {
  description = "Ready-to-copy Slipway server block."
  value       = <<-YAML
    servers:
      app-1:
        host: ${hcloud_server.app.ipv4_address}
        ssh_user: ${var.admin_user}
        host_ssh_port: 22
  YAML
}
