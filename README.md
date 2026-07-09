# Berghain

🕺 Welcome to Berghain: Where Only Valid Browsers Get the Backend Party Started! 🎉

Berghain is your trusty SPOE-Agent, guarding the entrance to the backend like a seasoned bouncer. This Go and
HAProxy-powered tool ensures that only the coolest and most valid browsers can access the exclusive party happening on
the other side.

With Berghain in charge, you can be confident that your backend is reserved for the true VIPs of the internet, keeping
out any uninvited guests. It's like the bouncer of the web world, ensuring that your resources are reserved for the
browsers that really know how to dance!

> Seeing a "Request on Hold" screen, or want to understand what visitors experience? See the
> [help page](https://dropmorepackets.github.io/berghain/) for troubleshooting, browser compatibility and privacy details.

## Supported CAPTCHAs
- None (Simple JS execute)
- POW

## Planned support
- Simple Captcha (Including Sound)
- [hCaptcha](https://www.hcaptcha.com/)
- [reCatpcha](https://developers.google.com/recaptcha?hl=de)
- [Turnstile](https://developers.cloudflare.com/turnstile/)

## Example setup with HAProxy
To start berghain locally you can follow these easy steps:

For Debian / Ubuntu: apt install npm

0. Run `npm install` inside `web/`
1. Run `npm run build` inside `web/`
2. Run `haproxy -f examples/haproxy/haproxy.cfg`
3. Run `go run ./cmd/spop/. -config cmd/spop/config.yaml`

For production use, generate a random `secret` to place in the Berghain configuration file using `openssl rand -base64 32`.

## Running with Docker

To run the project using Docker, follow these steps:

1. Build the Docker images:
   ```sh
   docker-compose build
   ```

2. Start the services:
   ```sh
   docker-compose up
   ```

3. Access the application:
   - Test App: [http://localhost:8080](http://localhost:8080)

Make sure to have Docker and Docker Compose installed on your system before running these commands.

## Configuration

Berghain is configured with a YAML file (see [`cmd/spop/config.yaml`](cmd/spop/config.yaml) for a
working example). The main knobs are:

| Key | Scope | Description |
| --- | --- | --- |
| `secret` | top-level | Base64-encoded HMAC secret, 32 bytes. Generate one with `openssl rand -base64 32`. Used to sign clearance cookies. |
| `default.levels` | top-level | The default list of challenge levels applied when a frontend defines none of its own. |
| `frontend.<name>.levels` | per frontend | Per-frontend list of challenge levels, overriding `default`. |
| `frontend.<name>.trusted_domains` | per frontend | Hosts (including their subdomains) that may share a validated session. By default the exact host is bound into the cookie; list a domain here to let its subdomains share one clearance. |

Each entry in a `levels` list accepts:

| Key | Type / range | Default | Description |
| --- | --- | --- | --- |
| `duration` | Go duration (e.g. `30s`, `30m`, `24h`) | — | How long a clearance obtained at this level stays valid. |
| `type` | `none` \| `pow` \| `pow-worker` | — | `none` is a simple JS execution check; `pow` requires a proof-of-work; `pow-worker` is the same POW but must be solved inside a Web Worker. |
| `countdown` | integer `0`–`9` | `3` | Seconds shown to the visitor before the check completes. |
| `difficulty` | integer `1`–`255` | `16` | For `type: pow` only: the number of leading zero bits the POW solution must have. Higher = harder for the client. |

## Customising the challenge page

The challenge page can be customised without forking `web/`:

- **`VITE_ENTRYPOINT`** — point at a custom `index.html` for full theming.
- **`VITE_HOOKS`** — point at a JS module that default-exports challenge-page hooks
  (`onInit`, `onCapabilities`, `onChallengeStart`, `onSuccess`, `onFailure`, `onBanned`).
  The interface is documented in [`web/src/hooks-default.js`](web/src/hooks-default.js). This lets
  branding/analytics/behaviour live in a separate repository:

  ```sh
  VITE_HOOKS=/path/to/my-hooks.js npm run build:default
  ```

## Operations (HAProxy + feeds)

Berghain itself is stateless — it only issues and validates clearance. Broader traffic policy lives in
HAProxy configuration (see [`examples/haproxy/haproxy.cfg`](examples/haproxy/haproxy.cfg)), which keeps
the agent free of any per-client state:

- **Rate limiting** — a global `rate-limit sessions` cap, a per-IP burst limiter (returns `429`), and a
  sliding-window anti-bruteforce counter on the challenge endpoint. These live in HAProxy stick-tables
  (three tracked counters, `sc0`/`sc1`/`sc2`, within the default `MAX_SESS_STKCTR`).
- **Escalating difficulty** — the per-IP request rate (and any reputation/ASN/VPN/Tor match) raises the
  Berghain challenge `level`, which selects a harder challenge from the `levels` list.
- **Temporary bans** — exceeding the anti-bruteforce window flags the source; further requests receive a
  `429` ban page with a live countdown (`X-Ban-Remaining`). This is a flat ban that lifts when the
  stick-table entry expires; true escalating/decaying durations are provided by CrowdSec (see below).
- **Tarpit + UA blocking** — library / scraper User-Agents (`examples/haproxy/maps/bad_ua.map`) are held
  in a tarpit instead of reaching the backend.
- **Privacy** — client IPs are never logged in plaintext: HAProxy logs a keyed hash, and the Go agent
  logs `Berghain.HashSource(addr)` (a keyed HMAC of the address).
- **Hidden services** — requests to `.onion`/`.i2p` hosts bypass Berghain entirely.

### IP reputation & network feeds

The map files under [`examples/haproxy/maps/`](examples/haproxy/maps/) classify source addresses
(banlist, reputation score, ASN → action, VPN/Tor exit ranges, the blocked Cloudflare ranges). They are
empty by default and refreshed out-of-band by the `feedupdater` tool, which fetches public feeds
(Cloudflare ranges, the Tor bulk exit list) and rewrites the maps atomically:

```sh
go run ./cmd/feedupdater -maps-dir examples/haproxy/maps            # run once
go run ./cmd/feedupdater -maps-dir examples/haproxy/maps -interval 6h  # or on a schedule
```

Run it from cron, a systemd timer, or a sidecar. A running HAProxy loads maps at startup, so apply
refreshed files with a reload or push them through the Runtime API admin socket.

### CrowdSec (optional)

For community-maintained IP reputation and true escalating/decaying ban durations, run
[CrowdSec](https://www.crowdsec.net/) with its HAProxy remediation component alongside Berghain (a
separate SPOE filter that coexists with Berghain's). CrowdSec decisions can drive the same `silent-drop`
/ level-raise / ban-page paths. This integration is optional and not wired into the example compose file.

## Attributions
Thanks to [@NullDev](https://github.com/NullDev) and [@arellak](https://github.com/arellak), as they did most of the frontend work.
