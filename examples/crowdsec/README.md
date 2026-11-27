# CrowdSec IP reputation (optional)

Berghain can act as a [CrowdSec](https://www.crowdsec.net/) *bouncer*: its
reputation service polls the CrowdSec local API decision stream and pushes
every decision into HAProxy stick-tables **live over the peers protocol** — no
map files, no reloads, and Berghain itself stays stateless. Decisions map to
the `gpt0` tag consumed by
[`examples/haproxy/haproxy-reputation.cfg`](../haproxy/haproxy-reputation.cfg):

| CrowdSec decision | gpt0 | HAProxy behaviour                    |
|-------------------|------|--------------------------------------|
| `ban`             | 1    | `silent-drop`                        |
| `captcha`         | 3    | minimum Berghain challenge level 3   |
| anything else     | 1    | fail closed to a ban                 |

Per-decision durations are honored: entries are pushed as timed stick-table
updates and expire in HAProxy exactly when the decision does, even if the
service is down at that moment. The same service also challenges Tor exit
nodes (static feed) unless `tor_exits: false` is set.

## Running it

The service runs **embedded** in the Berghain agent (`reputation:` section in
the spop config), so the docker setup stays two containers: the existing
haproxy+berghain container and CrowdSec.

1. Start CrowdSec once so you can register the bouncer:

   ```sh
   docker compose -f docker-compose.yml -f examples/crowdsec/docker-compose.crowdsec.yml up -d crowdsec
   docker compose -f docker-compose.yml -f examples/crowdsec/docker-compose.crowdsec.yml \
       exec crowdsec cscli bouncers add berghain
   ```

2. Export the printed key and start the rest:

   ```sh
   export CROWDSEC_API_KEY=<key from step 1>
   docker compose -f docker-compose.yml -f examples/crowdsec/docker-compose.crowdsec.yml up
   ```

3. Try it — add a decision and watch the stick-table:

   ```sh
   docker compose -f docker-compose.yml -f examples/crowdsec/docker-compose.crowdsec.yml \
       exec crowdsec cscli decisions add --ip 203.0.113.7 --type ban --duration 5m
   ```

   Within the poll interval (10s by default) requests from that address are
   silent-dropped; `cscli decisions delete --ip 203.0.113.7` lifts it again
   within one poll.

CrowdSec only produces decisions when it can *see* traffic (or when another
machine in your CrowdSec network reports it): feed it your HAProxy logs via an
acquisition file, or rely on the community blocklist that comes with console
enrollment. Both are standard CrowdSec configuration — see their
[HAProxy collection](https://app.crowdsec.net/hub/author/crowdsecurity/collections/haproxy).

## Standalone mode

Deployments that scale HAProxy and the feed separately can run the exact same
service as its own daemon instead of embedding it:

```sh
go run ./cmd/feedupdater \
    -peer-listen 0.0.0.0:10001 \
    -crowdsec-url http://crowdsec:8080    # key via CROWDSEC_API_KEY
```

Every HAProxy in the peers mesh then lists `berghain_feed` once and they all
learn the same tables; the daemon copes fine with being one peer among many
(it validates handshakes and acknowledges the updates the other peers teach).
