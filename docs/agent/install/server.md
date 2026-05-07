# SpeechKit Server Install

Install stable:

```sh
curl -fsSL https://speechkit.cc/install-server.sh | sh
```

Install v0.30 Preview:

```sh
curl -fsSL https://speechkit.cc/install-server.sh | sh -s -- --channel preview
```

Options:

```text
--channel stable|preview
--dir PATH
--public-url URL
--image IMAGE
--public-bind
--no-up
--help
```

The script creates a self-contained Docker Compose deployment. It does not
require a git clone.

By default the generated Compose stack binds the server to `127.0.0.1:8080`.
Use `--public-bind` only when the host is intentionally exposed through a TLS
reverse proxy and bearer auth remains enabled.

Stable image:

```text
ghcr.io/kombifyio/speechkit-server:latest
```

Preview image:

```text
ghcr.io/kombifyio/speechkit-server:v0.30-preview
```

After install:

```sh
cd /opt/speechkit
docker compose ps
curl -fsS http://localhost:8080/healthz
curl -fsS http://localhost:8080/readyz
```

Bearer token:

```sh
grep '^SPEECHKIT_SERVER_TOKEN=' /opt/speechkit/.env
```
