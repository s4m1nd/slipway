# Releasing

Slipway releases are cut from Git tags. The alpha release flow is intentionally
small: run the local gate, build the release artifacts, push a `v*` tag, and let
GitHub Actions publish the release assets.

## Local Gate

```sh
make check
scripts/release/build.sh v0.1.0-alpha-test
```

`make check` runs formatting checks, tests, shell syntax checks, install dry-run
checks, and the main Slipway dry-run paths against `slipway.example.yml`.

The release build writes four binaries plus `checksums.txt` under `dist/`:

```text
dist/slipway_darwin_amd64
dist/slipway_darwin_arm64
dist/slipway_linux_amd64
dist/slipway_linux_arm64
dist/checksums.txt
```

Before publishing a public alpha, run the public hygiene tests and a targeted
scrub if you have local private values to check:

```sh
go test .
git grep -nE '<private-ip>|<private-domain>|<1password-account-id>|<1password-vault-id>'
git ls-files | grep -E 'AGENTS|CODEX|MVP|live-nginx-real-run'
```

The two grep commands should print nothing.

## Tag An Alpha

Use a semantic prerelease tag for alpha builds:

```sh
git tag v0.1.0-alpha.1
git push origin v0.1.0-alpha.1
```

The release workflow runs on `v*` tags. Tags containing `alpha`, `beta`, or `rc`
are published as prereleases.

## Installer Smoke

After GitHub publishes the release, test the installer against that tag:

```sh
curl -fsSL https://raw.githubusercontent.com/s4m1nd/slipway/main/scripts/install.sh | SLIPWAY_VERSION=v0.1.0-alpha.1 bash
slipway version
```

Use `scripts/install.sh --dry-run --version v0.1.0-alpha.1` when you only need
to inspect the resolved URLs.

The unpinned `latest` installer URL points at GitHub's latest non-prerelease
release, so it will become useful after the first stable release. Alpha, beta,
and release-candidate users should pin the tag.
