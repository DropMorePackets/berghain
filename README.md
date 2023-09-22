# Berghain

🕺 Welcome to Berghain: Where Only Valid Browsers Get the Backend Party Started! 🎉

Berghain is your trusty SPOE-Agent, guarding the entrance to the backend like a seasoned bouncer. This Go and
HAProxy-powered tool ensures that only the coolest and most valid browsers can access the exclusive party happening on
the other side.

With Berghain in charge, you can be confident that your backend is reserved for the true VIPs of the internet, keeping
out any uninvited guests. It's like the bouncer of the web world, ensuring that your resources are reserved for the
browsers that really know how to dance!

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

1. Run `npm run build` inside `web/`
2. Run `haproxy -f examples/haproxy/haproxy.cfg`
3. Run `go run ./cmd/spop/. -config cmd/spop/config.yaml`

## Attributions
Thanks to [@NullDev](https://github.com/NullDev) and [@arellak](https://github.com/arellak), as they did most of the frontend work.
