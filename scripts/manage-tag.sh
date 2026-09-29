#!/usr/bin/env bash

# Create an immutable release tag for the version recorded in the repository.
# Releases are published from version tags; tags must never be moved after
# they have been published because clients cache binaries by tag.

set -euo pipefail

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
ROOT_DIR=$(CDPATH= cd -- "$SCRIPT_DIR/.." && pwd)

usage() {
    cat <<'EOF'
Usage: scripts/manage-tag.sh add

  add      Create and push the current release tag.

Release tags are immutable and annotated. Update both version files, commit
and push main, wait for CI to pass, then run this script. The checkout must
match origin/main. Existing tags are rejected; tags are never recreated.
EOF
}

if [[ $# -ne 1 || "$1" != "add" ]]; then
    usage >&2
    exit 2
fi

VERSION=$(node "$ROOT_DIR/scripts/check-version.mjs")

cd "$ROOT_DIR"

if [[ -n "$(git status --porcelain)" ]]; then
    printf 'Error: release tagging requires a clean working tree\n' >&2
    exit 1
fi

if git rev-parse --verify --quiet "refs/tags/$VERSION" >/dev/null; then
    printf 'Error: local tag %s already exists; release tags are immutable\n' "$VERSION" >&2
    exit 1
fi

if ! remote_refs=$(git ls-remote origin refs/heads/main "refs/tags/$VERSION"); then
    printf 'Error: unable to verify origin/main and remote release tags\n' >&2
    exit 1
fi
if [[ "$remote_refs" == *"refs/tags/$VERSION"* ]]; then
    printf 'Error: remote tag %s already exists; release tags are immutable\n' "$VERSION" >&2
    exit 1
fi

remote_main=$(awk '$2 == "refs/heads/main" { print $1 }' <<<"$remote_refs")
if [[ -z "$remote_main" || "$(git rev-parse HEAD)" != "$remote_main" ]]; then
    printf 'Error: release tagging requires HEAD to match the pushed origin/main commit\n' >&2
    exit 1
fi

printf 'Creating and pushing tag: %s\n' "$VERSION"
git tag -a "$VERSION" -m "Release $VERSION"
git push origin "refs/tags/$VERSION"
printf 'Tag %s pushed successfully\n' "$VERSION"
