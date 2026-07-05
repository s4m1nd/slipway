# Hetzner Single Node

This example creates one Ubuntu server on Hetzner Cloud for Slipway:

- one `hcloud_server`
- one Hetzner Cloud firewall
- one uploaded or reused Hetzner Cloud SSH public key
- Docker installed through cloud-init
- automatic Ubuntu security updates through `unattended-upgrades`
- `fail2ban` for basic SSH abuse protection

Slipway still manages the Docker app containers and the Caddy container. This
example prepares the host; it does not install host Caddy or store app secrets.

## Prerequisites

- Terraform installed locally.
- A Hetzner Cloud project token available as `HCLOUD_TOKEN`.
- An SSH public key as inline public key text, a `.pub` file, or an existing
  Hetzner Cloud SSH key name.
- Your current public IP/CIDR for `admin_cidrs`.

```sh
cp examples/terraform/hetzner-single-node/terraform.tfvars.example \
  examples/terraform/hetzner-single-node/terraform.tfvars
$EDITOR examples/terraform/hetzner-single-node/terraform.tfvars
```

Do not put registry tokens, 1Password metadata, database URLs, or Slipway app
secrets in Terraform variables or cloud-init. Terraform state and plan files can
contain configured values and rendered `user_data`.

If your private key is managed by the 1Password SSH agent, Terraform still needs
the public key text for Hetzner and cloud-init. You can list agent-backed public
keys with:

```sh
ssh-add -L
```

Paste the matching public key into `ssh_public_key`, or leave
`ssh_public_key = ""` and set `ssh_public_key_path` to an existing `.pub` file.
If both are available, `ssh_public_key` wins.

If the same public key is already uploaded to the Hetzner project, leave
`ssh_public_key` empty and set `existing_ssh_key_name` to that Hetzner Cloud SSH
key name instead. Terraform will attach the existing key to the new server and
reuse its public key for the cloud-init `admin_user`.

You can find the name in the Hetzner Cloud console. If you already have the
`hcloud` CLI installed, this also works:

```sh
slipway secrets exec -c slipway.live.yml --secret HCLOUD_TOKEN -- \
  hcloud ssh-key list
```

Without `hcloud`, use the Hetzner Cloud API directly with the same injected
token and read the `name` fields from the returned JSON:

```sh
slipway secrets exec -c slipway.live.yml --secret HCLOUD_TOKEN -- \
  sh -c 'curl -fsS -H "Authorization: Bearer $HCLOUD_TOKEN" https://api.hetzner.cloud/v1/ssh_keys'
```

If an apply fails with `SSH key not unique`, set `existing_ssh_key_name` and run
`terraform apply` again. Terraform should continue from any resources it already
created successfully, such as the firewall.

## Create The Host

From the repo root, run Terraform through Slipway when `HCLOUD_TOKEN` is stored
in the configured `secrets` provider:

```sh
slipway secrets exec -c slipway.live.yml --secret HCLOUD_TOKEN -- \
  terraform -chdir=examples/terraform/hetzner-single-node init
slipway secrets exec -c slipway.live.yml --secret HCLOUD_TOKEN -- \
  terraform -chdir=examples/terraform/hetzner-single-node fmt
slipway secrets exec -c slipway.live.yml --secret HCLOUD_TOKEN -- \
  terraform -chdir=examples/terraform/hetzner-single-node plan
slipway secrets exec -c slipway.live.yml --secret HCLOUD_TOKEN -- \
  terraform -chdir=examples/terraform/hetzner-single-node apply
```

Cloud-init can take a few minutes after Terraform reports the server as created.
Wait for it before running Slipway:

```sh
ssh "$(slipway secrets exec -c slipway.live.yml --secret HCLOUD_TOKEN -- terraform -chdir=examples/terraform/hetzner-single-node output -raw ssh_target)" \
  'cloud-init status --wait && docker version'
```

## Use With Slipway

Print the generated server block:

```sh
slipway secrets exec -c slipway.live.yml --secret HCLOUD_TOKEN -- \
  terraform -chdir=examples/terraform/hetzner-single-node output -raw slipway_server_yaml
```

Copy it into your `slipway.yml` environment:

```yaml
environments:
  production:
    servers:
      app-1:
        host: <terraform output server_ipv4>
        ssh_user: deploy
        host_ssh_port: 22
```

Then inspect Slipway provisioning from the repo root:

```sh
go run ./cmd/slipway provision -c slipway.yml --env production --dry-run
```

When the plan looks right, run it without `--dry-run`.

## Firewall

The Hetzner Cloud firewall allows:

- TCP 22 from `admin_cidrs`
- TCP 80 from anywhere
- TCP 443 from anywhere

This example intentionally does not enable UFW on the server. Docker manages its
own packet filter rules for bridge networking and published ports, so host UFW
requires extra Docker-specific policy to be meaningful. Start with the provider
firewall, then add host firewall rules only when you are ready to own that model.
