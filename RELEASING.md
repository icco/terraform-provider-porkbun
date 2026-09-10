# Releasing

A published Terraform Registry version can never be unpublished or replaced.
A bad `v0.4.0` is permanent under the `icco` namespace. Everything below
exists to make sure the first tag is not the first test.

## Two gates before anything else

**Gate A — Actions must be enabled on the fork.** As of this writing,
`gh api repos/icco/terraform-provider-porkbun/actions/workflows` returns
`{"total_count":0}` while both workflow files exist on `main`. GitHub
disables Actions on new forks until a human clicks through the Actions tab.
**Pushing a tag in this state runs nothing at all** — no release, no error,
and the tag is spent. Verify the same call returns `2` before tagging.
(`actions/permissions` reporting `enabled: true` is the allowed-actions
policy, not workflow enablement. It is not evidence.)

**Gate B — confirm the fork is listable.** Sign in to registry.terraform.io →
Publish → Provider and check that `icco/terraform-provider-porkbun` appears
in the repository dropdown. **Stop at the dropdown.** HashiCorp's docs say
nothing about forks. If it does not appear, recreate the repository as a
fresh non-fork and push this tree — free today, expensive once a tag exists.

## Signing key

The registry accepts RSA and DSA, **not ECC**. Generate a dedicated RSA-4096
release-signing key rather than reusing a personal or work identity.

Order matters, and getting it wrong fails silently — the GitHub release looks
perfect and ingestion just does not happen:

1. Upload the **public** key (`gpg --armor --export <FPR>`) to
   registry.terraform.io → signing keys.
2. Then set repository secrets `GPG_PRIVATE_KEY`
   (`gpg --armor --export-secret-keys <FPR>`) and `PASSPHRASE`.

If the key that signs and the key that is registered differ, ingestion fails
with no useful signal.

## Rehearse, twice

**1. Locally, with byte-identical naming.** Use the same GoReleaser version
the workflow pins.

```sh
goreleaser check          # catches every v2 config error for free
goreleaser healthcheck
git tag v0.4.0            # locally; do not push
GPG_FINGERPRINT=<FPR> goreleaser release --clean --skip=publish
git tag -d v0.4.0
```

Then inspect `dist/`:

- one zip per platform;
- `gpg --verify dist/*_SHA256SUMS.sig dist/*_SHA256SUMS`;
- `unzip -l` shows the binary named `terraform-provider-porkbun_v0.4.0`;
- `file dist/*.sig` says `data` / OpenPGP **binary**, not ASCII text.
  An armored signature is rejected by the registry.

**2. In CI, without spending the tag.** Run the Release workflow via
`workflow_dispatch` with `dry_run` checked. That exercises the real runner,
the real `ghaction-import-gpg` step and the real secrets — everything except
the irreversible publish. This is the highest-value step here.

## Cut the release

Tag and push. `release.draft` is on permanently, so the workflow produces a
**draft** GitHub release: inspect it, then publish by hand.

If the draft is wrong, the retry path is all three of these, in order:

1. delete the draft release;
2. **delete the remote tag**;
3. re-push the tag, which re-fires `on.push.tags`.

Skipping step 2 is what makes a re-run impossible.

## Version choice

`v0.4.0`. The fork inherited eleven upstream tags including `v0.3.0`, which
points at the current `main`, so `v0.3.0` is unavailable. Staying in `0.x`
keeps the freedom to break `porkbun_dns_record` again without a registry
major-version event, and avoids the Go module `/vN` suffix that `v1.0.0`
would impose on the next break. The inherited tags are inert: the registry
ingests GitHub *Releases*, and there are none.

## Sequencing

Do not cut `v0.4.0` until `porkbun_domain_nameservers` has been proven
against real infrastructure through a `dev_overrides` block. Burning the
version on a release that has not exercised the one feature the fork exists
for defeats the point of every rehearsal above.
