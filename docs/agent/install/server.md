# SpeechKit Server Install

Install stable:

```sh
curl -fsSL https://speechkit.cc/install-server.sh | sh
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

Browser-facing reference files:

- `https://speechkit.cc/install-server/docker-compose.example.yml`
- `https://speechkit.cc/install-server/config.browser.example.toml`

For a browser app running on the host against a server in Docker Compose, set
`SPEECHKIT_PUBLIC_URL=http://localhost:8080`. `SPEECHKIT_SERVER_PUBLIC_URL` is
accepted as a compatibility alias, but `SPEECHKIT_PUBLIC_URL` is canonical and
wins when both are set. Voice Agent session creation returns a `ws_url` based
on this public origin, not the Docker-internal service name. WebSocket upgrades
use `ws_url` plus `ws_subprotocol`; query-string ticket URLs are legacy-only.

Fresh local installs use SQLite by default at the configured data volume.
Postgres is optional for dev stacks and production operators that want a
separate database; provide `SPEECHKIT_POSTGRES_DSN` or `POSTGRES_DSN` only when
you intentionally add a Postgres service.

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

Complete setup with the API. The base server image leaves admin
username/password disabled, but public installs should enable it immediately
unless an authenticated edge already protects `/setup`:

Automation path: set `SPEECHKIT_SERVER_TOKEN` before first start when an agent
needs a ready-to-call local server. Otherwise `/setup` is public only during
first-run bootstrap; after setup is complete, settings writes require admin
auth.

```sh
ADMIN_PASSWORD="$(openssl rand -base64 32)"
curl -fsS -X PATCH http://localhost:8080/v1/server/settings \
  -H 'Content-Type: application/json' \
  -d "{
    \"onboarding_complete\": true,
    \"admin_auth\": {
      \"enabled\": true,
      \"username\": \"admin\",
      \"password\": \"${ADMIN_PASSWORD}\"
    },
    \"server_auth\": {
      \"mode\": \"managed_bearer\",
      \"bearer_token_env\": \"SPEECHKIT_SERVER_TOKEN\",
      \"generate_token\": true
    }
  }"
```

Store the returned generated bearer token in the deployment environment as
`SPEECHKIT_SERVER_TOKEN`, then restart the stack. Store the generated admin
password in the operator's password manager; the server writes only a bcrypt
hash.

Bearer token:

```sh
grep '^SPEECHKIT_SERVER_TOKEN=' /opt/speechkit/.env
```
