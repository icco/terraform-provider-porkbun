# Releasing

A published Terraform Registry version can never be unpublished or replaced. Every release is rehearsed twice and lands as a draft you publish by hand.

## Cut a release

1. Smoke-test against a real domain through a `dev_overrides` block. No automated test touches the live API — the client decode tests run against Porkbun's `/mock` endpoint and the lifecycle tests against an in-process fake — so a real delegation change is only ever exercised by hand.

2. Rehearse locally, with the GoReleaser version `.github/workflows/release.yml` pins:

   ```sh
   goreleaser check
   git tag v1.0.0                                          # local only
   GPG_FINGERPRINT=<FPR> goreleaser release --clean --skip=publish
   git tag -d v1.0.0
   ```

   Check `dist/`: one zip per platform, `gpg --verify dist/*_SHA256SUMS.sig dist/*_SHA256SUMS` passes, `unzip -l` shows a binary named `terraform-provider-porkbun_v1.0.0`, and `file dist/*.sig` says `data` — an armored signature is rejected by the registry.

3. Rehearse in CI: run the **Release** workflow via `workflow_dispatch` with `dry_run` checked. Same runner, same GPG import, same secrets, no publish.

4. Push the tag. The workflow builds a **draft** GitHub release.

5. Inspect the draft, then publish it. The registry ingests on publish.

6. Submit to the OpenTofu registry: open a provider issue at [opentofu/registry](https://github.com/opentofu/registry/issues/new/choose). This is a separate registry, not a mirror. `tofu` rewrites `registry.terraform.io/*` to `registry.opentofu.org/*`, so a provider published only to HashiCorp's registry is uninstallable under OpenTofu — which is what icco.me runs locally, even though its CI uses Terraform.

## If the draft is wrong

Delete the draft release, **delete the remote tag**, then re-push it. Skipping the tag deletion is what makes a release unrepeatable — the push event will not fire again for a tag that already exists.

## One-time setup

All in place. Re-check these if a release fails to appear:

- **Signing key.** A dedicated RSA-4096 key; the registry rejects ECC. The public half is uploaded at registry.terraform.io → signing keys, and `GPG_PRIVATE_KEY` / `PASSPHRASE` are repository secrets. If the signing key and the registered key differ, ingestion fails silently — the GitHub release looks perfect and nothing reaches the registry.
- **Actions are enabled** and all six CI jobs run on pull requests.
- **`Release` is only registered on `main`.** GitHub does not register workflows that exist solely on a branch, so its `workflow_dispatch` button appears once the branch is merged.

## Versions

First release is `v1.0.0`. The eleven tags inherited from upstream (`v0.1.0` through `v0.3.0`, plus a malformed `v.0.1.0`) are inert: the registry ingests GitHub *Releases*, and this fork has none.
