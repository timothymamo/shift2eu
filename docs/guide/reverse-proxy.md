# Behind a reverse proxy

> **Retired** ([ADR 0037](../adr/0037-retire-the-http-api-and-release-the-cli-alone.md)): releases no
> longer ship `shift2eu-server`. Its images up to the last release that shipped them stay on `ghcr.io`
> but get no updates, security fixes included. Run assessments with the CLI or in the browser on the site.

How to deploy `shift2eu-server`: behind a reverse proxy that terminates TLS, authenticates each user
and limits the rate of requests, with the server's own defences as a second line. This page lists the
server's flags and variables, a hardened `docker run` and a minimal nginx configuration.

The server speaks plain HTTP and knows no users. Run it behind a reverse proxy, or the website's own
backend, that terminates TLS, authenticates each user and limits the rate of requests per client, and
let only that proxy reach it. The server's own defences are a second line, not a replacement:

- **A bearer token.** Set one, of at least 32 characters (`openssl rand -hex 32`), in the environment
  variable `SHIFT2EU_SERVER_TOKEN` or in a file named by `--token-file`, never both; there is no token
  flag, as an argument shows in the process list. Every `/v1/` request then needs
  `Authorization: Bearer <token>`, or it is `401`. `/healthz` and `/readyz` stay open, for probes.
- **Loopback unless told otherwise.** It listens on `127.0.0.1:8080` by default. It refuses to start on
  any other address (`0.0.0.0` in a container) without a token, unless `--insecure-no-auth` says you
  mean it, and then warns at startup. On loopback without a token it answers only to the `Host`
  `localhost` or its own address, against DNS rebinding.
- **Bounded work.** At most `--concurrency` assessments run at once and a request over it is refused at
  once with `503 busy` and `Retry-After: 1`, never queued. A request body is capped at 10 MiB plus
  64 KiB, a slow client is cut off by the read and write timeouts, and at most `--max-connections`
  connections stay open. On `SIGTERM` it stops accepting connections and finishes the requests in
  flight, within 25 seconds.

| Flag or variable | Default | What it sets |
|---|---|---|
| `--listen <host:port>` | `127.0.0.1:8080` | The address to listen on. A non-loopback one needs a token or `--insecure-no-auth`. |
| `SHIFT2EU_SERVER_TOKEN` | unset | The bearer token. Set but empty, or shorter than 32 characters, is a startup error. |
| `--token-file <path>` | unset | Read the token from this file instead, white space trimmed. A file other users can read earns a warning: `chmod 600` it. |
| `--concurrency <n>` | `0` | Assessments served at once. `0` sizes it from the CPUs and the memory budget: `max(1, min(CPUs, budget / 1 GiB))`, the budget being `GOMEMLIMIT` or, without it, 2 GiB. The largest request takes about 1 GB. |
| `GOMEMLIMIT` | unset | The Go runtime's soft memory limit, read by the runtime and by the `--concurrency` default. Set it to the container's memory limit or a little under (`GOMEMLIMIT=1800MiB` for 2 GiB). |
| `--max-connections <n>` | `256` | Connections held open at once; one over the cap is closed at once. |
| `--cors-origin <origin>` | none | Allow CORS from this origin, such as `https://www.example.com` (repeatable). CORS is off without one; never `*`, never credentials. |
| `--allowed-host <host>` | none | Serve only this `Host` header value, with or without a port (repeatable); any other is `421`. Set it to the name the proxy forwards. |
| `--insecure-no-auth` | off | Listen on a non-loopback address without a token. Only behind a proxy that authenticates. |

The server writes its startup lines and an access log to standard error, one line per request with
the method, the path, the status, the duration and the size, and nothing else.

With Docker, publish the port on loopback only, so the proxy on the same host is the one way in, and
pass the token by name, never as `-e NAME=value`. The image listens on `0.0.0.0:8080` by default and,
without a token, refuses to start (exit 2). Arguments replace that default, so the example gives
`--listen` again beside `--allowed-host`. The server needs no Linux capabilities and no privilege
escalation, so drop them:

```bash
export SHIFT2EU_SERVER_TOKEN="$(openssl rand -hex 32)"   # or read it from your secret store
docker run -d --name shift2eu-server --restart unless-stopped --read-only \
  --cap-drop ALL --security-opt no-new-privileges \
  --memory 2g -e GOMEMLIMIT=1800MiB -e SHIFT2EU_SERVER_TOKEN \
  -p 127.0.0.1:8080:8080 \
  ghcr.io/timothymamo/shift2eu-server:<version> \
  --listen 0.0.0.0:8080 --allowed-host api.example.com
```

To keep the token out of the container's environment (`docker inspect` shows it), mount a token file
read-only instead and run as its owner, as [Mounts, users and reports](docker.md#mounts-users-and-reports)
explains: replace `-e SHIFT2EU_SERVER_TOKEN` with
`--user "$(id -u):$(id -g)" --mount type=bind,src="$PWD/shift2eu-token",dst=/run/secrets/shift2eu-token,readonly`
and add `--token-file /run/secrets/shift2eu-token`.

A minimal nginx in front of it: TLS only (plain HTTP just redirects), a password per user, a rate
limit per client address, a body cap just above the server's, and the token added on the way in, so
users never see it. A request over the proxy's rate limit is `429`, which tells it apart from the
server's own `503 busy` with `Retry-After`. The health endpoints are left off the public name; probe
them on `127.0.0.1:8080`.

```nginx
limit_req_zone $binary_remote_addr zone=shift2eu:10m rate=2r/s;

server {
    listen 80;
    server_name api.example.com;
    return 301 https://api.example.com$request_uri;
}

server {
    listen 443 ssl;
    server_name api.example.com;
    ssl_certificate     /etc/ssl/api.example.com/fullchain.pem;
    ssl_certificate_key /etc/ssl/api.example.com/privkey.pem;
    ssl_protocols       TLSv1.2 TLSv1.3;
    add_header Strict-Transport-Security "max-age=31536000" always;

    location /v1/ {
        auth_basic           "shift2eu";
        auth_basic_user_file /etc/nginx/shift2eu.htpasswd;
        limit_req            zone=shift2eu burst=10 nodelay;
        limit_req_status     429;
        client_max_body_size 11m;

        proxy_set_header Host $host;
        # One line, mode 0600: proxy_set_header Authorization "Bearer <the token>";
        include /etc/nginx/shift2eu-token.conf;
        proxy_pass http://127.0.0.1:8080;
    }

    location / {
        return 404;
    }
}
```

This proxy serves callers on its own origin and servers calling it directly. A browser page on another
origin should call through its site's own backend, which holds the credentials, rather than through
`--cors-origin`: a CORS preflight (`OPTIONS`) carries no credentials and the server never allows them,
so never drop authentication from the proxy, or from the server, to make preflights pass.

Configure every client with the `https://` URL. The port-80 redirect is a convenience, not a
protection: a client that sends its Basic credentials over plain HTTP has sent them in clear before
any redirect, and HSTS protects a client only after its first HTTPS visit.
