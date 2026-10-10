#!/usr/bin/env bash
set -euo pipefail

tag=${1:?Usage: ensure-release-tag.sh VERSION_TAG}
ref="refs/tags/$tag"
git check-ref-format "$ref"
commit=$(git rev-parse --verify HEAD)

# Checkout fetches all tags. Preserve an existing tag, including its annotation.
if git show-ref --verify --quiet "$ref"; then
  if [[ $(git rev-parse --verify "$ref^{commit}") != "$commit" ]]; then
    printf 'Release tag %s exists but does not point at the release commit.\n' "$tag" >&2
    exit 1
  fi
else
  git tag -- "$tag" "$commit"
fi

# A non-forced push also refuses a conflicting tag created after checkout.
git push origin "$ref"
