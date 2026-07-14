# Doppler Secret Provider

Slipway can resolve deploy, accessory, and `secrets exec` values from a Doppler
project and config. The Doppler CLI remains responsible for authentication;
Slipway never stores a Doppler token in `slipway.yml`.

## Configure Slipway

Declare every value Slipway may request, then select the native Doppler
provider:

```yaml
secrets:
  provider:
    type: doppler
    project: backend
    config: prd
  names:
    - REGISTRY_PASSWORD
    - DATABASE_URL
    - REDIS_PASSWORD
```

`project` and `config` are passed directly to the Doppler CLI. A separate
`doppler.yaml` is not required, so the secret scope remains explicit in the
same Slipway config as the deployment.

## Authenticate

Install the Doppler CLI first. For local development, authenticate it normally:

```sh
doppler login
```

For CI or unattended deployment, provide the bootstrap credential through the
runner environment:

```sh
export DOPPLER_TOKEN=<doppler-service-token>
```

`DOPPLER_TOKEN` is needed before Slipway can contact Doppler, so it cannot be
resolved by that same Doppler provider. Do not commit it to `slipway.yml`, shell
transcripts, Terraform variables, plans, or state.

## Use It

No command-specific flags are needed after the provider is configured:

```sh
slipway deploy -c slipway.yml --env production
slipway accessory apply -c slipway.yml --env production
slipway secrets exec -c slipway.yml --secret HCLOUD_TOKEN -- \
  terraform -chdir=examples/terraform/hetzner-single-node plan
```

Slipway asks Doppler only for the names required by the operation. Deploy
resolves the registry password plus the selected environment's service
secrets. `accessory apply` resolves only the selected accessories' secrets, and
`secrets exec` resolves only repeated `--secret` arguments.

Dry runs do not invoke Doppler:

```sh
slipway deploy -c slipway.yml --env production --dry-run
slipway secrets exec -c slipway.yml --secret HCLOUD_TOKEN --dry-run -- \
  terraform plan
```

Slipway calls `doppler secrets get` with JSON output and parses only the
requested names. Missing or restricted values fail the operation. Secret values
are never added to printed plans or errors. As with the other Slipway adapters,
values containing newlines are rejected because deployed service and accessory
secrets are rendered into Docker env files.
