# RDAI Telegram Verification Bot

This project is a Go web application that:

- serves an English frontend page
- generates a one-time verification token asynchronously after the page loads
- verifies the token when a user posts `/verify <token>` in a configured Telegram channel
- issues one AxonHub API key for each verified token
- stores verification state in SQLite

## Features

- Single Go binary
- Server-rendered frontend with minimal JavaScript
- SQLite persistence with one-time token issuance rules
- Telegram bot support with polling by default
- Optional Telegram webhook mode
- HTTP path prefix support for the web UI and app APIs
- Fixed internal AxonHub key name prefix in code

## Configuration

Environment variables:

- `AXONHUB_API_KEY`: Service account API key with `write_api_keys`
- `AXONHUB_ENDPOINT`: AxonHub OpenAPI GraphQL endpoint
- `TELEGRAM_BOT_TOKEN`: Telegram bot token
- `TELEGRAM_CHANNEL_ID`: Telegram channel ID to watch
- `HTTP_ADDR`: HTTP listen address, default `0.0.0.0:8080`
- `HTTP_PATH_PREFIX`: Optional path prefix for UI and app APIs, default empty
- `TOKEN_TTL`: Verification token TTL, default `24h`
- `TELEGRAM_USE_WEBHOOK`: Set to `true` to enable webhook mode
- `TELEGRAM_WEBHOOK_URL`: Public base URL used when webhook mode is enabled
- `TELEGRAM_WEBHOOK_SECRET`: Secret value used for Telegram webhook authentication
- `SQLITE_PATH`: Optional fallback SQLite path if `--sqlite-path` is not set, default `/data/rdai-bot.db`
- `RATE_LIMIT`: Requests per second per IP for public API endpoints, default `10`
- `RATE_BURST`: Maximum burst size for rate limiting, default `20`

CLI arguments:

```bash
go run . --sqlite-path /data/rdai-bot.db
```

Available flags:

- `--sqlite-path`: SQLite database path
- `--http-addr`: Override `HTTP_ADDR`
- `--http-path-prefix`: Override `HTTP_PATH_PREFIX`

## Runtime behavior

### Frontend

- `GET /` or `GET <prefix>/` only serves the frontend and does not create a token
- `POST /api/verification/start` creates a pending verification token asynchronously after the page loads
- The frontend renders the verification command after the async token request completes
- The page stores a second session token in the browser and sends it with API requests
- The user clicks `Get API Key` manually after Telegram verification completes

### App API

With an empty prefix:

- `POST /api/verification/status`
- `POST /api/verification/start`
- `POST /api/keys`

With `HTTP_PATH_PREFIX=/app`:

- `POST /app/api/verification/status`
- `POST /app/api/verification/start`
- `POST /app/api/keys`

### Telegram

- Bot command is always `/verify <token>`
- `HTTP_PATH_PREFIX` does not change Telegram command parsing
- Webhook endpoint is fixed at `/telegram/webhook`
- In webhook mode, requests must include a valid `X-Telegram-Bot-Api-Secret-Token`

## Development

Format:

```bash
gofmt -w *.go
```

Test:

```bash
GOPATH=/tmp/go GOMODCACHE=/tmp/go/pkg/mod GOCACHE=/tmp/go-build-cache go test ./...
```

If your environment blocks network access, Go may be unable to download missing modules.

## Docker

Build:

```bash
docker build -t rdai-bot .
```

Run with a persistent data directory:

```bash
docker run --rm \
  -p 8080:8080 \
  -v "$(pwd)/data:/data" \
  -e AXONHUB_API_KEY="your_service_account_key" \
  -e AXONHUB_ENDPOINT="http://host.docker.internal:8090/openapi/v1/graphql" \
  -e TELEGRAM_BOT_TOKEN="your_bot_token" \
  -e TELEGRAM_CHANNEL_ID="-1001234567890" \
  rdai-bot
```

The container stores SQLite data in `/data/rdai-bot.db` by default.

Compose example:

```bash
docker compose up -d
```

See [docker-compose.yaml](/Users/kaede/Developer/Utilities/rdai-bot/docker-compose.yaml) and replace the placeholder environment values before starting it.

## Release Image Publishing

Publishing a GitHub Release triggers [release-ghcr.yml](/Users/kaede/Developer/Utilities/rdai-bot/.github/workflows/release-ghcr.yml), which:

- builds per-platform images from `Dockerfile`
- pushes them to `ghcr.io/<owner>/<repo>` by digest
- merges them into a multi-arch manifest
- tags the final image with the release version and `latest`

You can also trigger the workflow manually with `workflow_dispatch` and provide a `tag` input to rebuild a specific tag.

Current release platforms:

- `linux/amd64`
- `linux/arm64`
- `linux/arm/v7`
- `linux/386`
- `linux/ppc64le`
- `linux/riscv64`
- `linux/s390x`

Example image name for this repository:

```bash
ghcr.io/akinokaede/rdai-bot:v1.0.0
ghcr.io/akinokaede/rdai-bot:latest
```

## AxonHub notes

- Requests to the AxonHub OpenAPI GraphQL endpoint require a service account key
- The caller key must include `write_api_keys`
- Newly created keys are generated through the existing `CreateLLMAPIKey` mutation in [`graphql/api_key.graphql`](/Users/kaede/Developer/Utilities/rdai-bot/graphql/api_key.graphql)
