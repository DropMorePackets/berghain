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

## Reputation feed updater

`cmd/feedupdater` builds one HAProxy `map_ip` reputation map from validated public feeds and an
optional local banlist. Tor exits receive the challenge action by default. Cloudflare ranges are
disabled by default because blocking them would break deployments that intentionally receive
traffic through Cloudflare.

```sh
# Write a persistent map file once.
go run ./cmd/feedupdater -map-file examples/haproxy/state/reputation.map

# Also update the same map atomically in a running HAProxy process.
go run ./cmd/feedupdater \
  -map-file examples/haproxy/state/reputation.map \
  -runtime-socket /tmp/haproxy-admin.sock \
  -interval 6h
```

Set `-cloudflare-action=block` only when that policy is intentional. `-banlist` accepts a local
file containing one IP address per line; local bans override a challenge action for the same IP.
If any enabled remote source fails, is oversized, is empty, or contains malformed data, the update
is rejected before the existing map is replaced. Live transactions require HAProxy 2.4 or newer;
when `-runtime-map` is set, its value must exactly match the map identifier in HAProxy's loaded
configuration.

The example HAProxy policy interprets map action `1` as a silent drop and action `3` as challenge
level 3. It refreshes the map every six hours in the container. Add lower-case, exact hostnames to
`examples/haproxy/maps/bypass-hosts.lst` only when a hostname must bypass both reputation and rate
policies. Ports are removed before matching; hostname suffixes such as `.onion` and `.i2p` are never
trusted implicitly. Docker Compose persists the generated reputation state in its
`reputation-data` volume and mounts the operator-edited bypass list read-only.

## Attributions
Thanks to [@NullDev](https://github.com/NullDev) and [@arellak](https://github.com/arellak), as they did most of the frontend work.
