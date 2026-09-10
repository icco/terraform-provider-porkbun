# Releasing

A published Terraform Registry version can never be unpublished or replaced: a bad `v1.0.0` is permanent under the `icco` namespace. Work through this in order.

## Before the first release

- [ ] **Enable Actions on the fork.** GitHub disables Actions on new forks until a human clicks through the Actions tab. Pushing a tag in that state runs nothing at all — no release, no error — and the push event will not replay: you have to delete the remote tag and push it again.
- [ ] **Merge to `main` first.** GitHub only registers workflows that exist on the default branch, so `Release` has no `workflow_dispatch` button until this branch is merged. Merge, then rehearse.
- [ ] **Confirm the repository is listable.** registry.terraform.io → Publish → Provider, and check that `icco/terraform-provider-porkbun` appears in the repository dropdown. This repository is a fork and HashiCorp's docs say nothing about forks. If it does not appear, recreate it as a fresh non-fork and push this tree — free now, impossible once a tag exists.
- [ ] **Register the signing key, public key first.** The registry accepts RSA and DSA, not ECC. Use a dedicated RSA-4096 release key rather than a personal or work identity.
    1. Upload the **public** key (`gpg --armor --export <FPR>`) to registry.terraform.io → signing keys.
    2. Then set the repository secrets `GPG_PRIVATE_KEY` (`gpg --armor --export-secret-keys <FPR>`) and `PASSPHRASE`.

    If the key that signs and the key that is registered differ, ingestion fails with no useful signal: the GitHub release looks perfect and nothing appears in the registry.
- [ ] **Exercise `porkbun_domain_nameservers` against a real domain** through a `dev_overrides` block. Nothing below tests the one feature this fork exists for.

## Rehearse

Locally, using the GoReleaser version `.github/workflows/release.yml` pins (currently v2.18.1):

```sh
goreleaser check          # catches v2 config errors that otherwise surface only at release time
goreleaser healthcheck
git tag v1.0.0            # local only, do not push
GPG_FINGERPRINT=<FPR> goreleaser release --clean --skip=publish
git tag -d v1.0.0
```

Then inspect `dist/`:

- [ ] one zip per platform;
- [ ] `gpg --verify dist/*_SHA256SUMS.sig dist/*_SHA256SUMS` passes;
- [ ] `unzip -l` shows the binary named `terraform-provider-porkbun_v1.0.0`;
- [ ] `file dist/*.sig` says `data`, not ASCII text. An armored signature is rejected by the registry.

Then in CI: run the `Release` workflow via `workflow_dispatch` with `dry_run` checked. That exercises the real runner, the real GPG import and the real secrets — everything except the irreversible publish.

## Cut the release

The first release is `v1.0.0`. The eleven inherited upstream tags (`v0.1.0` through `v0.3.0`, plus a malformed `v.0.1.0`) are inert: the registry ingests GitHub *Releases*, and this fork has none.

1. [ ] Push the tag. `release.draft` is on permanently, so the workflow produces a **draft** GitHub release.
2. [ ] Inspect the draft, then publish it by hand.

If the draft is wrong, the retry path is all three of these, in order:

1. delete the draft release;
2. **delete the remote tag**;
3. re-push the tag, which re-fires `on.push.tags`.

Skipping step 2 is what makes a re-run impossible.
