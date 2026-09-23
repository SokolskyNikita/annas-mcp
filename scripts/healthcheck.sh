#!/usr/bin/env bash

# Healthcheck script for annas-mcp. Local checks are safe to run by default;
# upstream searches require --live (or ANNAS_MCP_HEALTHCHECK_LIVE=1).

set -uo pipefail

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

usage() {
    cat <<'EOF'
Usage: scripts/healthcheck.sh [--live]

Runs local build and CLI checks. Pass --live to query Anna's Archive with
the configured environment. Live checks can also be enabled with
ANNAS_MCP_HEALTHCHECK_LIVE=1.

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

TESTS_PASSED=0
TESTS_FAILED=0

printf '%s\n' '====================================='
printf '%s\n' "  Anna's Archive MCP Healthcheck"
printf '%s\n\n' '====================================='

run_with_timeout() {
    if command -v timeout >/dev/null 2>&1; then
        timeout "$TIMEOUT_SECONDS" "$@"
    elif command -v gtimeout >/dev/null 2>&1; then
        gtimeout "$TIMEOUT_SECONDS" "$@"
    elif command -v perl >/dev/null 2>&1; then
        perl -e 'alarm shift; exec @ARGV' "$TIMEOUT_SECONDS" "$@"
    else
        printf 'No timeout implementation found; running the check without a timeout\n' >&2
        "$@"
    fi
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

run_check() {
    local label=$1
    local expected=$2
    shift 2
    local output_file
    local status
    local matched=1
    output_file=$(mktemp "${TMPDIR:-/tmp}/annas-mcp-healthcheck.XXXXXX")

    printf '%b[%s]%b %s...\n' "$YELLOW" "$((TESTS_PASSED + TESTS_FAILED + 1))" "$NC" "$label"
    run_with_timeout "$@" >"$output_file" 2>&1
    status=$?

    if [[ -z "$expected" ]] || grep -Fq "$expected" "$output_file"; then
        matched=0
    fi
    if [[ $status -eq 0 && $matched -eq 0 ]]; then
        printf '%b✓ PASSED%b\n\n' "$GREEN" "$NC"
        TESTS_PASSED=$((TESTS_PASSED + 1))
    else
        if [[ $status -eq 124 ]]; then
            printf '%b✗ FAILED%b - timed out after %ss\n' "$RED" "$NC" "$TIMEOUT_SECONDS"
        elif [[ $status -ne 0 ]]; then
            printf '%b✗ FAILED%b - exited with code %s\n' "$RED" "$NC" "$status"
        else
            printf '%b✗ FAILED%b - expected output was not found\n' "$RED" "$NC"
        fi
        if [[ -s "$output_file" ]]; then
            printf '%s\n' '  Diagnostic output (credentials redacted):'
            redact_output <"$output_file" | sed 's/^/    /'
        fi
        printf '\n'
        TESTS_FAILED=$((TESTS_FAILED + 1))
    fi
    rm -f "$output_file"
}

run_check 'Running the Go test suite' '' go test ./...
run_check 'Checking the CLI help command' '' go run ./cmd/annas-mcp --help

if [[ "$LIVE" -eq 1 ]]; then
    run_check "Testing live book search for 'crypto'" 'Book 1:' \
        go run ./cmd/annas-mcp book-search crypto
    run_check "Testing live article search for DOI 10.48550/arXiv.1706.03762" 'DOI:' \
        go run ./cmd/annas-mcp article-search 10.48550/arXiv.1706.03762
else
    printf '%bLive upstream checks skipped%b; pass --live to enable them.\n\n' "$YELLOW" "$NC"
fi

printf '%s\n' '====================================='
printf '%s\n' '  Test Summary'
printf '%s\n' '====================================='
printf 'Total tests: %s\n' "$((TESTS_PASSED + TESTS_FAILED))"
printf '%bPassed: %s%b\n' "$GREEN" "$TESTS_PASSED" "$NC"
printf '%bFailed: %s%b\n\n' "$RED" "$TESTS_FAILED" "$NC"

if [[ "$TESTS_FAILED" -eq 0 ]]; then
    printf '%bAll healthchecks passed!%b\n' "$GREEN" "$NC"
    exit 0
fi

printf '%bSome healthchecks failed.%b\n' "$RED" "$NC"
exit 1
