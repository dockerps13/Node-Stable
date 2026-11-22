#!/usr/bin/env bash
# Guard against accidental release/version drift in the independently
# maintained Node-Stable repository.
set -euo pipefail
cd "$(dirname "$0")/.."

VERSION="$(cat VERSION)"
[[ "$VERSION" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]] || {
  echo "Invalid VERSION: $VERSION" >&2
  exit 1
}
release='.github/workflows/release-stable.yml'
matrix='.github/workflows/multiplatform.yml'
grep -Fq 'tags:' "$release"
grep -Fq 'test "$VERSION" = "$(cat VERSION)"' "$release"
grep -Fq 'gh release create "$VERSION"' "$release"
grep -Fq 'VERSION="$(cat VERSION)"' "$matrix"
if grep -Eq 'main.version=v[0-9]+\.[0-9]+\.[0-9]+' "$matrix"; then
  echo "Hardcoded binary version found in multiplatform workflow" >&2
  exit 1
fi
if grep -Eq '(make build-linux build-linux-arm64 VERSION=v[0-9]+|gh release create v[0-9]+)' "$release"; then
  echo "Hardcoded release version found in publication workflow" >&2
  exit 1
fi
echo "Release version policy verified: $VERSION"
