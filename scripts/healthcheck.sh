#!/usr/bin/env bash

# Healthcheck script for annas-mcp. Local checks are safe to run by default;
# upstream searches require --live (or ANNAS_MCP_HEALTHCHECK_LIVE=1).

set -uo pipefail

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P) || exit 1
REPO_ROOT=$(CDPATH= cd -- "$SCRIPT_DIR/.." && pwd -P) || exit 1
cd "$REPO_ROOT" || exit 1

if [[ -t 1 && -z ${NO_COLOR:-} ]]; then
    RED=$'\033[0;31m'
    GREEN=$'\033[0;32m'
    YELLOW=$'\033[1;33m'
    NC=$'\033[0m'
else
    RED=''
    GREEN=''
    YELLOW=''
    NC=''
fi

usage() {
    cat <<'EOF'
Usage: scripts/healthcheck.sh [--live]

Runs local build and CLI checks from the repository root. Pass --live to query
Anna's Archive with the configured environment. Live checks can also be enabled
with ANNAS_MCP_HEALTHCHECK_LIVE=1.

ANNAS_MCP_HEALTHCHECK_TIMEOUT sets the per-check timeout in seconds (default: 120).
EOF
}

LIVE=${ANNAS_MCP_HEALTHCHECK_LIVE:-0}
TIMEOUT_SECONDS=${ANNAS_MCP_HEALTHCHECK_TIMEOUT:-120}

while [[ $# -gt 0 ]]; do
    case "$1" in
        --live)
            LIVE=1
            ;;
        --help|-h)
            usage
            exit 0
            ;;
        *)
            printf 'Unknown option: %s\n\n' "$1" >&2
            usage >&2
            exit 2
            ;;
    esac
    shift
done

case "$LIVE" in
    1|true|TRUE|yes|YES)
        LIVE=1
        ;;
    0|false|FALSE|no|NO)
        LIVE=0
        ;;
    *)
        printf 'ANNAS_MCP_HEALTHCHECK_LIVE must be 0 or 1\n' >&2
        exit 2
        ;;
esac

if ! [[ "$TIMEOUT_SECONDS" =~ ^[1-9][0-9]*$ ]]; then
    printf 'ANNAS_MCP_HEALTHCHECK_TIMEOUT must be a positive integer\n' >&2
    exit 2
fi

CHECK_DIR=$(mktemp -d "${TMPDIR:-/tmp}/annas-mcp-healthcheck.XXXXXX") || {
    printf 'Could not create a temporary healthcheck directory\n' >&2
    exit 1
}

cleanup() {
    local status=$?
    trap - EXIT INT TERM HUP
    rm -rf -- "$CHECK_DIR" >/dev/null 2>&1 || true
    exit "$status"
}

trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
trap 'exit 129' HUP

TIMEOUT_MODE=none
TIMEOUT_COMMAND=''
if command -v timeout >/dev/null 2>&1; then
    TIMEOUT_MODE=command
    TIMEOUT_COMMAND=timeout
elif command -v gtimeout >/dev/null 2>&1; then
    TIMEOUT_MODE=command
    TIMEOUT_COMMAND=gtimeout
elif command -v perl >/dev/null 2>&1; then
    TIMEOUT_MODE=perl
else
    printf '%bWarning:%b no timeout implementation found; checks will run without a timeout\n' "$YELLOW" "$NC" >&2
fi

run_with_timeout() {
    case "$TIMEOUT_MODE" in
        command)
            "$TIMEOUT_COMMAND" "$TIMEOUT_SECONDS" "$@"
            ;;
        perl)
            perl -e 'alarm shift; exec @ARGV' "$TIMEOUT_SECONDS" "$@"
            ;;
        none)
            "$@"
            ;;
    esac
}

redact_output() {
    # Only diagnostic command output is shown, with common credential-shaped
    # values removed. The script never prints the process environment.
    if command -v perl >/dev/null 2>&1; then
        perl -pe 's/(ANNAS_[A-Z0-9_]+|authorization|cookie|token|secret|password)(\s*[=:]\s*)("[^"]*"|\x27[^\x27]*\x27|[^\s,;}]+)/$1$2[REDACTED]/ig' | tail -n 20
    else
        tail -n 20
    fi
}

TESTS_PASSED=0
TESTS_FAILED=0

run_check() {
    local label=$1
    local expected=$2
    shift 2
    local output_file="$CHECK_DIR/check-$((TESTS_PASSED + TESTS_FAILED + 1)).log"
    local status
    local matched=0

    printf '%b[%d]%b %s...\n' "$YELLOW" "$((TESTS_PASSED + TESTS_FAILED + 1))" "$NC" "$label"
    run_with_timeout "$@" >"$output_file" 2>&1
    status=$?

    if [[ -n "$expected" ]] && ! grep -Fq "$expected" "$output_file"; then
        matched=1
    fi
    if [[ $status -eq 0 && $matched -eq 0 ]]; then
        printf '%bpassed%b\n' "$GREEN" "$NC"
        TESTS_PASSED=$((TESTS_PASSED + 1))
        return
    fi

    if [[ $status -eq 124 || $status -eq 142 ]]; then
        printf '%bfailed%b (timed out after %ss)\n' "$RED" "$NC" "$TIMEOUT_SECONDS"
    elif [[ $status -ne 0 ]]; then
        printf '%bfailed%b (exit %s)\n' "$RED" "$NC" "$status"
    else
        printf '%bfailed%b (expected output was not found)\n' "$RED" "$NC"
    fi
    if [[ -s "$output_file" ]]; then
        printf '  diagnostics:\n'
        redact_output <"$output_file" | sed 's/^/    /'
    fi
    TESTS_FAILED=$((TESTS_FAILED + 1))
}

printf "Anna's Archive MCP healthcheck (%s)\n" "$([[ "$LIVE" -eq 1 ]] && printf 'live' || printf 'local')"
run_check 'Go test suite' '' go test ./...
run_check 'CLI help' '' go run ./cmd/annas-mcp --help

if [[ "$LIVE" -eq 1 ]]; then
    run_check "Live book search for 'crypto'" 'Book 1:' \
        go run ./cmd/annas-mcp book-search crypto
    run_check "Live article search for DOI 10.48550/arXiv.1706.03762" 'DOI:' \
        go run ./cmd/annas-mcp article-search 10.48550/arXiv.1706.03762
else
    printf '%bLive upstream searches skipped%b; pass --live to enable them.\n' "$YELLOW" "$NC"
fi

printf '\n%d passed, %d failed\n' "$TESTS_PASSED" "$TESTS_FAILED"
if [[ "$TESTS_FAILED" -eq 0 ]]; then
    printf '%bAll healthchecks passed.%b\n' "$GREEN" "$NC"
    exit 0
fi

printf '%bHealthcheck failed.%b\n' "$RED" "$NC"
exit 1
