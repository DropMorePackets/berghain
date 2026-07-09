# CrowdSec integration (optional)

Berghain is stateless: rate-limits and a flat temporary ban are handled by
HAProxy stick-tables (see `examples/haproxy/haproxy.cfg`). For the **full**
reputation/ban experience — community IP reputation and true escalating/decaying
ban durations — run [CrowdSec](https://www.crowdsec.net/) alongside it. CrowdSec
holds all the state; Berghain stays stateless.

## How it fits

CrowdSec's HAProxy remediation runs as a **separate SPOE filter** that coexists
with Berghain's. It sets transaction variables per request:

- `txn.crowdsec.remediation` — `ban`, `captcha`, or `allow`
- `txn.crowdsec.duration`    — remaining ban time (drives the ban countdown)

Wire those into the existing decision points in `haproxy.cfg`:

```haproxy
# ban     -> serve the banned page with the CrowdSec-provided remaining time
# captcha -> raise the Berghain challenge level, let the normal flow run
http-request set-var(req.berghain.level) int(3) if !is_hidden_service { var(txn.crowdsec.remediation) -m str captcha }
http-request set-var(txn.ban_remaining) var(txn.crowdsec.duration) if { var(txn.crowdsec.remediation) -m str ban }
http-request return status 429 content-type "text/html" lf-file "examples/haproxy/errors/banned.html" \
    hdr "Retry-After" "%[var(txn.ban_remaining)]" if { var(txn.crowdsec.remediation) -m str ban }
```

This replaces the flat `gpt0` ban flag with CrowdSec decisions; the frontend
ban-countdown screen (`t:3` / `banned.html`) is unchanged.

## Setup

1. Start the stack with the CrowdSec override:
   ```sh
   docker compose -f docker-compose.yml -f examples/crowdsec/docker-compose.crowdsec.yml up
   ```
2. Register the bouncer and set its API key:
   ```sh
   docker compose exec crowdsec cscli bouncers add haproxy-spoa
   # put the printed key in CROWDSEC_BOUNCER_API_KEY
   ```
3. Point HAProxy at the bouncer's SPOA socket (a second `filter spoe` engine +
   backend, exactly like Berghain's `berghain_spop`) and add the ACLs above.

CrowdSec's integration is read-only outbound — no visitor data leaves your
infrastructure unless you opt into the CrowdSec Console.
