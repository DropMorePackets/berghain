#!/usr/bin/env bash

set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
run_dir="$(mktemp -d)"
berghain_pid=""
haproxy_pid=""

cleanup() {
    status=$?
    trap - EXIT

    [[ -z "$haproxy_pid" ]] || kill "$haproxy_pid" 2>/dev/null || true
    [[ -z "$berghain_pid" ]] || kill "$berghain_pid" 2>/dev/null || true
    [[ -z "$haproxy_pid" ]] || wait "$haproxy_pid" 2>/dev/null || true
    [[ -z "$berghain_pid" ]] || wait "$berghain_pid" 2>/dev/null || true

    if [[ $status -ne 0 ]]; then
        printf '\nBerghain log:\n' >&2
        cat "$run_dir/berghain.log" >&2 2>/dev/null || true
        printf '\nHAProxy log:\n' >&2
        cat "$run_dir/haproxy.log" >&2 2>/dev/null || true
    fi

    rm -rf "$run_dir"
    exit "$status"
}
trap cleanup EXIT

cd "$repo_root"
go build -o "$run_dir/berghain" ./cmd/spop

"$run_dir/berghain" -config test/e2e/spop.yaml >"$run_dir/berghain.log" 2>&1 &
berghain_pid=$!
haproxy -db -f test/e2e/haproxy.cfg >"$run_dir/haproxy.log" 2>&1 &
haproxy_pid=$!

for port in 18080 18081; do
    ready=""
    for _ in $(seq 1 30); do
        status="$(curl --max-time 1 --silent --output /dev/null --write-out '%{http_code}' "http://localhost:$port/" || true)"
        if [[ $status == 403 ]]; then
            ready=1
            break
        fi
        sleep 1
    done
    if [[ -z "$ready" ]]; then
        echo "E2E stack did not serve the challenge page on port $port" >&2
        exit 1
    fi
done

export BERGHAIN_E2E_BASE_URL=http://localhost:18080
export BERGHAIN_E2E_TURNSTILE_URL=http://localhost:18081
cd test/e2e
go test -count=1 -tags=e2e -v .
