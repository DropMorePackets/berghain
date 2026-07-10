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

## Customising the challenge page

The frontend build supports two environment variables for operator-owned customisation:

- `VITE_ENTRYPOINT` selects an alternative HTML entrypoint.
- `VITE_HOOKS` selects a JavaScript file whose labeled phase blocks are compiled directly
  into the default challenge flow.

Paths may be absolute or relative to the `web/` directory when running the npm scripts. The file
can contain shared static imports and any of these optional labeled blocks:

- `init` runs after the DOM is ready and before the cookie check.
- `challengeStart` runs after the challenge is fetched and can access `challenge`.
- `success` runs before the success UI and can access `challenge` and `countdown`.
- `failure` runs before the failure UI and can access `challenge`, `countdown`, and `result`.

For example:

```js
init:{
    await document.fonts.ready;
    document.title = "Verifying - Example";
}

challengeStart:{
    console.info("Challenge started", challenge.t);
}

success:{
    document.documentElement.dataset.challengeStatus = "success";
    console.info("Challenge passed", {challenge, countdown});
}

failure:{
    document.documentElement.dataset.challengeStatus = "failure";
    console.error("Challenge failed", {challenge, countdown, result});
}
```

This example is available as [`web/examples/challenge-page.js`](web/examples/challenge-page.js).

```sh
cd web
VITE_HOOKS=./examples/challenge-page.js npm run build:default
```

The phase blocks are inserted as scoped statements during Vite's transform phase; there is no runtime
hook registry or callback dispatch. Static imports are resolved relative to the customization file. Top-level
`await` is supported and delays the next challenge step. Exceptions follow the surrounding challenge flow's
normal error behavior, and omitted phase blocks are ignored.

The file cannot export values or contain other top-level statements. Phase blocks cannot use top-level
`var`, dynamic `import()`, `import.meta`, or top-level `arguments`. Environment path changes require
restarting the Vite development server.

## Attributions
Thanks to [@NullDev](https://github.com/NullDev) and [@arellak](https://github.com/arellak), as they did most of the frontend work.
