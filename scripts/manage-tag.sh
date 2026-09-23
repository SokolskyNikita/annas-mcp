#!/usr/bin/env bash

# Create an immutable release tag for the version recorded in the repository.
# Releases are published from version tags; tags must never be moved after
# they have been published because clients cache binaries by tag.

set -euo pipefail

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
ROOT_DIR=$(CDPATH= cd -- "$SCRIPT_DIR/.." && pwd)
VERSION_FILE="$ROOT_DIR/internal/version/version.txt"
PACKAGE_FILE="$ROOT_DIR/package.json"

usage() {
    cat <<'EOF'
Usage: scripts/manage-tag.sh add

  add      Create and push the current release tag.

Release tags are immutable. To publish a new build, update the version files,
commit the change, and run this script again. Existing local or remote tags
are rejected; there is no tag recreation or deletion command.
EOF
}

if [[ $# -ne 1 || "$1" != "add" ]]; then
    usage >&2
    exit 2
fi

if [[ ! -f "$VERSION_FILE" ]]; then
    printf 'Error: %s was not found\n' "$VERSION_FILE" >&2
    exit 1
fi

VERSION=$(tr -d '[:space:]' < "$VERSION_FILE")
if [[ ! "$VERSION" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
    printf 'Error: version.txt must contain a release version like v1.2.3 (got %q)\n' "$VERSION" >&2
    exit 1
fi

if [[ ! -f "$PACKAGE_FILE" ]]; then
    printf 'Error: %s was not found\n' "$PACKAGE_FILE" >&2
    exit 1
fi

PACKAGE_VERSION=$(node -p "JSON.parse(require('fs').readFileSync(process.argv[1], 'utf8')).version" "$PACKAGE_FILE")
if [[ "${VERSION#v}" != "$PACKAGE_VERSION" ]]; then
    printf 'Error: %s and %s disagree (%s vs %s)\n' \
        "$VERSION_FILE" "$PACKAGE_FILE" "$VERSION" "$PACKAGE_VERSION" >&2
    exit 1
fi

cd "$ROOT_DIR"

if [[ -n "$(git status --porcelain)" ]]; then
    printf 'Error: release tagging requires a clean working tree\n' >&2
    exit 1
fi

if git rev-parse --verify --quiet "refs/tags/$VERSION" >/dev/null; then
    printf 'Error: local tag %s already exists; release tags are immutable\n' "$VERSION" >&2
    exit 1
fi

if ! remote_tags=$(git ls-remote --tags origin "refs/tags/$VERSION"); then
    printf 'Error: unable to verify whether remote tag %s exists\n' "$VERSION" >&2
    exit 1
fi
if [[ -n "$remote_tags" ]]; then
    printf 'Error: remote tag %s already exists; release tags are immutable\n' "$VERSION" >&2
    exit 1
fi

printf 'Creating and pushing tag: %s\n' "$VERSION"
git tag "$VERSION"
git push origin "$VERSION"
printf 'Tag %s pushed successfully\n' "$VERSION"
