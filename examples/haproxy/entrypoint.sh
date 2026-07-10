#!/bin/sh
set -eu

./berghain -config config.yaml &

(
    attempts=0
    while [ ! -S /tmp/haproxy-admin.sock ]; do
        attempts=$((attempts + 1))
        if [ "$attempts" -ge 100 ]; then
            echo "HAProxy Runtime API socket did not become ready" >&2
            exit 1
        fi
        sleep 0.1
    done

    exec ./feedupdater \
        -map-file examples/haproxy/state/reputation.map \
        -runtime-socket /tmp/haproxy-admin.sock \
        -runtime-map examples/haproxy/state/reputation.map \
        -interval 6h
) &

exec haproxy -W -db -f haproxy.cfg
